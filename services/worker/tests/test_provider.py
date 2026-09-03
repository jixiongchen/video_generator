import io
import json
import unittest
import urllib.error
from unittest.mock import patch

from worker.provider import (
    ProviderConfig,
    VideoProvider,
    extract_asset_id,
    extract_progress,
    extract_provider_id,
    extract_video_url,
    normalize_status,
)


class JSONResponse:
    def __init__(self, payload: dict):
        self.payload = payload

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self) -> bytes:
        return json.dumps(self.payload).encode("utf-8")


class ProviderNormalizationTests(unittest.TestCase):
    def test_normalizes_common_statuses(self) -> None:
        self.assertEqual(normalize_status({"status": "PROCESSING"}), "running")
        self.assertEqual(normalize_status({"task_status": "SUCCESS"}), "succeeded")
        self.assertEqual(normalize_status({"status": "expired"}), "failed")

    def test_extracts_provider_id_and_progress(self) -> None:
        self.assertEqual(extract_provider_id({"task_id": "task-1"}), "task-1")
        self.assertEqual(extract_provider_id({"jobId": "job-1"}), "job-1")
        self.assertEqual(extract_asset_id({"assets": [{"assetId": "asset-1"}]}), "asset-1")
        self.assertEqual(extract_progress({"progress": 0.42}), 42)
        self.assertEqual(extract_progress({"progress": 87}), 87)

    def test_extracts_nested_video_url(self) -> None:
        payload = {"output": [{"content": {"video_url": "https://x.test/a.mp4"}}]}
        self.assertEqual(extract_video_url(payload), "https://x.test/a.mp4")

    def test_extracts_nested_status_and_id(self) -> None:
        payload = {"data": {"task_id": "task-2", "status": "processing"}}
        self.assertEqual(extract_provider_id(payload), "task-2")
        self.assertEqual(normalize_status(payload), "running")

    @patch("worker.provider.urllib.request.urlopen")
    def test_live_provider_submits_and_polls(self, urlopen) -> None:
        urlopen.side_effect = [
            JSONResponse({"jobId": "job-live", "status": "queued"}),
            JSONResponse(
                {
                    "id": "job-live",
                    "status": "succeeded",
                    "assets": [{"assetId": "asset-live"}],
                }
            ),
        ]
        provider = VideoProvider(
            ProviderConfig(
                base_url="https://api.example.test:10443",
                submit_path="/v1/media/generations",
                job_path_template="/v1/jobs/{id}?model={model}",
                asset_path_template="/v1/assets/{asset_id}/content?model={model}",
                api_key="secret-key",
                poll_interval_seconds=0,
                timeout_seconds=1,
                request_timeout_seconds=300,
            )
        )
        updates = []

        provider.generate(
            {
                "model": "minimax-h3",
                "prompt": "test",
                "generationMode": "t2v",
                "resolutionTier": "768p",
                "orientation": "portrait",
                "seconds": 15,
            },
            lambda *args: updates.append(args),
            lambda: False,
        )

        self.assertEqual(urlopen.call_count, 2)
        submit_request = urlopen.call_args_list[0].args[0]
        poll_request = urlopen.call_args_list[1].args[0]
        self.assertEqual(
            submit_request.full_url,
            "https://api.example.test:10443/v1/media/generations",
        )
        self.assertEqual(submit_request.get_header("Authorization"), "Bearer secret-key")
        self.assertTrue(submit_request.get_header("Idempotency-key"))
        self.assertEqual(
            poll_request.full_url,
            "https://api.example.test:10443/v1/jobs/job-live?model=minimax-h3",
        )
        self.assertEqual(updates[-1][0], "succeeded")
        self.assertEqual(updates[-1][4], "asset-live")

    @patch("worker.provider.urllib.request.urlopen")
    def test_asset_content_uses_model_and_authorization(self, urlopen) -> None:
        urlopen.return_value = object()
        provider = VideoProvider(
            ProviderConfig(
                base_url="https://api.example.test:10443",
                submit_path="/v1/media/generations",
                job_path_template="/v1/jobs/{id}?model={model}",
                asset_path_template="/v1/assets/{asset_id}/content?model={model}",
                api_key="secret-key",
                poll_interval_seconds=0,
                timeout_seconds=1,
                request_timeout_seconds=300,
            )
        )

        provider.open_asset("asset/1", "minimax-h3")

        request = urlopen.call_args.args[0]
        self.assertEqual(
            request.full_url,
            "https://api.example.test:10443/v1/assets/asset%2F1/content?model=minimax-h3",
        )
        self.assertEqual(request.get_header("Authorization"), "Bearer secret-key")

    @patch("worker.provider.urllib.request.urlopen")
    def test_uploads_universal_reference_input(self, urlopen) -> None:
        urlopen.return_value = JSONResponse({"asset": {"assetId": "asset-input"}})
        provider = VideoProvider(
            ProviderConfig(
                base_url="https://api.example.test:10443",
                submit_path="/v1/media/generations",
                job_path_template="/v1/jobs/{id}?model={model}",
                asset_path_template="/v1/assets/{asset_id}/content?model={model}",
                api_key="secret-key",
                poll_interval_seconds=0,
                timeout_seconds=1,
                request_timeout_seconds=300,
            )
        )

        result = provider.upload_input(
            "minimax-h3", "multipart/form-data; boundary=test", b"multipart-body"
        )

        request = urlopen.call_args.args[0]
        self.assertEqual(
            request.full_url,
            "https://api.example.test:10443/v1/assets/input?model=minimax-h3",
        )
        self.assertEqual(request.get_header("Authorization"), "Bearer secret-key")
        self.assertEqual(
            request.get_header("Content-type"), "multipart/form-data; boundary=test"
        )
        self.assertEqual(request.data, b"multipart-body")
        self.assertEqual(result, {"asset": {"assetId": "asset-input"}})

    @patch("worker.provider.time.sleep")
    @patch("worker.provider.urllib.request.urlopen")
    def test_submit_retries_503_with_same_idempotency_key(self, urlopen, _sleep) -> None:
        urlopen.side_effect = [
            urllib.error.HTTPError(
                "https://api.example.test/v1/media/generations",
                503,
                "Service Unavailable",
                {},
                io.BytesIO(b'{"retryable":true}'),
            ),
            JSONResponse(
                {
                    "jobId": "job-after-retry",
                    "status": "succeeded",
                    "assets": [{"assetId": "asset-after-retry"}],
                }
            ),
        ]
        provider = VideoProvider(
            ProviderConfig(
                base_url="https://api.example.test",
                submit_path="/v1/media/generations",
                job_path_template="/v1/jobs/{id}?model={model}",
                asset_path_template="/v1/assets/{asset_id}/content?model={model}",
                api_key="secret-key",
                poll_interval_seconds=0,
                timeout_seconds=1,
                request_timeout_seconds=300,
            )
        )

        provider.generate(
            {
                "model": "minimax-h3",
                "prompt": "test",
                "generationMode": "t2v",
                "resolutionTier": "768p",
                "orientation": "portrait",
                "seconds": 15,
            },
            lambda *_args: None,
            lambda: False,
        )

        first = urlopen.call_args_list[0].args[0]
        second = urlopen.call_args_list[1].args[0]
        self.assertEqual(
            first.get_header("Idempotency-key"),
            second.get_header("Idempotency-key"),
        )


if __name__ == "__main__":
    unittest.main()
