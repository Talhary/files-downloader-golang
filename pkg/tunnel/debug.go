package tunnel

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var debugMu sync.Mutex

func DebugLog(format string, args ...interface{}) {
	debugMu.Lock()
	defer debugMu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	logPath := filepath.Join(home, ".dlengine", "debug.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err == nil {
		defer f.Close()
		timestamp := time.Now().Format("15:04:05.000")
		fmt.Fprintf(f, "[%s] %s\n", timestamp, fmt.Sprintf(format, args...))
	}
}
