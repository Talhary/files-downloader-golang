package engine

import (
	"fmt"
	"sync"
)

// Chunk represents a discrete byte range of the target file.
type Chunk struct {
	Index      int   `json:"index"`
	Start      int64 `json:"start"`
	End        int64 `json:"end"`
	Downloaded int64 `json:"downloaded"`
	Completed  bool  `json:"completed"`
	Retries    int   `json:"retries"`
	mu         sync.Mutex
}

// Size returns the total byte size of this chunk.
func (c *Chunk) Size() int64 {
	return c.End - c.Start + 1
}

// RangeHeader returns the standard HTTP Range header value for this chunk.
func (c *Chunk) RangeHeader() string {
	return fmt.Sprintf("bytes=%d-%d", c.Start, c.End)
}

// SetDownloaded atomically updates the downloaded bytes for this chunk.
func (c *Chunk) SetDownloaded(bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if bytes < 0 {
		bytes = 0
	}
	if bytes > c.Size() {
		bytes = c.Size()
	}
	c.Downloaded = bytes
}

// GetDownloaded returns the downloaded bytes for this chunk safely.
func (c *Chunk) GetDownloaded() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Downloaded
}

// MarkCompleted marks this chunk as completed.
func (c *Chunk) MarkCompleted() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Downloaded = c.Size()
	c.Completed = true
}

// IsCompleted returns whether the chunk is finished.
func (c *Chunk) IsCompleted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Completed
}

// CalculateChunks divides the file size into a list of chunks based on chunk size and concurrency.
func CalculateChunks(totalSize int64, preferredChunkSize int64, concurrency int) []*Chunk {
	if totalSize <= 0 {
		return nil
	}

	chunkSize := preferredChunkSize
	if chunkSize <= 0 {
		// Calculate dynamic chunk size
		if concurrency <= 0 {
			concurrency = DefaultConcurrency
		}
		// Slices the file into at least concurrency * 4 parts (minimum 2MB, max 64MB)
		dynamicSize := totalSize / int64(concurrency*4)
		if dynamicSize < 2*1024*1024 {
			dynamicSize = 2 * 1024 * 1024 // 2 MB min
		}
		if dynamicSize > 64*1024*1024 {
			dynamicSize = 64 * 1024 * 1024 // 64 MB max
		}
		chunkSize = dynamicSize
	}

	if chunkSize > totalSize {
		chunkSize = totalSize
	}

	var chunks []*Chunk
	var start int64 = 0
	index := 0

	for start < totalSize {
		end := start + chunkSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}

		chunks = append(chunks, &Chunk{
			Index: index,
			Start: start,
			End:   end,
		})

		start = end + 1
		index++
	}

	return chunks
}
