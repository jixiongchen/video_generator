import time
import unittest
from typing import Any, Callable

from worker.app import JobManager


class SuccessfulProvider:
    def generate(
        self,
        payload: dict[str, Any],
        update: Callable[[str, int, str, str, str, str], None],
        is_canceled: Callable[[], bool],
    ) -> None:
        if not is_canceled():
            update(
                "succeeded",
                100,
                "",
                "provider-task-1",
                "asset-1",
                "",
            )


class WorkerTests(unittest.TestCase):
    def test_job_completes_with_provider_result(self) -> None:
        manager = JobManager(SuccessfulProvider(), "http://127.0.0.1:8090")
        job = manager.create(
            {
                "model": "minimax-h3",
                "prompt": "test",
                "generationMode": "t2v",
                "resolutionTier": "768p",
                "orientation": "portrait",
                "seconds": 15,
            }
        )
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            current = manager.get(job.id)
            if current and current.status == "succeeded":
                self.assertEqual(current.progress, 100)
                self.assertEqual(current.assetId, "asset-1")
                self.assertEqual(
                    current.videoUrl,
                    f"http://127.0.0.1:8090/v1/jobs/{job.id}/video",
                )
                return
            time.sleep(0.05)
        self.fail("provider job did not complete")


if __name__ == "__main__":
    unittest.main()
