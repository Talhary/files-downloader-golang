package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// ProgressTracker maintains live download statistics.
type ProgressTracker struct {
	totalBytes      int64
	downloadedBytes atomic.Int64
	activeWorkers   atomic.Int32
	totalChunks     int
	completedChunks atomic.Int32

	startTime       time.Time
	lastSampleTime  time.Time
	lastSampleBytes int64
	smoothedSpeed   float64

	callback ProgressCallback
	interval time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	done   bool
}

// NewProgressTracker creates a new progress tracker.
func NewProgressTracker(totalBytes int64, totalChunks int, cb ProgressCallback, interval time.Duration) *ProgressTracker {
	if interval <= 0 {
		interval = DefaultProgressInterval
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()

	pt := &ProgressTracker{
		totalBytes:      totalBytes,
		totalChunks:     totalChunks,
		startTime:       now,
		lastSampleTime:  now,
		lastSampleBytes: 0,
		callback:        cb,
		interval:        interval,
		ctx:             ctx,
		cancel:          cancel,
	}

	if cb != nil {
		pt.wg.Add(1)
		go pt.runTicker()
	}

	return pt
}

// AddBytes atomically records newly downloaded bytes.
func (pt *ProgressTracker) AddBytes(n int64) {
	if n > 0 {
		pt.downloadedBytes.Add(n)
	}
}

// WorkerStarted increments active worker count.
func (pt *ProgressTracker) WorkerStarted() {
	pt.activeWorkers.Add(1)
}

// WorkerFinished decrements active worker count.
func (pt *ProgressTracker) WorkerFinished() {
	pt.activeWorkers.Add(-1)
}

// ChunkCompleted increments completed chunks.
func (pt *ProgressTracker) ChunkCompleted() {
	pt.completedChunks.Add(1)
}

// Snapshot returns the current progress snapshot.
func (pt *ProgressTracker) Snapshot(err error) ProgressSnapshot {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	downloaded := pt.downloadedBytes.Load()
	if pt.totalBytes > 0 && downloaded > pt.totalBytes {
		downloaded = pt.totalBytes
	}

	var percent float64
	if pt.totalBytes > 0 {
		percent = (float64(downloaded) / float64(pt.totalBytes)) * 100.0
		if percent > 100.0 {
			percent = 100.0
		}
	}

	elapsed := time.Since(pt.startTime)
	if elapsed < 1*time.Millisecond {
		elapsed = 1 * time.Millisecond
	}

	speed := pt.smoothedSpeed
	if speed <= 0 && elapsed.Seconds() > 0 {
		speed = float64(downloaded) / elapsed.Seconds()
	}

	var eta time.Duration
	if pt.totalBytes > 0 && speed > 0 {
		remainingBytes := pt.totalBytes - downloaded
		if remainingBytes > 0 {
			eta = time.Duration(float64(remainingBytes)/speed) * time.Second
		}
	}

	return ProgressSnapshot{
		TotalBytes:       pt.totalBytes,
		DownloadedBytes:  downloaded,
		Percent:          percent,
		SpeedBytesPerSec: speed,
		ETA:              eta,
		Elapsed:          elapsed,
		ActiveWorkers:    int(pt.activeWorkers.Load()),
		TotalChunks:      pt.totalChunks,
		CompletedChunks:  int(pt.completedChunks.Load()),
		Done:             pt.done,
		Err:              err,
	}
}

func (pt *ProgressTracker) runTicker() {
	defer pt.wg.Done()
	ticker := time.NewTicker(pt.interval)
	defer ticker.Stop()

	for {
		select {
		case <-pt.ctx.Done():
			return
		case now := <-ticker.C:
			pt.updateSpeed(now)
			if pt.callback != nil {
				pt.callback(pt.Snapshot(nil))
			}
		}
	}
}

func (pt *ProgressTracker) updateSpeed(now time.Time) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	duration := now.Sub(pt.lastSampleTime).Seconds()
	if duration <= 0 {
		return
	}

	currentBytes := pt.downloadedBytes.Load()
	deltaBytes := currentBytes - pt.lastSampleBytes
	if deltaBytes < 0 {
		deltaBytes = 0
	}

	instantSpeed := float64(deltaBytes) / duration

	// Exponential Moving Average (EMA) with alpha=0.3 for smooth display
	if pt.smoothedSpeed <= 0 {
		pt.smoothedSpeed = instantSpeed
	} else {
		alpha := 0.3
		pt.smoothedSpeed = alpha*instantSpeed + (1.0-alpha)*pt.smoothedSpeed
	}

	pt.lastSampleTime = now
	pt.lastSampleBytes = currentBytes
}

// Stop halts the progress ticker and sends a final snapshot.
func (pt *ProgressTracker) Stop(err error) {
	pt.mu.Lock()
	pt.done = true
	pt.mu.Unlock()

	pt.cancel()
	pt.wg.Wait()

	if pt.callback != nil {
		pt.callback(pt.Snapshot(err))
	}
}
