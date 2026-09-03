# Python Integration Guide for Multi-Part Download Engine (v1.1.0)

This guide demonstrates how to control the Go Multi-Part Download Engine directly from Python on **Linux (Ubuntu)**, **macOS**, and **Windows**.

---

## 🚀 1. Quick Setup in Python

### Step 1: Clone or Download the Binary
You can either clone this repo or download the standalone Linux binary directly from GitHub Releases:

```bash
# On Ubuntu / Linux
wget https://github.com/Talhary/files-downloader-golang/releases/latest/download/dlengine-linux-amd64 -O dlengine
chmod +x dlengine
```

### Step 2: Import `dlengine.py`

Place `dlengine.py` (from the `python/` folder) into your project.

---

## 💻 2. Python Code Examples

### Example A: Basic Multi-Part Download with Progress Bar

```python
from dlengine import DLEngine, ProgressEvent

# Initialize engine (auto-locates binary or specify path: DLEngine("./dlengine"))
engine = DLEngine()

# 1. Probe file information with link timeout
target_url = "https://dl.downloadly.ir/Files/Elearning/The_Gnomon_Workshop_3D_WEAPON_DESIGN_VR_WORKFLOW_2024-6.part5_Downloadly.ir.rar?nocache=1788171959"
info = engine.probe(target_url, link_timeout="15s")

print(f"Target File: {info.filename}")
print(f"Size:        {info.total_mb:.2f} MB ({info.total_bytes:,} bytes)")
print(f"Ranges:      {info.accept_ranges}")
print(f"Total Parts: {info.total_chunks}")

# 2. Define custom live progress callback
def on_progress(evt: ProgressEvent):
    print(
        f"\rProgress: {evt.percent:5.1f}% | "
        f"Speed: {evt.speed_mb_s:6.2f} MB/s | "
        f"ETA: {int(evt.eta_seconds):3d}s | "
        f"Downloaded: {evt.downloaded_mb:.1f}/{evt.total_mb:.1f} MB | "
        f"Workers: {evt.active_workers} | "
        f"Parts: {evt.completed_chunks}/{evt.total_chunks}",
        end="",
        flush=True,
    )

# 3. Start high-speed multi-part download with timeouts
result = engine.download(
    url=target_url,
    output_path="downloaded_file.rar",  # Desired local destination
    concurrency=16,                     # Number of parallel streams
    chunk_size="8MB",                   # Chunk size per worker
    link_timeout="30s",                 # HTTP response header timeout
    idle_timeout="30s",                 # Per-chunk read stall timeout
    timeout="15m",                      # Overall download timeout
    on_progress=on_progress,
)

print(f"\n\nDownload completed in {result.elapsed_seconds:.2f}s!")
print(f"Saved to: {result.dest_path}")
print(f"Average Speed: {result.avg_speed_mb_s:.2f} MB/s")
```

---

### Example B: Asynchronous Download (`asyncio` / FastAPI / Telegram / Discord Bots)

```python
import asyncio
from dlengine import DLEngine, ProgressEvent

async def download_file():
    engine = DLEngine()
    
    async def async_progress(evt: ProgressEvent):
        print(f"[Async] {evt.percent:.1f}% - Speed: {evt.speed_mb_s:.2f} MB/s - ETA: {evt.eta_seconds:.0f}s")

    result = await engine.download_async(
        url="https://dl.downloadly.ir/...rar",
        output_path="async_download.rar",
        concurrency=32,
        link_timeout="20s",
        idle_timeout="30s",
        on_progress=async_progress,
    )
    print(f"Finished: {result.dest_path}")

asyncio.run(download_file())
```

---

### Example C: In-Memory Sequential Streaming (`Generator` / `AsyncGenerator`)

Ideal for piping bytes directly into web responses (FastAPI `StreamingResponse`), cloud storage uploads (S3, GCS), or media players without saving to local disk:

```python
from dlengine import DLEngine

engine = DLEngine()
target_url = "https://example.com/movie.mp4"

# Stream synchronous chunks
for chunk in engine.stream(target_url, concurrency=16, link_timeout="20s"):
    # send chunk to network socket or S3
    pass
```

---

### Example D: Integration with `tqdm` Progress Bar

```python
from tqdm import tqdm
from dlengine import DLEngine, ProgressEvent

engine = DLEngine()
url = "https://dl.downloadly.ir/...rar"

info = engine.probe(url, link_timeout="15s")
pbar = tqdm(total=info.total_bytes, unit='B', unit_scale=True, desc=info.filename)

last_bytes = 0
def update_tqdm(evt: ProgressEvent):
    global last_bytes
    delta = evt.downloaded_bytes - last_bytes
    if delta > 0:
        pbar.update(delta)
        last_bytes = evt.downloaded_bytes

result = engine.download(url, concurrency=16, link_timeout="30s", on_progress=update_tqdm)
pbar.close()
print("Done!")
```

---

## ⚙️ 3. Parameters Explained

| Parameter | Type | Default | Description |
|---|---|---|---|
| `url` | `str` | *Required* | The direct or redirecting download URL. |
| `output_path` | `str` or `Path` | `None` | Local destination file or directory. If `None`, uses original filename. |
| `concurrency` | `int` | `16` | Number of simultaneous HTTP range workers (e.g. `16`, `32`, `64`). |
| `chunk_size` | `str` | `"8MB"` | Size of each chunk (`"4MB"`, `"8MB"`, `"16MB"`, `"32MB"`). |
| `stream_mode` | `bool` | `False` | Sequential stream pipeline mode (`io.Reader` prefetching). |
| `retries` | `int` | `5` | Maximum retry attempts per chunk on dropped connections. |
| `link_timeout` | `str` or `float` | `None` | HTTP response header timeout (e.g. `"30s"`, `30`). |
| `connect_timeout` | `str` or `float` | `None` | TCP dial and TLS handshake timeout (e.g. `"15s"`, `15`). |
| `idle_timeout` | `str` or `float` | `None` | Per-chunk read stall timeout (e.g. `"30s"`, `30`). |
| `timeout` | `str` or `float` | `None` | Overall execution timeout (e.g. `"15m"`, `900`). |
| `insecure` | `bool` | `False` | Allow insecure TLS certificates (InsecureSkipVerify). |
| `headers` | `dict` | `None` | Custom HTTP headers (e.g. `{"Authorization": "Bearer ...", "User-Agent": "..."}`). |
| `on_progress` | `Callable` | `None` | Callback receiving `ProgressEvent` dataclass every ~150ms. |
