"""Agent HTTP 边界；由现有 Worker Handler 调用，不创建第二个服务端口。

路由与 Agent 执行器分离，后续 Agent 可在 REGISTRY 注册。不会读取小说本地路径，
避免请求参数演变成任意文件读取工具；Go 只传递当前步骤需要的材料。
"""
import base64

from .novel.agent import NovelAgent, validate_document
from .novel.importer import import_novel
from ..providers.text import TextConfig, TextProvider

REGISTRY = {"novel": NovelAgent}


def dispatch(path: str, body: dict) -> dict:
    if path == "/v1/agents/novel/import":
        try:
            raw = base64.b64decode(body.get("content", ""), validate=True)
        except (ValueError, TypeError):
            raise ValueError("导入内容编码无效") from None
        return import_novel(raw, body.get("encoding", "auto"))
    if path == "/v1/agents/novel/validate":
        return {"output": validate_document(body.get("operation", ""), body.get("document"), body.get("input", {}))}
    if path == "/v1/agents/novel/steps":
        context = body.get("input")
        if not isinstance(context, dict):
            raise ValueError("input 必须为对象")
        config = TextConfig.from_env()
        if body.get("expectedModel", config.model) != config.model or body.get("expectedProtocol", config.protocol) != config.protocol:
            raise ValueError("文本 Worker 的模型配置已变化，请新建任务，避免混用不同模型的检查点")
        return REGISTRY["novel"](TextProvider(config)).execute(body.get("operation", ""), context)
    raise ValueError("Agent 路由不存在")
