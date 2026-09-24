package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Checkpoint captures the persistent state of a download to support Pause and Resume.
type Checkpoint struct {
	URL           string        `json:"url"`
	FinalURL      string        `json:"final_url"`
	Filename      string        `json:"filename"`
	ContentLength int64         `json:"content_length"`
	ETag          string        `json:"etag"`
	LastModified  string        `json:"last_modified"`
	AcceptRanges  bool          `json:"accept_ranges"`
	ChunkSize     int64         `json:"chunk_size"`
	TotalChunks   int           `json:"total_chunks"`
	CompletedSize int64         `json:"completed_size"`
	Chunks        []*ChunkState `json:"chunks"`
	mu            sync.Mutex    `json:"-"`
}

// ChunkState represents the persistent progress of a single chunk.
type ChunkState struct {
	Index      int   `json:"index"`
	Start      int64 `json:"start"`
	End        int64 `json:"end"`
	Downloaded int64 `json:"downloaded"`
	Completed  bool  `json:"completed"`
}

// GetDefaultStatePath returns the path where the checkpoint file is stored.
func GetDefaultStatePath(destPath string) string {
	return destPath + ".dlstate.json"
}

// NewCheckpoint creates a new Checkpoint record from probe info and chunk layout.
func NewCheckpoint(info *FileInfo, chunks []*Chunk, chunkSize int64) *Checkpoint {
	lastModStr := ""
	if !info.LastModified.IsZero() {
		lastModStr = info.LastModified.Format(time.RFC3339)
	}

	cp := &Checkpoint{
		URL:           info.URL,
		FinalURL:      info.FinalURL,
		Filename:      info.Filename,
		ContentLength: info.ContentLength,
		ETag:          info.ETag,
		LastModified:  lastModStr,
		AcceptRanges:  info.AcceptRanges,
		ChunkSize:     chunkSize,
		TotalChunks:   len(chunks),
		Chunks:        make([]*ChunkState, len(chunks)),
	}

	for i, ch := range chunks {
		cp.Chunks[i] = &ChunkState{
			Index:      ch.Index,
			Start:      ch.Start,
			End:        ch.End,
			Downloaded: ch.GetDownloaded(),
			Completed:  ch.IsCompleted(),
		}
	}
	return cp
}

// SyncFromChunks updates the checkpoint state with current chunk statuses.
func (cp *Checkpoint) SyncFromChunks(chunks []*Chunk) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	var totalCompleted int64
	for i, ch := range chunks {
		if i < len(cp.Chunks) {
			cp.Chunks[i].Downloaded = ch.GetDownloaded()
			cp.Chunks[i].Completed = ch.IsCompleted()
			totalCompleted += cp.Chunks[i].Downloaded
		}
	}
	cp.CompletedSize = totalCompleted
}

// Save writes the checkpoint atomicaly to statePath using a temp file.
func (cp *Checkpoint) Save(statePath string) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling checkpoint: %w", err)
	}

	dir := filepath.Dir(statePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating checkpoint directory: %w", err)
		}
	}

	tmpPath := statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing checkpoint tmp: %w", err)
	}

	if err := os.Rename(tmpPath, statePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming checkpoint: %w", err)
	}
	return nil
}

// LoadCheckpoint reads a checkpoint file from disk.
func LoadCheckpoint(statePath string) (*Checkpoint, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("unmarshaling checkpoint: %w", err)
	}
	return &cp, nil
}

// RemoveCheckpoint deletes the state file when download completes.
func RemoveCheckpoint(statePath string) {
	_ = os.Remove(statePath)
	_ = os.Remove(statePath + ".tmp")
}

// IsResumeValid checks if remote server metadata matches stored checkpoint.
func (cp *Checkpoint) IsResumeValid(freshInfo *FileInfo) bool {
	if freshInfo == nil || !freshInfo.AcceptRanges || cp.ContentLength <= 0 || cp.ContentLength != freshInfo.ContentLength {
		return false
	}
	storedETag := strings.Trim(cp.ETag, `"`)
	freshETag := strings.Trim(freshInfo.ETag, `"`)
	if storedETag != "" && storedETag != freshETag {
		return false
	}
	if cp.LastModified != "" && !freshInfo.LastModified.IsZero() && cp.LastModified != freshInfo.LastModified.Format(time.RFC3339) {
		return false
	}
	if len(cp.Chunks) == 0 || cp.TotalChunks != len(cp.Chunks) {
		return false
	}
	var expectedStart int64
	for i, state := range cp.Chunks {
		if state == nil || state.Index != i || state.Start != expectedStart || state.End < state.Start || state.End >= cp.ContentLength {
			return false
		}
		size := state.End - state.Start + 1
		if state.Downloaded < 0 || state.Downloaded > size || (state.Completed && state.Downloaded != size) {
			return false
		}
		expectedStart = state.End + 1
	}
	return expectedStart == cp.ContentLength
}
