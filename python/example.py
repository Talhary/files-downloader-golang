"""
Example script demonstrating how to control the Go Download Engine from Python.
"""

import sys
from pathlib import Path

# Force UTF-8 on Windows terminals if supported
if hasattr(sys.stdout, "reconfigure"):
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass

# Add python SDK to sys.path
sys.path.insert(0, str(Path(__file__).parent))

from dlengine import DLEngine, ProgressEvent


def on_download_progress(evt: ProgressEvent):
    """Custom progress callback invoked in real-time."""
    bar_width = 25
    filled = int((evt.percent / 100.0) * bar_width)
    bar = "=" * filled + "-" * (bar_width - filled)
    
    print(
        f"\r[Python] [{bar}] {evt.percent:5.1f}% | "
        f"Speed: {evt.speed_mb_s:6.2f} MB/s | "
        f"ETA: {int(evt.eta_seconds):3d}s | "
        f"Downloaded: {evt.downloaded_mb:.1f}/{evt.total_mb:.1f} MB | "
        f"Workers: {evt.active_workers} | "
        f"Parts: {evt.completed_chunks}/{evt.total_chunks}",
        end="",
        flush=True,
    )


def main():
    target_url = (
        "https://dl.downloadly.ir/Files/Elearning/"
        "The_Gnomon_Workshop_3D_WEAPON_DESIGN_VR_WORKFLOW_2024-6.part5_Downloadly.ir.rar?nocache=1788171959"
    )

    print("=" * 70)
    print("Python Controller for High-Speed Go Download Engine")
    print("=" * 70)

    # Initialize Downloader (auto-detects Windows/Linux binary)
    engine = DLEngine()
    print(f"Loaded Engine Binary: {engine.bin_path}")

    # 1. Probe URL metadata
    print("\nProbing remote URL...")
    info = engine.probe(target_url)
    print(f"  * Filename:     {info.filename}")
    print(f"  * Total Size:   {info.total_mb:.2f} MB ({info.total_bytes:,} bytes)")
    print(f"  * Range Capable: {info.accept_ranges}")
    print(f"  * Parts Count:  {info.total_chunks}")
    print(f"  * Final URL:    {info.final_url[:60]}...")

    # 2. Download to specific path with 16 workers
    # (Uncomment below to run live download)
    # output_file = "my_custom_download.rar"
    # print(f"\nDownloading {info.filename} -> {output_file}...")
    # result = engine.download(
    #     url=target_url,
    #     output_path=output_file,
    #     concurrency=16,
    #     chunk_size="8MB",
    #     on_progress=on_download_progress,
    # )
    # print(f"\n\nFinished in {result.elapsed_seconds:.2f}s! Avg Speed: {result.avg_speed_mb_s:.2f} MB/s")


if __name__ == "__main__":
    main()
