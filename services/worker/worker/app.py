from __future__ import annotations

import json
import os
import shutil
import threading
import time
import uuid
from dataclasses import asdict, dataclass, field
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.parse import urlparse

from .provider import ProviderConfig, ProviderError, VideoProvider


@dataclass
class Job:
    id: str
    request: dict[str, Any]
    status: str = "queued"
    progress: int = 0
    videoUrl: str = ""
    providerId: str = ""
    assetId: str = ""
    error: str = ""
    createdAt: float = field(default_factory=time.time)
    updatedAt: float = field(default_factory=time.time)
    canceled: bool = False

    def public(self) -> dict[str, Any]:
        result = asdict(self)
        result.pop("request", None)
        result.pop("canceled", None)
        return result


class JobManager:
    def __init__(self, provider: VideoProvider, public_base_url: str):
        self.provider = provider
        self.public_base_url = public_base_url.rstrip("/")
        self._jobs: dict[str, Job] = {}
        self._lock = threading.RLock()

    def create(self, request: dict[str, Any]) -> Job:
        validate_request(request)
        job = Job(id=f"job-{uuid.uuid4().hex[:16]}", request=request)
        with self._lock:
            self._jobs[job.id] = job
        thread = threading.Thread(target=self._run, args=(job.id,), daemon=True)
        thread.start()
        return job

    def get(self, job_id: str) -> Job | None:
        with self._lock:
            return self._jobs.get(job_id)

    def cancel(self, job_id: str) -> Job | None:
        with self._lock:
            job = self._jobs.get(job_id)
            if job is None:
                return None
            if job.status in {"queued", "running", "waiting_provider"}:
                job.canceled = True
                job.status = "canceled"
                job.updatedAt = time.time()
            return job

    def _run(self, job_id: str) -> None:
        try:
            with self._lock:
                job = self._jobs[job_id]
                job.status = "running"
                job.progress = max(1, job.progress)
                job.updatedAt = time.time()

            self.provider.generate(
                self._jobs[job_id].request,
                lambda status, progress, video_url, provider_id, asset_id, error: self._update(
                    job_id,
                    status,
                    progress,
                    video_url,
                    provider_id,
                    asset_id,
                    error,
                ),
                lambda: self._is_canceled(job_id),
            )
        except ProviderError as exc:
            self._update(job_id, "failed", 0, "", "", "", str(exc))
        except Exception as exc:  # Keep worker process alive; never expose tracebacks/API keys.
            self._update(job_id, "failed", 0, "", "", "", f"Worker 内部错误: {exc}")

    def _update(
        self,
        job_id: str,
        status: str,
        progress: int,
        video_url: str,
        provider_id: str,
        asset_id: str,
        error: str,
    ) -> None:
        with self._lock:
            job = self._jobs[job_id]
            if job.canceled:
                return
            job.status = status
            job.progress = 100 if status == "succeeded" else progress
            job.providerId = provider_id or job.providerId
            if asset_id:
                job.assetId = asset_id
                job.videoUrl = f"{self.public_base_url}/v1/jobs/{job_id}/video"
            elif video_url:
                job.videoUrl = video_url
            job.error = error
            job.updatedAt = time.time()

    def _is_canceled(self, job_id: str) -> bool:
        with self._lock:
            job = self._jobs.get(job_id)
            return job is None or job.canceled


def validate_request(payload: dict[str, Any]) -> None:
    required = (
        "model",
        "prompt",
        "generationMode",
        "resolutionTier",
        "orientation",
        "seconds",
    )
    missing = [key for key in required if payload.get(key) in (None, "")]
    if missing:
        raise ValueError("缺少字段: " + ", ".join(missing))


class Handler(BaseHTTPRequestHandler):
    manager: JobManager
    config: ProviderConfig

    def do_GET(self) -> None:  # noqa: N802
        path = urlparse(self.path).path
        if path == "/healthz":
            self._json(
                HTTPStatus.OK,
                {
                    "status": "ok",
                    "provider": self.config.base_url,
                    "providerConfigured": bool(self.config.api_key),
                },
            )
            return
        if path.startswith("/v1/jobs/") and path.endswith("/video"):
            job_id = path.removeprefix("/v1/jobs/").removesuffix("/video")
            job = self.manager.get(job_id)
            if job is None:
                self._error(HTTPStatus.NOT_FOUND, "任务不存在")
                return
            self._stream_video(job)
            return
        if path.startswith("/v1/jobs/"):
            job_id = path.removeprefix("/v1/jobs/").split("/", 1)[0]
            job = self.manager.get(job_id)
            if job is None:
                self._error(HTTPStatus.NOT_FOUND, "任务不存在")
                return
            self._json(HTTPStatus.OK, job.public())
            return
        self._error(HTTPStatus.NOT_FOUND, "路由不存在")

    def _stream_video(self, job: Job) -> None:
        if job.status != "succeeded" or not job.assetId:
            self._error(HTTPStatus.CONFLICT, "视频资源尚未生成完成")
            return
        try:
            response = self.manager.provider.open_asset(
                job.assetId,
                str(job.request["model"]),
                self.headers.get("Range", ""),
            )
        except ProviderError as exc:
            self._error(HTTPStatus.BAD_GATEWAY, str(exc))
            return

        with response:
            status = getattr(response, "status", None) or response.getcode()
            self.send_response(status)
            for header in (
                "Content-Type",
                "Content-Length",
                "Content-Range",
                "Accept-Ranges",
                "ETag",
                "Last-Modified",
            ):
                value = response.headers.get(header)
                if value:
                    self.send_header(header, value)
            if not response.headers.get("Content-Type"):
                self.send_header("Content-Type", "video/mp4")
            self.end_headers()
            shutil.copyfileobj(response, self.wfile, length=1024 * 1024)

    def do_POST(self) -> None:  # noqa: N802
        path = urlparse(self.path).path
        if path == "/v1/jobs":
            try:
                payload = self._read_json()
                job = self.manager.create(payload)
            except ValueError as exc:
                self._error(HTTPStatus.UNPROCESSABLE_ENTITY, str(exc))
                return
            self._json(HTTPStatus.ACCEPTED, job.public())
            return
        if path.startswith("/v1/jobs/") and path.endswith("/cancel"):
            job_id = path.removeprefix("/v1/jobs/").removesuffix("/cancel")
            job = self.manager.cancel(job_id)
            if job is None:
                self._error(HTTPStatus.NOT_FOUND, "任务不存在")
                return
            self._json(HTTPStatus.OK, job.public())
            return
        self._error(HTTPStatus.NOT_FOUND, "路由不存在")

    def _read_json(self) -> dict[str, Any]:
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError as exc:
            raise ValueError("Content-Length 无效") from exc
        if length <= 0 or length > 1024 * 1024:
            raise ValueError("请求体为空或过大")
        try:
            value = json.loads(self.rfile.read(length))
        except json.JSONDecodeError as exc:
            raise ValueError("请求 JSON 无效") from exc
        if not isinstance(value, dict):
            raise ValueError("请求 JSON 必须是对象")
        return value

    def _json(self, status: HTTPStatus, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _error(self, status: HTTPStatus, message: str) -> None:
        self._json(status, {"error": {"message": message}})

    def log_message(self, format: str, *args: Any) -> None:
        print(f"worker-http {self.address_string()} {format % args}")


def create_server(config: ProviderConfig | None = None) -> ThreadingHTTPServer:
    config = config or ProviderConfig.from_env()
    host = os.getenv("WORKER_ADDR", "127.0.0.1")
    port = int(os.getenv("WORKER_PORT", "8090"))
    public_base_url = os.getenv("WORKER_PUBLIC_URL", f"http://127.0.0.1:{port}")
    Handler.config = config
    Handler.manager = JobManager(VideoProvider(config), public_base_url)
    return ThreadingHTTPServer((host, port), Handler)


def main() -> None:
    config = ProviderConfig.from_env()
    if not config.api_key:
        raise SystemExit(
            "Worker 启动失败：缺少 VIDEO_API_KEY。"
            "请在项目根目录 .env 中配置后重新启动。"
        )
    server = create_server(config)
    print(
        f"worker listening on http://{server.server_address[0]}:{server.server_address[1]} "
        f"provider={Handler.config.base_url}"
    )
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
