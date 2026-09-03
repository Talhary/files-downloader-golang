"""
Python Controller & SDK for the Go High-Speed Multi-Part Download & Streaming Engine.
Compatible with Windows, Linux (Ubuntu/Debian), macOS, and GitHub Actions.
"""

from __future__ import annotations

import asyncio
import dataclasses
import json
import os
import platform
import subprocess
import sys
import threading
from pathlib import Path
from typing import Any, AsyncGenerator, Callable, Dict, Generator, Optional, Union

__version__ = "1.1.0"


@dataclasses.dataclass
class ProbeResult:
    filename: str
    total_bytes: int
    accept_ranges: bool
    status_code: int
    final_url: str
    total_chunks: int
    version: str = "1.1.0"

    @property
    def total_mb(self) -> float:
        return self.total_bytes / (1024 * 1024)

    @property
    def total_gb(self) -> float:
        return self.total_bytes / (1024 * 1024 * 1024)


@dataclasses.dataclass
class ProgressEvent:
    downloaded_bytes: int
    total_bytes: int
    percent: float
    speed_bytes_sec: float
    eta_seconds: float
    elapsed_seconds: float
    active_workers: int
    completed_chunks: int
    total_chunks: int

    @property
    def speed_mb_s(self) -> float:
        return self.speed_bytes_sec / (1024 * 1024)

    @property
    def downloaded_mb(self) -> float:
        return self.downloaded_bytes / (1024 * 1024)

    @property
    def total_mb(self) -> float:
        return self.total_bytes / (1024 * 1024)


@dataclasses.dataclass
class DownloadResult:
    filename: str
    dest_path: str
    total_bytes: int
    elapsed_seconds: float
    avg_speed_bytes_sec: float

    @property
    def total_mb(self) -> float:
        return self.total_bytes / (1024 * 1024)

    @property
    def avg_speed_mb_s(self) -> float:
        return self.avg_speed_bytes_sec / (1024 * 1024)


class DownloadEngineError(Exception):
    """Raised when the download engine encounters a fatal error."""
    pass


class DLEngine:
    """
    High-Speed Multi-Part Downloader & Streamer controlled from Python.
    """

    def __init__(self, bin_path: Optional[Union[str, Path]] = None):
        if bin_path:
            self.bin_path = Path(bin_path).resolve()
        else:
            self.bin_path = self._auto_detect_binary()

        if not self.bin_path.exists():
            raise FileNotFoundError(
                f"DLEngine binary not found at '{self.bin_path}'. "
                "Please build it with 'go build ./cmd/dlengine' or provide the binary path."
            )

        # On Linux/macOS ensure binary has executable permissions
        if platform.system() != "Windows":
            try:
                os.chmod(self.bin_path, 0o755)
            except Exception:
                pass

        self.last_progress: Optional[ProgressEvent] = None

    @staticmethod
    def _auto_detect_binary() -> Path:
        base_dir = Path(__file__).resolve().parent.parent
        is_windows = platform.system() == "Windows"
        arch = platform.machine().lower()

        if is_windows:
            candidates = [
                base_dir / "dlengine.exe",
                base_dir / "bin" / "dlengine-windows-amd64.exe",
                base_dir / "bin" / "dlengine.exe",
            ]
        else:
            if "arm" in arch or "aarch64" in arch:
                candidates = [
                    base_dir / "bin" / "dlengine-linux-arm64",
                    base_dir / "dlengine",
                    base_dir / "bin" / "dlengine",
                ]
            else:
                candidates = [
                    base_dir / "bin" / "dlengine-linux-amd64",
                    base_dir / "dlengine",
                    base_dir / "bin" / "dlengine",
                ]

        for p in candidates:
            if p.exists():
                return p

        # Fallback to current working directory
        if is_windows:
            return Path("dlengine.exe")
        return Path("./bin/dlengine-linux-amd64")

    def _format_timeout(self, timeout: Optional[Union[float, int, str]]) -> Optional[str]:
        if timeout is None:
            return None
        if isinstance(timeout, (int, float)):
            return f"{int(timeout)}s"
        return str(timeout)

    def probe(
        self,
        url: str,
        headers: Optional[Dict[str, str]] = None,
        timeout: Optional[Union[float, int, str]] = None,
        link_timeout: Optional[Union[float, int, str]] = None,
        connect_timeout: Optional[Union[float, int, str]] = None,
        insecure: bool = False,
    ) -> ProbeResult:
        """
        Probe remote target metadata (size, ranges, filename) without downloading.
        """
        cmd = [str(self.bin_path), "-u", url, "--probe-only", "--json"]
        if headers:
            for k, v in headers.items():
                cmd.extend(["-H", f"{k}: {v}"])
        if timeout:
            cmd.extend(["--probe-timeout", self._format_timeout(timeout)])
        if link_timeout:
            cmd.extend(["--link-timeout", self._format_timeout(link_timeout)])
        if connect_timeout:
            cmd.extend(["--connect-timeout", self._format_timeout(connect_timeout)])
        if insecure:
            cmd.append("--insecure")

        proc = subprocess.run(cmd, capture_output=True, text=True, check=False)
        if proc.returncode != 0:
            raise DownloadEngineError(f"Probe failed: {proc.stderr or proc.stdout}")

        for line in proc.stdout.splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                data = json.loads(line)
                if data.get("event") == "probe":
                    return ProbeResult(
                        filename=data.get("filename", ""),
                        total_bytes=data.get("total_bytes", 0),
                        accept_ranges=data.get("accept_ranges", False),
                        status_code=data.get("status_code", 0),
                        final_url=data.get("final_url", ""),
                        total_chunks=data.get("total_chunks", 1),
                        version=data.get("version", "1.1.0"),
                    )
                elif data.get("event") == "error":
                    raise DownloadEngineError(data.get("message", "Unknown probe error"))
            except json.JSONDecodeError:
                continue

        raise DownloadEngineError(f"Failed to parse probe output: {proc.stdout}")

    def download(
        self,
        url: str,
        output_path: Optional[Union[str, Path]] = None,
        concurrency: int = 16,
        chunk_size: str = "8MB",
        stream_mode: bool = False,
        retries: int = 5,
        headers: Optional[Dict[str, str]] = None,
        on_progress: Optional[Callable[[ProgressEvent], None]] = None,
        timeout: Optional[Union[float, int, str]] = None,
        link_timeout: Optional[Union[float, int, str]] = None,
        connect_timeout: Optional[Union[float, int, str]] = None,
        idle_timeout: Optional[Union[float, int, str]] = None,
        insecure: bool = False,
    ) -> DownloadResult:
        """
        Download file directly to disk with multi-part acceleration and real-time progress callbacks.
        """
        cmd = [
            str(self.bin_path),
            "-u", url,
            "-c", str(concurrency),
            "-s", chunk_size,
            "-r", str(retries),
            "--json",
        ]
        if output_path:
            cmd.extend(["-o", str(output_path)])
        if stream_mode:
            cmd.append("--stream")
        if headers:
            for k, v in headers.items():
                cmd.extend(["-H", f"{k}: {v}"])
        if timeout:
            cmd.extend(["--timeout", self._format_timeout(timeout)])
        if link_timeout:
            cmd.extend(["--link-timeout", self._format_timeout(link_timeout)])
        if connect_timeout:
            cmd.extend(["--connect-timeout", self._format_timeout(connect_timeout)])
        if idle_timeout:
            cmd.extend(["--idle-timeout", self._format_timeout(idle_timeout)])
        if insecure:
            cmd.append("--insecure")

        process = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
            universal_newlines=True,
        )

        result: Optional[DownloadResult] = None
        last_error: Optional[str] = None

        try:
            for line in process.stdout:
                line = line.strip()
                if not line:
                    continue
                try:
                    data = json.loads(line)
                    event_type = data.get("event")

                    if event_type == "progress":
                        evt = ProgressEvent(
                            downloaded_bytes=data.get("downloaded_bytes", 0),
                            total_bytes=data.get("total_bytes", 0),
                            percent=data.get("percent", 0.0),
                            speed_bytes_sec=data.get("speed_bytes_sec", 0.0),
                            eta_seconds=data.get("eta_seconds", 0.0),
                            elapsed_seconds=data.get("elapsed_seconds", 0.0),
                            active_workers=data.get("active_workers", 0),
                            completed_chunks=data.get("completed_chunks", 0),
                            total_chunks=data.get("total_chunks", 0),
                        )
                        self.last_progress = evt
                        if on_progress:
                            on_progress(evt)

                    elif event_type == "completed":
                        result = DownloadResult(
                            filename=data.get("filename", ""),
                            dest_path=data.get("dest_path", ""),
                            total_bytes=data.get("total_bytes", 0),
                            elapsed_seconds=data.get("elapsed_seconds", 0.0),
                            avg_speed_bytes_sec=data.get("avg_speed_bytes_sec", 0.0),
                        )

                    elif event_type == "error":
                        last_error = data.get("message", "Unknown error")

                except json.JSONDecodeError:
                    continue

            wait_timeout = float(timeout) if isinstance(timeout, (int, float)) else None
            process.wait(timeout=wait_timeout)
            if process.returncode != 0:
                stderr_output = process.stderr.read()
                raise DownloadEngineError(
                    last_error or f"Process exited with code {process.returncode}: {stderr_output}"
                )

            if result is None:
                raise DownloadEngineError(last_error or "Download finished without completion event")

            return result

        except Exception:
            process.kill()
            raise

    async def download_async(
        self,
        url: str,
        output_path: Optional[Union[str, Path]] = None,
        concurrency: int = 16,
        chunk_size: str = "8MB",
        stream_mode: bool = False,
        retries: int = 5,
        headers: Optional[Dict[str, str]] = None,
        on_progress: Optional[Callable[[ProgressEvent], Any]] = None,
        timeout: Optional[Union[float, int, str]] = None,
        link_timeout: Optional[Union[float, int, str]] = None,
        connect_timeout: Optional[Union[float, int, str]] = None,
        idle_timeout: Optional[Union[float, int, str]] = None,
        insecure: bool = False,
    ) -> DownloadResult:
        """
        Asynchronously download file directly to disk with multi-part acceleration and real-time progress callbacks.
        """
        cmd = [
            str(self.bin_path),
            "-u", url,
            "-c", str(concurrency),
            "-s", chunk_size,
            "-r", str(retries),
            "--json",
        ]
        if output_path:
            cmd.extend(["-o", str(output_path)])
        if stream_mode:
            cmd.append("--stream")
        if headers:
            for k, v in headers.items():
                cmd.extend(["-H", f"{k}: {v}"])
        if timeout:
            cmd.extend(["--timeout", self._format_timeout(timeout)])
        if link_timeout:
            cmd.extend(["--link-timeout", self._format_timeout(link_timeout)])
        if connect_timeout:
            cmd.extend(["--connect-timeout", self._format_timeout(connect_timeout)])
        if idle_timeout:
            cmd.extend(["--idle-timeout", self._format_timeout(idle_timeout)])
        if insecure:
            cmd.append("--insecure")

        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )

        result: Optional[DownloadResult] = None
        last_error: Optional[str] = None

        try:
            while True:
                line_bytes = await proc.stdout.readline()
                if not line_bytes:
                    break
                line = line_bytes.decode("utf-8", errors="replace").strip()
                if not line:
                    continue
                try:
                    data = json.loads(line)
                    event_type = data.get("event")

                    if event_type == "progress":
                        evt = ProgressEvent(
                            downloaded_bytes=data.get("downloaded_bytes", 0),
                            total_bytes=data.get("total_bytes", 0),
                            percent=data.get("percent", 0.0),
                            speed_bytes_sec=data.get("speed_bytes_sec", 0.0),
                            eta_seconds=data.get("eta_seconds", 0.0),
                            elapsed_seconds=data.get("elapsed_seconds", 0.0),
                            active_workers=data.get("active_workers", 0),
                            completed_chunks=data.get("completed_chunks", 0),
                            total_chunks=data.get("total_chunks", 0),
                        )
                        self.last_progress = evt
                        if on_progress:
                            if asyncio.iscoroutinefunction(on_progress):
                                await on_progress(evt)
                            else:
                                on_progress(evt)

                    elif event_type == "completed":
                        result = DownloadResult(
                            filename=data.get("filename", ""),
                            dest_path=data.get("dest_path", ""),
                            total_bytes=data.get("total_bytes", 0),
                            elapsed_seconds=data.get("elapsed_seconds", 0.0),
                            avg_speed_bytes_sec=data.get("avg_speed_bytes_sec", 0.0),
                        )

                    elif event_type == "error":
                        last_error = data.get("message", "Unknown error")

                except json.JSONDecodeError:
                    continue

            await proc.wait()
            if proc.returncode != 0:
                stderr_bytes = await proc.stderr.read()
                stderr_output = stderr_bytes.decode("utf-8", errors="replace")
                raise DownloadEngineError(
                    last_error or f"Process exited with code {proc.returncode}: {stderr_output}"
                )

            if result is None:
                raise DownloadEngineError(last_error or "Download finished without completion event")

            return result

        except Exception:
            proc.kill()
            raise

    def stream(
        self,
        url: str,
        concurrency: int = 16,
        chunk_size: str = "8MB",
        buffer_size: int = 64 * 1024,
        retries: int = 5,
        headers: Optional[Dict[str, str]] = None,
        on_progress: Optional[Callable[[ProgressEvent], None]] = None,
        timeout: Optional[Union[float, int, str]] = None,
        link_timeout: Optional[Union[float, int, str]] = None,
        connect_timeout: Optional[Union[float, int, str]] = None,
        idle_timeout: Optional[Union[float, int, str]] = None,
        insecure: bool = False,
    ) -> Generator[bytes, None, None]:
        """
        Stream file content as raw byte chunks (Generator) while tracking progress in real-time.
        Ideal for piping bytes into FastAPI StreamingResponse, S3, Google Drive, or network sockets.
        """
        cmd = [
            str(self.bin_path),
            "-u", url,
            "-o", "-",        # Direct raw binary bytes to stdout
            "-c", str(concurrency),
            "-s", chunk_size,
            "-r", str(retries),
            "--json",         # Emits JSON events to stderr
        ]
        if headers:
            for k, v in headers.items():
                cmd.extend(["-H", f"{k}: {v}"])
        if timeout:
            cmd.extend(["--timeout", self._format_timeout(timeout)])
        if link_timeout:
            cmd.extend(["--link-timeout", self._format_timeout(link_timeout)])
        if connect_timeout:
            cmd.extend(["--connect-timeout", self._format_timeout(connect_timeout)])
        if idle_timeout:
            cmd.extend(["--idle-timeout", self._format_timeout(idle_timeout)])
        if insecure:
            cmd.append("--insecure")

        process = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            bufsize=0,
        )

        error_msg = []

        # Background thread to read JSON progress events from stderr
        def stderr_reader():
            for line_bytes in process.stderr:
                line = line_bytes.decode("utf-8", errors="replace").strip()
                if not line:
                    continue
                try:
                    data = json.loads(line)
                    event_type = data.get("event")
                    if event_type == "progress":
                        evt = ProgressEvent(
                            downloaded_bytes=data.get("downloaded_bytes", 0),
                            total_bytes=data.get("total_bytes", 0),
                            percent=data.get("percent", 0.0),
                            speed_bytes_sec=data.get("speed_bytes_sec", 0.0),
                            eta_seconds=data.get("eta_seconds", 0.0),
                            elapsed_seconds=data.get("elapsed_seconds", 0.0),
                            active_workers=data.get("active_workers", 0),
                            completed_chunks=data.get("completed_chunks", 0),
                            total_chunks=data.get("total_chunks", 0),
                        )
                        self.last_progress = evt
                        if on_progress:
                            on_progress(evt)
                    elif event_type == "error":
                        error_msg.append(data.get("message", "Stream error"))
                except json.JSONDecodeError:
                    continue

        t = threading.Thread(target=stderr_reader, daemon=True)
        t.start()

        try:
            while True:
                chunk = process.stdout.read(buffer_size)
                if not chunk:
                    break
                yield chunk

            process.wait()
            t.join(timeout=1.0)

            if process.returncode != 0:
                err = " ".join(error_msg) if error_msg else f"Process exited with code {process.returncode}"
                raise DownloadEngineError(f"Stream error: {err}")

        except Exception:
            process.kill()
            raise

    async def stream_async(
        self,
        url: str,
        concurrency: int = 16,
        chunk_size: str = "8MB",
        buffer_size: int = 64 * 1024,
        retries: int = 5,
        headers: Optional[Dict[str, str]] = None,
        on_progress: Optional[Callable[[ProgressEvent], Any]] = None,
        timeout: Optional[Union[float, int, str]] = None,
        link_timeout: Optional[Union[float, int, str]] = None,
        connect_timeout: Optional[Union[float, int, str]] = None,
        idle_timeout: Optional[Union[float, int, str]] = None,
        insecure: bool = False,
    ) -> AsyncGenerator[bytes, None]:
        """
        Asynchronously stream file content as byte chunks (AsyncGenerator) with live progress tracking.
        """
        cmd = [
            str(self.bin_path),
            "-u", url,
            "-o", "-",
            "-c", str(concurrency),
            "-s", chunk_size,
            "-r", str(retries),
            "--json",
        ]
        if headers:
            for k, v in headers.items():
                cmd.extend(["-H", f"{k}: {v}"])
        if timeout:
            cmd.extend(["--timeout", self._format_timeout(timeout)])
        if link_timeout:
            cmd.extend(["--link-timeout", self._format_timeout(link_timeout)])
        if connect_timeout:
            cmd.extend(["--connect-timeout", self._format_timeout(connect_timeout)])
        if idle_timeout:
            cmd.extend(["--idle-timeout", self._format_timeout(idle_timeout)])
        if insecure:
            cmd.append("--insecure")

        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )

        error_msg = []

        async def read_stderr():
            while True:
                line_bytes = await proc.stderr.readline()
                if not line_bytes:
                    break
                line = line_bytes.decode("utf-8", errors="replace").strip()
                if not line:
                    continue
                try:
                    data = json.loads(line)
                    if data.get("event") == "progress":
                        evt = ProgressEvent(
                            downloaded_bytes=data.get("downloaded_bytes", 0),
                            total_bytes=data.get("total_bytes", 0),
                            percent=data.get("percent", 0.0),
                            speed_bytes_sec=data.get("speed_bytes_sec", 0.0),
                            eta_seconds=data.get("eta_seconds", 0.0),
                            elapsed_seconds=data.get("elapsed_seconds", 0.0),
                            active_workers=data.get("active_workers", 0),
                            completed_chunks=data.get("completed_chunks", 0),
                            total_chunks=data.get("total_chunks", 0),
                        )
                        self.last_progress = evt
                        if on_progress:
                            if asyncio.iscoroutinefunction(on_progress):
                                await on_progress(evt)
                            else:
                                on_progress(evt)
                    elif data.get("event") == "error":
                        error_msg.append(data.get("message", "Stream error"))
                except json.JSONDecodeError:
                    continue

        stderr_task = asyncio.create_task(read_stderr())

        try:
            while True:
                chunk = await proc.stdout.read(buffer_size)
                if not chunk:
                    break
                yield chunk

            await proc.wait()
            await stderr_task

            if proc.returncode != 0:
                err = " ".join(error_msg) if error_msg else f"Process exited with code {proc.returncode}"
                raise DownloadEngineError(f"Stream error: {err}")

        except Exception:
            proc.kill()
            raise
