# Go Multi-Part Download & Streaming Engine (v1.1.0)

A high-performance, resilient, and modular multi-part download and streaming engine written in Go. It splits large files into concurrent byte-range parts, bypasses single-connection bandwidth throttling, follows HTTP redirect chains, and supports both zero-overhead direct disk writes (`WriteAt`) and ordered sequential streaming pipelines (`io.Reader` with background prefetching).

---

## ✨ Features

- **🚀 Concurrent Multi-Part Chunking**: Bypasses CDN/server per-connection rate limits (e.g. 2–3 MB/s per connection) by opening 8, 16, or 32 parallel HTTP range streams.
- **⏱️ Comprehensive Timeout Controls (v1.1.0)**:
  - **Link Timeout (`--link-timeout`)**: Guard against dead or stalled links waiting for HTTP response headers (default 30s).
  - **Connection Timeout (`--connect-timeout`)**: TCP dial and TLS handshake timeout (default 15s).
  - **Idle Stall Timeout (`--idle-timeout`)**: Detects frozen/hung socket reads mid-chunk and triggers automatic retries (default 30s).
  - **Probe Timeout (`--probe-timeout`)**: Fast metadata probing timeout (default 15s).
  - **Total Timeout (`--timeout` / `-t`)**: Overall operation timeout (e.g., `10m`, `300s`).
- **🛡️ Resilience & Retries**: Automatic exponential backoff and retries on dropped connections, network timeouts, or 5xx server errors.
- **🔒 Retry Offset & Progress Protection**: Isolated chunk writers ensure retries never write at corrupted file offsets. Failed attempts atomically roll back counted bytes for 100% accurate speed and ETA.
- **🔄 Smart Redirect Following**: Automatically follows multi-hop HTTP 301/302 redirects (e.g., `downloadly.ir` ➔ `dl-downloadly.110.ir.cdn.ir` ➔ `edge12.110.ir.cdn.ir`) and reuses the resolved final URL across all concurrent workers.
- **💾 Dual Output Modes**:
  - **Direct File (`DownloadToFile`)**: Uses `os.File.WriteAt` (`io.NewOffsetWriter`) for direct random-access writing into pre-allocated disk files without temporary chunk files or assembly delays.
  - **Sequential Streaming (`DownloadStream` / `DownloadToWriter`)**: Produces an `io.ReadCloser` that emits bytes in strict sequential order `0..N` while downloading chunks ahead in the background with memory backpressure control.
- **🐧 Linux Ubuntu & GitHub Actions Ready**: Pre-compiled standalone static binaries for Linux (amd64/arm64) and Windows with GitHub Actions workflow template included.
- **🐍 Python SDK (`python/dlengine.py`)**: Synchronous and asynchronous (`download_async`, `stream_async`) high-level controllers with live callbacks.
- **📊 Real-Time CLI Dashboard**: Visual progress bar, instantaneous & smoothed EMA speeds (MB/s), ETA calculation, active worker count, and chunk completion stats.

---

## 📚 Detailed Documentation

- **[CLI & Deployment Documentation (docs/CLI.md)](file:///d:/download-engine/docs/CLI.md)**: Full reference for all CLI flags, timeout tuning, Ubuntu / GitHub Actions CI/CD setup, JSON event schemas, Python SDK, Node.js integration, and performance tuning.
- **[Python Integration Guide (docs/PYTHON_GUIDE.md)](file:///d:/download-engine/docs/PYTHON_GUIDE.md)**: Guide and recipes for `download()`, `download_async()`, `stream()`, `stream_async()`, and `tqdm` integration.

---

## 📦 Pre-Built Binaries

Pre-compiled standalone binaries (`CGO_ENABLED=0`) are available in `./bin/`:

| Binary | Platform | Notes |
|---|---|---|
| `bin/dlengine-linux-amd64` | Linux x86_64 / Ubuntu | **GitHub Actions (`ubuntu-latest`)**, Docker, Cloud VMs |
| `bin/dlengine-linux-arm64` | Linux ARM64 | AWS Graviton, Raspberry Pi, Apple Silicon containers |
| `bin/dlengine-windows-amd64.exe` | Windows x64 | Windows 10/11/Server CLI |

To recompile all binaries at once:
```powershell
.\build.bat     # On Windows
./build.sh      # On Linux/macOS
```

---

## 💻 CLI Usage

### Basic Usage
Download using default settings (16 workers, 8MB chunk size, auto-detected filename):
```powershell
.\dlengine.exe -u "https://dl.downloadly.ir/Files/Elearning/The_Gnomon_Workshop_3D_WEAPON_DESIGN_VR_WORKFLOW_2024-6.part5_Downloadly.ir.rar?nocache=1788171959"
```

### High-Speed 32 Workers with Custom Timeouts
```powershell
.\dlengine.exe -u "<URL>" -c 32 -s 8MB -o "my_file.rar" --link-timeout 15s --idle-timeout 20s -t 15m
```

### Stream Pipeline Mode
Stream bytes sequentially via the prefetching `io.Reader` pipeline:
```powershell
.\dlengine.exe -u "<URL>" --stream -o "streamed_file.rar"
```

### Pipe Binary Stream to Stdout (e.g. into FFmpeg, Cloud S3, or Tar)
```powershell
.\dlengine.exe -u "<URL>" -o - | tar -xvf -
```

### Machine-Readable JSON Mode (for Python / Node / CI)
```powershell
.\dlengine.exe -u "<URL>" -c 16 --json
```

### Check Version
```powershell
.\dlengine.exe -v
```

---

## 🐍 Python SDK Example

A lightweight Python SDK is provided in `python/dlengine.py`:

```python
from dlengine import DLEngine, ProgressEvent

engine = DLEngine() # Auto-detects Windows or Linux binary

def on_progress(evt: ProgressEvent):
    print(f"\rProgress: {evt.percent:.1f}% | Speed: {evt.speed_mb_s:.2f} MB/s | ETA: {evt.eta_seconds:.0f}s", end="")

result = engine.download(
    url="https://dl.downloadly.ir/...rar",
    output_path="downloaded_file.rar",
    concurrency=16,
    link_timeout="20s",
    idle_timeout="30s",
    timeout="10m",
    on_progress=on_progress,
)
print(f"\nCompleted in {result.elapsed_seconds:.2f}s! Avg Speed: {result.avg_speed_mb_s:.2f} MB/s")
```

### Asynchronous Download (FastAPI / Celery / Bots)

```python
import asyncio
from dlengine import DLEngine, ProgressEvent

async def main():
    engine = DLEngine()
    
    async def async_progress(evt: ProgressEvent):
        print(f"Async Progress: {evt.percent:.1f}% | Speed: {evt.speed_mb_s:.2f} MB/s")

    result = await engine.download_async(
        url="https://example.com/largefile.rar",
        output_path="async_output.rar",
        concurrency=16,
        link_timeout="30s",
        on_progress=async_progress,
    )
    print("Downloaded:", result.dest_path)

asyncio.run(main())
```

---

## 📦 Go Package API Usage

```go
package main

import (
	"context"
	"fmt"
	"time"

	"download-engine/pkg/engine"
)

func main() {
	url := "https://example.com/largefile.zip"

	eng := engine.New(
		engine.WithConcurrency(16),
		engine.WithChunkSize(8 * 1024 * 1024), // 8MB chunks
		engine.WithMaxRetries(5),
		engine.WithLinkTimeout(30 * time.Second),     // Link / response header timeout
		engine.WithConnectTimeout(15 * time.Second),  // TCP / TLS dial timeout
		engine.WithIdleTimeout(30 * time.Second),     // Per-chunk read stall timeout
		engine.WithTimeout(15 * time.Minute),         // Overall download timeout
		engine.WithProgressCallback(func(s engine.ProgressSnapshot) {
			fmt.Printf("\rProgress: %.1f%% | Speed: %.2f MB/s | ETA: %v",
				s.Percent, s.SpeedBytesPerSec/(1024*1024), s.ETA)
		}, 200*time.Millisecond),
	)

	ctx := context.Background()

	// Mode A: Direct download to local file
	info, err := eng.DownloadToFile(ctx, url, "output.zip")
	if err != nil {
		panic(err)
	}
	fmt.Printf("\nSaved %s (%d bytes)\n", info.Filename, info.ContentLength)

	// Mode B: Stream via io.ReadCloser
	rc, info, err := eng.DownloadStream(ctx, url)
	if err != nil {
		panic(err)
	}
	defer rc.Close()
}
```
