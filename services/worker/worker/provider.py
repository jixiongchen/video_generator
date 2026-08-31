from __future__ import annotations

import json
import os
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any, Callable


TERMINAL_STATUSES = {"succeeded", "failed", "canceled"}


@dataclass(frozen=True)
class ProviderConfig:
    base_url: str
    submit_path: str
    status_path_template: str
    api_key: str
    poll_interval_seconds: float
    timeout_seconds: float

    @classmethod
    def from_env(cls) -> "ProviderConfig":
        return cls(
            base_url=os.getenv(
                "VIDEO_API_BASE_URL", "https://api.jusuanhub.com:10443"
            ).rstrip("/"),
            submit_path=os.getenv(
                "VIDEO_API_SUBMIT_PATH", "/v1/media/generations"
            ),
            status_path_template=os.getenv(
                "VIDEO_API_STATUS_PATH_TEMPLATE", "/v1/media/generations/{id}"
            ),
            api_key=os.getenv("VIDEO_API_KEY", ""),
            poll_interval_seconds=float(
                os.getenv("VIDEO_API_POLL_INTERVAL_SECONDS", "3")
            ),
            timeout_seconds=float(os.getenv("VIDEO_API_TIMEOUT_SECONDS", "900")),
        )


class ProviderError(RuntimeError):
    pass


class VideoProvider:
    def __init__(self, config: ProviderConfig):
        self.config = config

    def generate(
        self,
        payload: dict[str, Any],
        update: Callable[[str, int, str, str, str], None],
        is_canceled: Callable[[], bool],
    ) -> None:
        if not self.config.api_key:
            raise ProviderError("缺少 VIDEO_API_KEY，请在项目根目录 .env 中配置")

        response = self._request_json("POST", self.config.submit_path, payload)
        provider_id = extract_provider_id(response)
        status = normalize_status(response)
        progress = extract_progress(response)
        video_url = extract_video_url(response)
        update(status, progress, video_url, provider_id, extract_error(response))

        if status in TERMINAL_STATUSES:
            if status == "succeeded" and not video_url:
                raise ProviderError("供应商任务已完成，但响应中没有视频 URL")
            return
        if not provider_id:
            raise ProviderError("供应商响应中没有任务 id/task_id")

        deadline = time.monotonic() + self.config.timeout_seconds
        while time.monotonic() < deadline:
            if is_canceled():
                return
            time.sleep(self.config.poll_interval_seconds)
            path = self.config.status_path_template.format(id=provider_id)
            response = self._request_json("GET", path)
            status = normalize_status(response)
            progress = extract_progress(response)
            video_url = extract_video_url(response)
            error = extract_error(response)
            update(status, progress, video_url, provider_id, error)
            if status in TERMINAL_STATUSES:
                if status == "succeeded" and not video_url:
                    raise ProviderError("供应商任务已完成，但响应中没有视频 URL")
                return
        raise ProviderError("等待供应商生成结果超时")

    def _request_json(
        self, method: str, path: str, payload: dict[str, Any] | None = None
    ) -> dict[str, Any]:
        url = self.config.base_url + (path if path.startswith("/") else "/" + path)
        body = None if payload is None else json.dumps(payload).encode("utf-8")
        request = urllib.request.Request(url=url, data=body, method=method)
        request.add_header("Authorization", f"Bearer {self.config.api_key}")
        request.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                raw = response.read()
        except urllib.error.HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="replace")[:2000]
            safe_raw = raw.replace(self.config.api_key, "***") if self.config.api_key else raw
            raise ProviderError(f"供应商 HTTP {exc.code}: {safe_raw}") from exc
        except urllib.error.URLError as exc:
            raise ProviderError(f"无法连接视频供应商: {exc.reason}") from exc
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise ProviderError("供应商返回了非 JSON 响应") from exc
        if not isinstance(parsed, dict):
            raise ProviderError("供应商响应必须是 JSON 对象")
        return parsed


def normalize_status(response: dict[str, Any]) -> str:
    raw = "queued"
    for item in iter_response_dicts(response):
        value = item.get("status") or item.get("task_status") or item.get("state")
        if value not in (None, ""):
            raw = str(value).lower()
            break
    mapping = {
        "pending": "queued",
        "queued": "queued",
        "created": "queued",
        "processing": "running",
        "running": "running",
        "in_progress": "running",
        "generating": "running",
        "success": "succeeded",
        "succeeded": "succeeded",
        "completed": "succeeded",
        "done": "succeeded",
        "failure": "failed",
        "failed": "failed",
        "error": "failed",
        "cancelled": "canceled",
        "canceled": "canceled",
        "expired": "failed",
    }
    return mapping.get(raw, "running")


def extract_provider_id(response: dict[str, Any]) -> str:
    for item in iter_response_dicts(response):
        value = item.get("id") or item.get("task_id") or item.get("request_id")
        if value not in (None, ""):
            return str(value)
    return ""


def extract_progress(response: dict[str, Any]) -> int:
    value: Any = 0
    for item in iter_response_dicts(response):
        if item.get("progress") not in (None, ""):
            value = item["progress"]
            break
    try:
        number = float(value)
    except (TypeError, ValueError):
        return 0
    if 0 <= number <= 1:
        number *= 100
    return max(0, min(100, round(number)))


def extract_video_url(response: dict[str, Any]) -> str:
    keys = ("video_url", "videoUrl", "content_url", "output_url", "url")
    for item in iter_response_dicts(response):
        for key in keys:
            value = item.get(key)
            if isinstance(value, str) and value.startswith(("http://", "https://")):
                return value

    for value in iter_response_values(response):
        if isinstance(value, str) and value.startswith(("http://", "https://")):
            lowered = value.lower().split("?", 1)[0]
            if lowered.endswith((".mp4", ".webm", ".mov", ".m4v")):
                return value
    return ""


def extract_error(response: dict[str, Any]) -> str:
    for item in iter_response_dicts(response):
        error = item.get("error")
        if isinstance(error, str):
            return error[:1000]
        if isinstance(error, dict):
            return str(error.get("message") or error.get("code") or "生成失败")[:1000]
    if normalize_status(response) == "failed":
        for item in iter_response_dicts(response):
            if item.get("message"):
                return str(item["message"])[:1000]
        return "生成失败"
    return ""


def iter_response_dicts(value: Any):
    """Yield response objects depth-first to support common provider envelopes."""
    if isinstance(value, dict):
        yield value
        for nested in value.values():
            yield from iter_response_dicts(nested)
    elif isinstance(value, list):
        for nested in value:
            yield from iter_response_dicts(nested)


def iter_response_values(value: Any):
    if isinstance(value, dict):
        for nested in value.values():
            yield nested
            yield from iter_response_values(nested)
    elif isinstance(value, list):
        for nested in value:
            yield nested
            yield from iter_response_values(nested)
