import asyncio
import json
import subprocess
import tempfile
import unittest
from unittest import mock
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import dlengine


class FakeStdout:
    def __init__(self, lines):
        self.lines = iter(lines)

    def __iter__(self):
        return self

    def __next__(self):
        return next(self.lines)

    def read(self, size):
        return b"payload"


class FakeStderr:
    def __init__(self, lines=()):
        self.lines = list(lines)

    def __iter__(self):
        return iter(self.lines)

    def read(self):
        raise AssertionError("stderr must be drained concurrently")

    def readline(self):
        raise AssertionError("stderr must be drained concurrently")


class FakeProcess:
    def __init__(self, stdout_lines=(), stderr_lines=(), timeout_on_wait=False):
        self.stdout = FakeStdout(stdout_lines)
        self.stderr = FakeStderr(stderr_lines)
        self.returncode = None
        self.killed = False
        self.waited = False
        self.timeout_on_wait = timeout_on_wait

    def poll(self):
        return self.returncode

    def wait(self, timeout=None):
        self.waited = True
        if self.timeout_on_wait:
            self.timeout_on_wait = False
            raise subprocess.TimeoutExpired("dlengine", timeout)
        if self.returncode is None:
            self.returncode = 0
        return self.returncode

    def kill(self):
        self.killed = True
        self.returncode = -9


class FakeAsyncStream:
    def __init__(self, chunk):
        self.chunk = chunk

    async def read(self, size):
        return self.chunk


class FakeAsyncProcess:
    def __init__(self):
        self.stdout = FakeAsyncStream(b"payload")
        self.stderr = self
        self.stderr_done = asyncio.Event()
        self.returncode = None
        self.killed = False
        self.waited = False

    async def readline(self):
        await self.stderr_done.wait()
        return b""

    async def wait(self):
        self.waited = True
        if self.returncode is None:
            self.returncode = 0
        return self.returncode

    def kill(self):
        self.killed = True
        self.returncode = -9
        self.stderr_done.set()


class DLEngineTests(unittest.TestCase):
    def make_engine(self):
        engine = object.__new__(dlengine.DLEngine)
        engine.bin_path = Path("dlengine")
        return engine

    def test_timeout_seconds_supports_numeric_and_go_durations(self):
        self.assertEqual(dlengine.DLEngine._timeout_seconds(2.5), 2.5)
        self.assertEqual(dlengine.DLEngine._timeout_seconds("15m"), 900.0)
        self.assertEqual(dlengine.DLEngine._timeout_seconds("1h30m10.5s"), 5410.5)
        self.assertIsNone(dlengine.DLEngine._timeout_seconds("invalid"))

    def test_darwin_binary_detection_uses_release_name(self):
        for machine, binary_name in (
            ("arm64", "dlengine-darwin-arm64"),
            ("x86_64", "dlengine-darwin-amd64"),
        ):
            with self.subTest(machine=machine), tempfile.TemporaryDirectory() as temp_dir:
                module_path = Path(temp_dir) / "python" / "dlengine.py"
                binary_path = Path(temp_dir) / "bin" / binary_name
                module_path.parent.mkdir()
                binary_path.parent.mkdir()
                module_path.touch()
                binary_path.touch()
                with mock.patch.object(dlengine, "__file__", str(module_path)), mock.patch.object(
                    dlengine.platform, "system", return_value="Darwin"
                ), mock.patch.object(dlengine.platform, "machine", return_value=machine):
                    self.assertEqual(dlengine.DLEngine._auto_detect_binary(), binary_path.resolve())

    def test_download_wraps_timeout_and_drains_stderr(self):
        process = FakeProcess(
            stdout_lines=[
                json.dumps(
                    {
                        "event": "completed",
                        "filename": "file.bin",
                        "dest_path": "file.bin",
                        "total_bytes": 1,
                        "elapsed_seconds": 0.1,
                        "avg_speed_bytes_sec": 10,
                    }
                ) + "\n"
            ],
            stderr_lines=["runtime warning\n"],
            timeout_on_wait=True,
        )
        with mock.patch.object(dlengine.subprocess, "Popen", return_value=process):
            with self.assertRaisesRegex(dlengine.DownloadEngineError, "Download timed out after 1m"):
                self.make_engine().download("https://example.test/file", timeout="1m")
        self.assertTrue(process.waited)
        self.assertEqual(process.stderr.lines, ["runtime warning\n"])

    def test_sync_stream_close_kills_child(self):
        process = FakeProcess()
        with mock.patch.object(dlengine.subprocess, "Popen", return_value=process):
            stream = self.make_engine().stream("https://example.test/file")
            self.assertEqual(next(stream), b"payload")
            stream.close()
        self.assertTrue(process.killed)
        self.assertTrue(process.waited)

    def test_async_stream_close_kills_child(self):
        async def run():
            process = FakeAsyncProcess()
            with mock.patch.object(
                dlengine.asyncio, "create_subprocess_exec", new=mock.AsyncMock(return_value=process)
            ):
                stream = self.make_engine().stream_async("https://example.test/file")
                self.assertEqual(await anext(stream), b"payload")
                await stream.aclose()
            self.assertTrue(process.killed)
            self.assertTrue(process.waited)

        asyncio.run(run())


if __name__ == "__main__":
    unittest.main()
