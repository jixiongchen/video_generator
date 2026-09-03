"""文本模型传输边界。

依据用户提供的聚算 Chat Completions 示例，使用 qwen3-6-35b-a3b。
只传文本内容块，不走图片/视频素材上传；新供应商若协议不同，在这里增加适配。
没有 TEXT_API_KEY 时拒绝付费调用，不悄悄复用视频 Key。
"""
from __future__ import annotations

import json
import os
import time
from dataclasses import dataclass
from urllib import error, request
from urllib.parse import urlparse


class TextProviderError(Exception):
    """对外只携带安全消息；绝不拼接原始响应、请求头或供应商异常堆栈。"""


@dataclass(frozen=True)
class TextConfig:
    base_url: str = ""
    api_key: str = ""
    model: str = ""
    protocol: str = ""
    path: str = "/v1/chat/completions"
    timeout: int = 300
    context_tokens: int = 32768
    output_tokens: int = 4096

    @classmethod
    def from_env(cls) -> "TextConfig":
        return cls(
            base_url=os.getenv("TEXT_API_BASE_URL", "https://api.jusuanhub.com:10443").rstrip("/"),
            api_key=os.getenv("TEXT_API_KEY", ""), model=os.getenv("TEXT_API_MODEL", "qwen3-6-35b-a3b"),
            protocol=os.getenv("TEXT_API_PROTOCOL", "openai_chat"),
            path=os.getenv("TEXT_API_PATH", "/v1/chat/completions"),
            timeout=int(os.getenv("TEXT_API_TIMEOUT_SECONDS", "300")),
            context_tokens=int(os.getenv("TEXT_API_CONTEXT_TOKENS", "32768")),
            output_tokens=int(os.getenv("TEXT_API_OUTPUT_TOKENS", "4096")),
        )

    @property
    def configured(self) -> bool:
        return bool(self.base_url and self.api_key and self.model and self.protocol == "openai_chat")

    def public(self) -> dict:
        # 即使新增字段，也不能使用 asdict(self)：其中含有 Key。
        return {"configured": self.configured, "model": self.model,
                "protocol": self.protocol, "contextTokens": self.context_tokens,
                "outputTokens": self.output_tokens}


class _NoRedirect(request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        # Authorization 不随重定向发送至其他主机。
        return None


class TextProvider:
    def __init__(self, config: TextConfig):
        self.config = config
        self.opener = request.build_opener(_NoRedirect())

    def complete(self, system: str, content: dict) -> tuple[str, dict | None]:
        c = self.config
        if not c.configured:
            raise TextProviderError("文本模型未配置。请确认协议并配置 TEXT_API_BASE_URL、TEXT_API_KEY、TEXT_API_MODEL、TEXT_API_PROTOCOL。")
        parsed = urlparse(c.base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc or parsed.username or parsed.query:
            raise TextProviderError("TEXT_API_BASE_URL 格式无效")
        if not c.path.startswith("/") or c.path.startswith("//"):
            raise TextProviderError("TEXT_API_PATH 必须是以 / 开头的路径")
        if c.timeout <= 0 or c.output_tokens < 512 or c.context_tokens <= c.output_tokens + 2048:
            raise TextProviderError("文本模型上下文、输出额度或超时配置无效")
        user = json.dumps(content, ensure_ascii=False)
        # 无供应商 tokenizer 时按 UTF-8 字节数做保守上界估算，不按中文字符数冒充 token。
        # 超额直接拒绝，绝不能静默截断小说或模型上下文。
        if len((system + user).encode("utf-8")) + 1024 + c.output_tokens > c.context_tokens:
            raise TextProviderError("本步骤超出保守上下文预算，请增加模型上下文额度或减少本步骤材料；原文没有被截断。")
        payload = json.dumps({"model": c.model, "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": [{"type": "text", "text": user}]}],
            "max_tokens": c.output_tokens, "stream": False}, ensure_ascii=False).encode("utf-8")
        for attempt in range(4):
            req = request.Request(c.base_url + c.path, data=payload, method="POST", headers={
                "Content-Type": "application/json", "Authorization": f"Bearer {c.api_key}"})
            try:
                with self.opener.open(req, timeout=c.timeout) as response:
                    body = response.read(2 * 1024 * 1024 + 1)
                if len(body) > 2 * 1024 * 1024:
                    raise TextProviderError("文本响应过大，任务已暂停")
                data = json.loads(body)
                choice = data["choices"][0]
                if choice.get("finish_reason") == "length":
                    raise TextProviderError("模型输出达到长度上限，请调大输出额度后重试；未将截断内容保存为成功结果。")
                value = choice["message"]["content"]
                if not isinstance(value, str) or not value.strip():
                    raise TextProviderError("模型未返回有效文本，可能拒绝了请求")
                if c.api_key and c.api_key in value:
                    raise TextProviderError("模型返回内容触发密钥保护，结果已丢弃")
                usage = data.get("usage")
                safe_usage = {k: v for k, v in usage.items() if k in {"prompt_tokens", "completion_tokens", "total_tokens"} and type(v) is int and v >= 0} if isinstance(usage, dict) else None
                return value, safe_usage
            except error.HTTPError as exc:
                exc.close()
                # 只有明确拒绝执行的 429 自动重试。5xx/超时可能已消耗额度，不盲目重发。
                if exc.code == 429 and attempt < 3:
                    time.sleep(min(2 ** attempt, 8))
                    continue
                labels = {401: "鉴权失败", 403: "权限不足", 402: "余额不足", 429: "请求限流"}
                raise TextProviderError(f"文本供应商 HTTP {exc.code}：{labels.get(exc.code, '请求失败，请检查接口配置或稍后手动继续')}。") from None
            except (error.URLError, TimeoutError, OSError):
                raise TextProviderError("文本请求连接中断或超时，供应商是否计费未知；请确认后手动继续。") from None
            except (ValueError, KeyError, IndexError, TypeError):
                raise TextProviderError("文本响应不符合已配置的 Chat Completions 协议，请核对供应商示例。") from None
        raise TextProviderError("文本请求限流，请稍后继续")
