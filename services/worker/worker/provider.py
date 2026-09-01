from __future__ import annotations

import json
import os
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from dataclasses import dataclass
from typing import Any, Callable


TERMINAL_STATUSES = {"succeeded", "failed", "canceled"}


@dataclass(frozen=True)
class ProviderConfig:
    base_url: str
    submit_path: str
    job_path_template: str
    asset_path_template: str
    api_key: str
    poll_interval_seconds: float
    timeout_seconds: float
    request_timeout_seconds: float

    @classmethod
    def from_env(cls) -> "ProviderConfig":
        return cls(
            base_url=os.getenv(
                "VIDEO_API_BASE_URL", "https://api.jusuanhub.com:10443"
            ).rstrip("/"),
            submit_path=os.getenv(
                "VIDEO_API_SUBMIT_PATH", "/v1/media/generations"
            ),
            job_path_template=os.getenv(
                "VIDEO_API_JOB_PATH_TEMPLATE", "/v1/jobs/{id}?model={model}"
            ),
            asset_path_template=os.getenv(
                "VIDEO_API_ASSET_PATH_TEMPLATE",
                "/v1/assets/{asset_id}/content?model={model}",
            ),
            api_key=os.getenv("VIDEO_API_KEY", ""),
            poll_interval_seconds=float(
                os.getenv("VIDEO_API_POLL_INTERVAL_SECONDS", "3")
            ),
            timeout_seconds=float(os.getenv("VIDEO_API_TIMEOUT_SECONDS", "900")),
            request_timeout_seconds=float(
                os.getenv("VIDEO_API_REQUEST_TIMEOUT_SECONDS", "300")
            ),
        )


class ProviderError(RuntimeError):
    pass


class VideoProvider:
    def __init__(self, config: ProviderConfig):
        self.config = config

    def generate(
        self,
        payload: dict[str, Any],
        update: Callable[[str, int, str, str, str, str], None],
        is_canceled: Callable[[], bool],
    ) -> None:
        if not self.config.api_key:
            raise ProviderError("缺少 VIDEO_API_KEY，请在项目根目录 .env 中配置")

        idempotency_key = os.getenv("IDEMPOTENCY_KEY") or str(uuid.uuid4())
        response = self._request_json(
            "POST",
            self.config.submit_path,
            payload,
            extra_headers={"Idempotency-Key": idempotency_key},
            retry_attempts=3,
        )
        provider_id = extract_provider_id(response)
        status = normalize_status(response)
        progress = extract_progress(response)
        video_url = extract_video_url(response)
        asset_id = extract_asset_id(response)
        update(
            status,
            progress,
            video_url,
            provider_id,
            asset_id,
            extract_error(response),
        )

        if status in TERMINAL_STATUSES:
            if status == "succeeded" and not (video_url or asset_id):
                raise ProviderError("供应商任务已完成，但响应中没有视频或资源 ID")
            return
        if not provider_id:
            raise ProviderError("供应商响应中没有任务 jobId/id")

        model = urllib.parse.quote(str(payload["model"]), safe="")
        deadline = time.monotonic() + self.config.timeout_seconds
        while time.monotonic() < deadline:
            if is_canceled():
                return
            time.sleep(self.config.poll_interval_seconds)
            path = self.config.job_path_template.format(
                id=urllib.parse.quote(provider_id, safe=""),
                model=model,
            )
            response = self._request_json("GET", path, retry_attempts=3)
            status = normalize_status(response)
            progress = extract_progress(response)
            video_url = extract_video_url(response)
            asset_id = extract_asset_id(response)
            error = extract_error(response)
            update(status, progress, video_url, provider_id, asset_id, error)
            if status in TERMINAL_STATUSES:
                if status == "succeeded" and not (video_url or asset_id):
                    raise ProviderError("供应商任务已完成，但响应中没有视频或资源 ID")
                return
        raise ProviderError("等待供应商生成结果超时")

    def open_asset(self, asset_id: str, model: str, range_header: str = ""):
        path = self.config.asset_path_template.format(
            asset_id=urllib.parse.quote(asset_id, safe=""),
            model=urllib.parse.quote(model, safe=""),
        )
        request = self._build_request("GET", path)
        if range_header:
            request.add_header("Range", range_header)
        try:
            return urllib.request.urlopen(
                request,
                timeout=self.config.request_timeout_seconds,
            )
        except urllib.error.HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="replace")[:2000]
            raise ProviderError(
                f"供应商资源 HTTP {exc.code}: {self._safe(raw)}"
            ) from exc
        except urllib.error.URLError as exc:
            raise ProviderError(f"无法下载供应商视频: {exc.reason}") from exc

    def _request_json(
        self,
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        extra_headers: dict[str, str] | None = None,
        retry_attempts: int = 1,
    ) -> dict[str, Any]:
        request = self._build_request(method, path, payload, extra_headers)
        raw = b""
        for attempt in range(retry_attempts):
            try:
                with urllib.request.urlopen(
                    request,
                    timeout=self.config.request_timeout_seconds,
                ) as response:
                    raw = response.read()
                break
            except urllib.error.HTTPError as exc:
                raw_error = exc.read().decode("utf-8", errors="replace")[:2000]
                retryable = exc.code == 429 or 500 <= exc.code < 600
                if retryable and attempt + 1 < retry_attempts:
                    time.sleep(2**attempt)
                    continue
                raise ProviderError(
                    f"供应商 HTTP {exc.code}: {self._safe(raw_error)}"
                ) from exc
            except urllib.error.URLError as exc:
                if attempt + 1 < retry_attempts:
                    time.sleep(2**attempt)
                    continue
                raise ProviderError(f"无法连接视频供应商: {exc.reason}") from exc

        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise ProviderError("供应商返回了非 JSON 响应") from exc
        if not isinstance(parsed, dict):
            raise ProviderError("供应商响应必须是 JSON 对象")
        return parsed

    def _build_request(
        self,
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        extra_headers: dict[str, str] | None = None,
    ) -> urllib.request.Request:
        url = self.config.base_url + (path if path.startswith("/") else "/" + path)
        body = None if payload is None else json.dumps(payload).encode("utf-8")
        request = urllib.request.Request(url=url, data=body, method=method)
        request.add_header("Authorization", f"Bearer {self.config.api_key}")
        request.add_header("Content-Type", "application/json")
        for key, value in (extra_headers or {}).items():
            request.add_header(key, value)
        return request

    def _safe(self, value: str) -> str:
        return value.replace(self.config.api_key, "***") if self.config.api_key else value


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
        value = (
            item.get("jobId")
            or item.get("job_id")
            or item.get("id")
            or item.get("task_id")
            or item.get("request_id")
        )
        if value not in (None, ""):
            return str(value)
    return ""


def extract_asset_id(response: dict[str, Any]) -> str:
    for item in iter_response_dicts(response):
        value = item.get("assetId") or item.get("asset_id")
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
