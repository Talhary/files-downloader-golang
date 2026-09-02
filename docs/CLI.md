# Multi-Part Download Engine CLI Documentation

The **Multi-Part Download Engine (`dlengine`)** is a standalone, high-performance binary written in Go. It accelerates large file transfers by opening concurrent HTTP Range connections, bypassing CDN per-stream throttling, following 302/301 redirect chains automatically, and writing directly to disk or streaming via ordered pipelines.

---

## 📑 Table of Contents

1. [Pre-built Binaries](#-pre-built-binaries)
2. [CLI Flags & Options](#-cli-flags--options)
3. [Running on Linux Ubuntu & GitHub Actions](#-running-on-linux-ubuntu--github-actions)
4. [Machine-Readable JSON Output (`--json`)](#-machine-readable-json-output---json)
5. [Python Integration & SDK](#-python-integration--sdk)
6. [Node.js / TypeScript Integration](#-nodejs--typescript-integration)
7. [Performance Tuning Guide](#-performance-tuning-guide)

---

## 📦 Pre-Built Binaries

Pre-compiled, static binaries with **zero runtime dependencies** (`CGO_ENABLED=0`) are located in `./bin/`:

| Binary | Target Platform / OS | Use Case |
|---|---|---|
| `bin/dlengine-linux-amd64` | Linux x86_64 / Ubuntu / Debian / Alpine | **GitHub Actions (`ubuntu-latest`)**, Docker, Cloud VMs |
| `bin/dlengine-linux-arm64` | Linux ARM64 / aarch64 | AWS Graviton, Raspberry Pi 4/5, Apple Silicon Docker |
| `bin/dlengine-windows-amd64.exe` | Windows 10 / 11 / Server (x64) | Local Windows CLI, PowerShell scripts |

To compile manually:
```powershell
# Windows PowerShell
.\build.bat

# Linux / macOS
chmod +x build.sh && ./build.sh
```

---

## 🚀 CLI Flags & Options

```text
dlengine [flags]
```

### Complete Flag Reference

| Flag | Short | Default | Description |
|---|---|---|---|
| `--url` | `-u` | *(Target URL)* | The remote URL to download. |
| `--output` | `-o` | `""` | Destination path (can be a target filename or directory). If empty, auto-extracts remote filename. |
| `--concurrency` | `-c` | `16` | Number of parallel worker threads / HTTP connections. |
| `--chunk-size` | `-s` | `8MB` | Size of each chunk/part (`4MB`, `8MB`, `16MB`, `32MB`, `64MB`). |
| `--stream` | | `false` | Enables ordered sequential stream pipeline with background prefetching. |
| `--retries` | `-r` | `5` | Maximum retry attempts per chunk on dropped connections or 5xx errors. |
| `--json` | | `false` | Emits newline-delimited JSON (NDJSON) events for Python/Node/CI pipelines. |
| `--probe-only` | | `false` | Probes remote URL metadata (size, range support, final URL) and exits without downloading. |
| `--silent` | | `false` | Suppresses console progress output (only reports errors). |
| `--header` | `-H` | | Custom HTTP request header in `"Key: Value"` format (can be specified multiple times). |

### CLI Examples

#### 1. Download with 16 Workers (Default)
```bash
./bin/dlengine-linux-amd64 -u "https://dl.downloadly.ir/Files/Elearning/The_Gnomon_Workshop_3D_WEAPON_DESIGN_VR_WORKFLOW_2024-6.part5_Downloadly.ir.rar?nocache=1788171959"
```

#### 2. Maximize Line Saturation with 32 Workers & Custom Output Path
```bash
./bin/dlengine-linux-amd64 -u "<URL>" -c 32 -s 8MB -o "/data/downloads/custom_name.rar"
```

#### 3. Stream Mode (Sequential `io.Reader` with Prefetching)
```bash
./bin/dlengine-linux-amd64 -u "<URL>" --stream -o "streamed_file.rar"
```

#### 4. Pass Custom Headers (Auth / Cookies / Referer)
```bash
./bin/dlengine-linux-amd64 -u "<URL>" -H "Authorization: Bearer my_token" -H "Referer: https://example.com"
```

---

## 🐧 Running on Linux Ubuntu & GitHub Actions

### Direct Execution in Ubuntu / Debian
```bash
# Make binary executable
chmod +x ./bin/dlengine-linux-amd64

# Run download
./bin/dlengine-linux-amd64 -u "<URL>" -c 16 -o "downloaded.rar"
```

### GitHub Actions Workflow Example

Create `.github/workflows/download.yml`:

```yaml
name: Multi-Part Fast Downloader

on:
  workflow_dispatch:
    inputs:
      download_url:
        description: "Target URL to download"
        required: true
        default: "https://dl.downloadly.ir/Files/Elearning/The_Gnomon_Workshop_3D_WEAPON_DESIGN_VR_WORKFLOW_2024-6.part5_Downloadly.ir.rar?nocache=1788171959"
      concurrency:
        description: "Concurrent connections"
        required: false
        default: "32"
      output_filename:
        description: "Output filename"
        required: false
        default: "downloaded_file.rar"

jobs:
  download:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout Code
        uses: actions/checkout@v4

      - name: Make Binary Executable
        run: chmod +x ./bin/dlengine-linux-amd64

      - name: Download via Multi-Part Engine
        run: |
          ./bin/dlengine-linux-amd64 \
            -u "${{ github.event.inputs.download_url }}" \
            -c ${{ github.event.inputs.concurrency }} \
            -s 8MB \
            -o "${{ github.event.inputs.output_filename }}"

      - name: Verify Downloaded File
        run: ls -lh "${{ github.event.inputs.output_filename }}"
```

---

## 🤖 Machine-Readable JSON Output (`--json`)

When `--json` is enabled, the CLI outputs **Newline-Delimited JSON (NDJSON)** to stdout. This makes it effortless to parse in any programming language (Python, Node.js, C#, Rust, Bash).

### Event Schemas

#### 1. `probe` Event (Emitted immediately after remote inspection)
```json
{
  "event": "probe",
  "filename": "The_Gnomon_Workshop_3D_WEAPON_DESIGN_VR_WORKFLOW_2024-6.part5_Downloadly.ir.rar",
  "total_bytes": 1073741824,
  "total_chunks": 128,
  "accept_ranges": true,
  "status_code": 200,
  "final_url": "https://edge12.110.ir.cdn.ir/Files/Elearning/..."
}
```

#### 2. `progress` Event (Emitted periodically during download)
```json
{
  "event": "progress",
  "downloaded_bytes": 524288000,
  "total_bytes": 1073741824,
  "percent": 48.82,
  "speed_bytes_sec": 29845120.5,
  "eta_seconds": 18.4,
  "elapsed_seconds": 17.5,
  "active_workers": 16,
  "completed_chunks": 62,
  "total_chunks": 128
}
```

#### 3. `completed` Event (Emitted when download finishes)
```json
{
  "event": "completed",
  "filename": "The_Gnomon_Workshop_3D_WEAPON_DESIGN_VR_WORKFLOW_2024-6.part5_Downloadly.ir.rar",
  "dest_path": "./The_Gnomon_Workshop_3D_WEAPON_DESIGN_VR_WORKFLOW_2024-6.part5_Downloadly.ir.rar",
  "total_bytes": 1073741824,
  "elapsed_seconds": 35.8,
  "avg_speed_bytes_sec": 29992788.3
}
```

#### 4. `error` Event
```json
{
  "event": "error",
  "message": "chunk 4 failed after 5 retries: connection reset by peer"
}
```

---

## 🐍 Python Integration & SDK

The repository includes a ready-to-use Python SDK located at `python/dlengine.py`.

### Installation / Usage in Python

```python
from dlengine import DLEngine, ProgressEvent

# 1. Initialize Downloader (auto-detects Windows/Linux binary)
engine = DLEngine()

# 2. Probe metadata
info = engine.probe("https://example.com/largefile.rar")
print(f"Filename: {info.filename}, Size: {info.total_mb:.2f} MB")

# 3. Download with live progress callback
def on_progress(evt: ProgressEvent):
    print(f"\rProgress: {evt.percent:.1f}% | Speed: {evt.speed_mb_s:.2f} MB/s | ETA: {evt.eta_seconds:.0f}s", end="")

result = engine.download(
    url="https://example.com/largefile.rar",
    output_path="custom_destination.rar",
    concurrency=16,
    chunk_size="8MB",
    on_progress=on_progress,
)

print(f"\nDownload finished in {result.elapsed_seconds:.2f}s! Avg Speed: {result.avg_speed_mb_s:.2f} MB/s")
```

### Asyncio Python Example (FastAPI / Celery / Discord Bots)

```python
import asyncio
from dlengine import DLEngine

async def main():
    engine = DLEngine()
    
    async def progress_hook(evt):
        print(f"Async Progress: {evt.percent:.1f}% ({evt.downloaded_mb:.1f} MB)")

    result = await engine.download_async(
        url="https://example.com/largefile.rar",
        output_path="async_output.rar",
        concurrency=16,
        on_progress=progress_hook,
    )
    print("Downloaded:", result.dest_path)

asyncio.run(main())
```

---

## 🌐 Node.js / TypeScript Integration

You can easily execute and stream progress in Node.js or TypeScript using `child_process`:

```typescript
import { spawn } from "child_process";
import * as readline from "readline";

function downloadFile(url: string, destPath: string, concurrency = 16): Promise<void> {
  return new Promise((resolve, reject) => {
    const binPath = process.platform === "win32" ? "./dlengine.exe" : "./bin/dlengine-linux-amd64";
    
    const proc = spawn(binPath, [
      "-u", url,
      "-o", destPath,
      "-c", concurrency.toString(),
      "--json",
    ]);

    const rl = readline.createInterface({ input: proc.stdout });

    rl.on("line", (line) => {
      try {
        const event = JSON.parse(line.trim());
        if (event.event === "progress") {
          const speedMB = (event.speed_bytes_sec / (1024 * 1024)).toFixed(2);
          process.stdout.write(`\r[Node] ${event.percent.toFixed(1)}% | ${speedMB} MB/s | ETA: ${event.eta_seconds.toFixed(0)}s`);
        } else if (event.event === "completed") {
          console.log(`\n[Node] Download complete: ${event.dest_path}`);
        }
      } catch (e) {}
    });

    proc.on("close", (code) => {
      if (code === 0) resolve();
      else reject(new Error(`Download failed with exit code ${code}`));
    });
  });
}
```

---

## ⚡ Performance Tuning Guide

| Scenario | Concurrency (`-c`) | Chunk Size (`-s`) | Rationale |
|---|---|---|---|
| **Throttled CDNs (e.g. 2–3 MB/s per connection)** | `16` – `32` | `8MB` | Slices the file into 64–128 parts and saturates line bandwidth by opening many parallel streams. |
| **High Gigabit Connections (1 Gbps+)** | `32` – `64` | `16MB` – `32MB` | Keeps socket queues full and utilizes multiple CPU cores for I/O writing. |
| **Constrained RAM / Memory Environments** | `8` – `16` | `4MB` | Reduces in-flight memory footprint while maintaining strong concurrent speed. |
| **High Latency / Distant International Routes** | `24` – `32` | `8MB` | Multiple TCP connections overcome BDP (Bandwidth-Delay Product) and TCP window scaling limits. |
