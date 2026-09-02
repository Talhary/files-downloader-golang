package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"download-engine/pkg/engine"
)

type JSONEvent struct {
	Event            string  `json:"event"`
	Filename         string  `json:"filename,omitempty"`
	TotalBytes       int64   `json:"total_bytes,omitempty"`
	DownloadedBytes  int64   `json:"downloaded_bytes,omitempty"`
	Percent          float64 `json:"percent,omitempty"`
	SpeedBytesPerSec float64 `json:"speed_bytes_sec,omitempty"`
	ETASeconds       float64 `json:"eta_seconds,omitempty"`
	ElapsedSeconds   float64 `json:"elapsed_seconds,omitempty"`
	ActiveWorkers    int     `json:"active_workers,omitempty"`
	CompletedChunks  int     `json:"completed_chunks,omitempty"`
	TotalChunks      int     `json:"total_chunks,omitempty"`
	AcceptRanges     bool    `json:"accept_ranges,omitempty"`
	StatusCode       int     `json:"status_code,omitempty"`
	FinalURL         string  `json:"final_url,omitempty"`
	AvgSpeedBytesSec float64 `json:"avg_speed_bytes_sec,omitempty"`
	Message          string  `json:"message,omitempty"`
	DestPath         string  `json:"dest_path,omitempty"`
}

func emitJSON(event JSONEvent) {
	data, _ := json.Marshal(event)
	fmt.Println(string(data))
}

func main() {
	var (
		rawURL       string
		destPath     string
		concurrency  int
		chunkSizeStr string
		streamMode   bool
		maxRetries   int
		jsonMode     bool
		silentMode   bool
		probeOnly    bool
		headersFlag  headerList
	)

	defaultURL := "https://dl.downloadly.ir/Files/Elearning/The_Gnomon_Workshop_3D_WEAPON_DESIGN_VR_WORKFLOW_2024-6.part5_Downloadly.ir.rar?nocache=1788171959"

	flag.StringVar(&rawURL, "u", defaultURL, "Target URL to download")
	flag.StringVar(&rawURL, "url", defaultURL, "Target URL to download")
	flag.StringVar(&destPath, "o", "", "Destination path (file or directory, defaults to remote filename)")
	flag.StringVar(&destPath, "output", "", "Destination path (file or directory, defaults to remote filename)")
	flag.IntVar(&concurrency, "c", 16, "Number of concurrent download connections (workers)")
	flag.IntVar(&concurrency, "concurrency", 16, "Number of concurrent download connections (workers)")
	flag.StringVar(&chunkSizeStr, "s", "8MB", "Chunk size (e.g. 4MB, 8MB, 16MB, 32MB)")
	flag.StringVar(&chunkSizeStr, "chunk-size", "8MB", "Chunk size (e.g. 4MB, 8MB, 16MB, 32MB)")
	flag.BoolVar(&streamMode, "stream", false, "Enable stream mode (stream sequentially via io.Reader with prefetch)")
	flag.IntVar(&maxRetries, "r", 5, "Max retries per chunk on network failures")
	flag.IntVar(&maxRetries, "retries", 5, "Max retries per chunk on network failures")
	flag.BoolVar(&jsonMode, "json", false, "Output machine-readable newline-delimited JSON events (for Python/Node integrations)")
	flag.BoolVar(&silentMode, "silent", false, "Suppress progress output (only output errors or completion)")
	flag.BoolVar(&probeOnly, "probe-only", false, "Only probe remote metadata and exit without downloading")
	flag.Var(&headersFlag, "H", "Custom HTTP Header in 'Key: Value' format (can be specified multiple times)")
	flag.Var(&headersFlag, "header", "Custom HTTP Header in 'Key: Value' format")

	flag.Parse()

	if strings.TrimSpace(rawURL) == "" {
		if jsonMode {
			emitJSON(JSONEvent{Event: "error", Message: "Download URL is required."})
		} else {
			fmt.Println("❌ Error: Download URL is required.")
			flag.Usage()
		}
		os.Exit(1)
	}

	chunkSizeBytes := parseSize(chunkSizeStr, engine.DefaultChunkSize)

	// Set up cancellation context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if !jsonMode && !silentMode {
		fmt.Println()
		fmt.Println("================================================================================")
		fmt.Println("  🚀 High-Speed Multi-Part Download & Streaming Engine (Go)")
		fmt.Println("================================================================================")
		fmt.Printf("  🔍 Probing target: %s\n", truncateString(rawURL, 65))
	}

	// Probe remote target first
	probeOpts := engine.DefaultOptions()
	probeOpts.Headers = headersFlag.toMap()
	info, err := engine.Probe(ctx, rawURL, probeOpts)
	if err != nil {
		if jsonMode {
			emitJSON(JSONEvent{Event: "error", Message: fmt.Sprintf("probe failed: %v", err)})
		} else {
			fmt.Printf("\n❌ Probe error: %v\n", err)
		}
		os.Exit(1)
	}

	modeStr := "Multi-Part Concurrent (Direct WriteAt)"
	if streamMode {
		modeStr = "Multi-Part Stream Pipeline (Ordered io.Reader + Prefetch)"
	}
	if !info.AcceptRanges {
		modeStr = "Single Stream Fallback (No Accept-Ranges on server)"
		concurrency = 1
	}

	numChunks := 1
	if info.AcceptRanges && info.ContentLength > 0 {
		chunks := engine.CalculateChunks(info.ContentLength, chunkSizeBytes, concurrency)
		numChunks = len(chunks)
	}

	if jsonMode {
		emitJSON(JSONEvent{
			Event:        "probe",
			Filename:     info.Filename,
			TotalBytes:   info.ContentLength,
			AcceptRanges: info.AcceptRanges,
			StatusCode:   info.StatusCode,
			FinalURL:     info.FinalURL,
			TotalChunks:  numChunks,
		})
	}

	if probeOnly {
		return
	}

	if !jsonMode && !silentMode {
		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Printf("  📁 Filename:     %s\n", info.Filename)
		fmt.Printf("  📦 File Size:    %s (%s bytes)\n", formatBytes(info.ContentLength), formatNumber(info.ContentLength))
		fmt.Printf("  ⚡ Mode:         %s\n", modeStr)
		fmt.Printf("  🌐 Range Support: %v (Status: %d)\n", info.AcceptRanges, info.StatusCode)
		fmt.Printf("  🧵 Concurrency:  %d workers\n", concurrency)
		fmt.Printf("  🧩 Chunk Size:   %s (%d total chunks)\n", formatBytes(chunkSizeBytes), numChunks)
		if destPath != "" {
			fmt.Printf("  💾 Destination:  %s\n", destPath)
		} else {
			fmt.Printf("  💾 Destination:  ./%s\n", info.Filename)
		}
		fmt.Println("================================================================================")
		fmt.Println("  ⏳ Starting download...")
		fmt.Println()
	}

	ui := newTerminalUI(info.ContentLength, numChunks)

	opts := []engine.Option{
		engine.WithConcurrency(concurrency),
		engine.WithChunkSize(chunkSizeBytes),
		engine.WithMaxRetries(maxRetries),
		engine.WithHeaders(headersFlag.toMap()),
		engine.WithProgressCallback(func(s engine.ProgressSnapshot) {
			if jsonMode {
				emitJSON(JSONEvent{
					Event:            "progress",
					DownloadedBytes:  s.DownloadedBytes,
					TotalBytes:       s.TotalBytes,
					Percent:          s.Percent,
					SpeedBytesPerSec: s.SpeedBytesPerSec,
					ETASeconds:       s.ETA.Seconds(),
					ElapsedSeconds:   s.Elapsed.Seconds(),
					ActiveWorkers:    s.ActiveWorkers,
					CompletedChunks:  s.CompletedChunks,
					TotalChunks:      s.TotalChunks,
				})
			} else if !silentMode {
				ui.Update(s)
			}
		}, 150*time.Millisecond),
	}

	eng := engine.New(opts...)
	startTime := time.Now()

	var downloadResult *engine.FileInfo
	targetOutputFile := destPath
	if targetOutputFile == "" {
		targetOutputFile = info.Filename
	}

	if streamMode {
		var f *os.File
		f, err = os.OpenFile(targetOutputFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err == nil {
			defer f.Close()
			downloadResult, err = eng.DownloadToWriter(ctx, rawURL, f)
		}
	} else {
		downloadResult, err = eng.DownloadToFile(ctx, rawURL, destPath)
	}

	elapsed := time.Since(startTime)
	if !jsonMode && !silentMode {
		ui.ClearProgressLine()
	}

	if err != nil {
		if jsonMode {
			emitJSON(JSONEvent{Event: "error", Message: err.Error()})
		} else {
			fmt.Printf("\n❌ Download failed: %v\n", err)
		}
		os.Exit(1)
	}

	avgSpeed := float64(info.ContentLength) / elapsed.Seconds()
	if elapsed.Seconds() <= 0 {
		avgSpeed = 0
	}

	if jsonMode {
		emitJSON(JSONEvent{
			Event:            "completed",
			Filename:         info.Filename,
			DestPath:         targetOutputFile,
			TotalBytes:       info.ContentLength,
			ElapsedSeconds:   elapsed.Seconds(),
			AvgSpeedBytesSec: avgSpeed,
		})
	} else if !silentMode {
		fmt.Println()
		fmt.Println("================================================================================")
		fmt.Println("  ✅ Download Completed Successfully!")
		fmt.Println("================================================================================")
		if downloadResult != nil {
			fmt.Printf("  📄 File:        %s\n", downloadResult.Filename)
		}
		fmt.Printf("  📊 Total Size:  %s (%s bytes)\n", formatBytes(info.ContentLength), formatNumber(info.ContentLength))
		fmt.Printf("  ⏱️  Time Elapsed: %s\n", formatDuration(elapsed))
		fmt.Printf("  🚀 Average Speed: %s/s\n", formatBytes(int64(avgSpeed)))
		fmt.Println("================================================================================")
		fmt.Println()
	}
}

// TerminalUI renders the live dynamic progress dashboard in the CLI.
type TerminalUI struct {
	totalBytes  int64
	totalChunks int
}

func newTerminalUI(totalBytes int64, totalChunks int) *TerminalUI {
	return &TerminalUI{
		totalBytes:  totalBytes,
		totalChunks: totalChunks,
	}
}

func (ui *TerminalUI) Update(s engine.ProgressSnapshot) {
	progressBar := renderProgressBar(s.Percent, 32)
	speedStr := formatBytes(int64(s.SpeedBytesPerSec)) + "/s"
	downloadedStr := formatBytes(s.DownloadedBytes)
	totalStr := formatBytes(s.TotalBytes)
	if s.TotalBytes <= 0 {
		totalStr = "Unknown"
	}

	etaStr := "estimating..."
	if s.ETA > 0 {
		etaStr = formatDuration(s.ETA)
	}

	statusLine := fmt.Sprintf("\r  [%s] %5.1f%% (%s / %s) | ⚡ %9s | ⏱️ ETA: %-6s | 🧵 %d w | 🧩 %d/%d ",
		progressBar,
		s.Percent,
		downloadedStr,
		totalStr,
		speedStr,
		etaStr,
		s.ActiveWorkers,
		s.CompletedChunks,
		s.TotalChunks,
	)

	fmt.Print(statusLine)
}

func (ui *TerminalUI) ClearProgressLine() {
	fmt.Print("\r" + strings.Repeat(" ", 110) + "\r")
}

func renderProgressBar(percent float64, width int) string {
	if width <= 0 {
		width = 25
	}
	filled := int(math.Round((percent / 100.0) * float64(width)))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled
	return strings.Repeat("█", filled) + strings.Repeat("░", empty)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.2f %s", float64(bytes)/float64(div), units[exp])
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%02dh %02dm %02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%02dm %02ds", m, s)
	}
	return fmt.Sprintf("%02ds", s)
}

func formatNumber(n int64) string {
	str := strconv.FormatInt(n, 10)
	var result []string
	length := len(str)
	for i, c := range str {
		if i > 0 && (length-i)%3 == 0 {
			result = append(result, ",")
		}
		result = append(result, string(c))
	}
	return strings.Join(result, "")
}

func parseSize(str string, fallback int64) int64 {
	str = strings.ToUpper(strings.TrimSpace(str))
	if str == "" {
		return fallback
	}

	var multiplier int64 = 1
	var numStr string

	if strings.HasSuffix(str, "GB") || strings.HasSuffix(str, "G") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(strings.TrimSuffix(str, "GB"), "G")
	} else if strings.HasSuffix(str, "MB") || strings.HasSuffix(str, "M") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(strings.TrimSuffix(str, "MB"), "M")
	} else if strings.HasSuffix(str, "KB") || strings.HasSuffix(str, "K") {
		multiplier = 1024
		numStr = strings.TrimSuffix(strings.TrimSuffix(str, "KB"), "K")
	} else if strings.HasSuffix(str, "B") {
		multiplier = 1
		numStr = strings.TrimSuffix(str, "B")
	} else {
		numStr = str
	}

	val, err := strconv.ParseInt(strings.TrimSpace(numStr), 10, 64)
	if err != nil || val <= 0 {
		return fallback
	}

	return val * multiplier
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

type headerList []string

func (h *headerList) String() string {
	return strings.Join(*h, ", ")
}

func (h *headerList) Set(val string) error {
	*h = append(*h, val)
	return nil
}

func (h *headerList) toMap() map[string]string {
	res := make(map[string]string)
	for _, header := range *h {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			res[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return res
}
