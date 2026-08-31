import json
import unittest
from unittest.mock import patch

from worker.provider import (
    ProviderConfig,
    VideoProvider,
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
            JSONResponse({"data": {"id": "task-live", "status": "queued"}}),
            JSONResponse(
                {
                    "data": {
                        "id": "task-live",
                        "status": "completed",
                        "progress": 1,
                        "video_url": "https://cdn.example.test/live.mp4",
                    }
                }
            ),
        ]
        provider = VideoProvider(
            ProviderConfig(
                base_url="https://api.example.test:10443",
                submit_path="/v1/media/generations",
                status_path_template="/v1/media/generations/{id}",
                api_key="secret-key",
                poll_interval_seconds=0,
                timeout_seconds=1,
            )
        )
        updates = []

        provider.generate(
            {
                "model": "video-model",
                "prompt": "test",
                "generationMode": "t2v",
                "resolutionTier": "480p",
                "orientation": "landscape",
                "seconds": 5,
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
        self.assertEqual(
            poll_request.full_url,
            "https://api.example.test:10443/v1/media/generations/task-live",
        )
        self.assertEqual(updates[-1][0], "succeeded")
        self.assertEqual(updates[-1][2], "https://cdn.example.test/live.mp4")


if __name__ == "__main__":
    unittest.main()
