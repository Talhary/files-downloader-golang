package main

/*
#include "jni_bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"
	"unsafe"

	"download-engine/pkg/engine"
)

type activeTask struct {
	cancel context.CancelFunc
	ctx    context.Context
	paused bool
	mu     sync.Mutex
}

var (
	tasksMu sync.Mutex
	tasks   = make(map[string]*activeTask)
)

//export freeGoCString
func freeGoCString(str *C.char) {
	if str != nil {
		C.free(unsafe.Pointer(str))
	}
}

//export goProbe
func goProbe(rawURL *C.char, customHeaders *C.char) *C.char {
	goURL := C.GoString(rawURL)
	goHeaders := C.GoString(customHeaders)

	opts := engine.DefaultOptions()
	if goHeaders != "" {
		var headerMap map[string]string
		if err := json.Unmarshal([]byte(goHeaders), &headerMap); err == nil {
			for k, v := range headerMap {
				opts.Headers[k] = v
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	info, err := engine.Probe(ctx, goURL, opts)
	resp := make(map[string]interface{})

	if err != nil {
		resp["error"] = err.Error()
	} else {
		resp["url"] = info.URL
		resp["final_url"] = info.FinalURL
		resp["filename"] = info.Filename
		resp["content_length"] = info.ContentLength
		resp["accept_ranges"] = info.AcceptRanges
		resp["content_type"] = info.ContentType
		resp["status_code"] = info.StatusCode
		resp["etag"] = info.ETag
		if !info.LastModified.IsZero() {
			resp["last_modified"] = info.LastModified.Format(time.RFC3339)
		}
	}

	data, _ := json.Marshal(resp)
	return C.CString(string(data))
}

//export goStartDownload
func goStartDownload(
	cTaskId *C.char,
	cRawURL *C.char,
	cDestPath *C.char,
	cStatePath *C.char,
	concurrency C.int,
	chunkSize C.int64_t,
	maxRetries C.int,
	callbackHandle C.uintptr_t,
) C.int {
	taskId := C.GoString(cTaskId)
	rawURL := C.GoString(cRawURL)
	destPath := C.GoString(cDestPath)
	statePath := C.GoString(cStatePath)

	ctx, cancel := context.WithCancel(context.Background())
	task := &activeTask{cancel: cancel, ctx: ctx}

	tasksMu.Lock()
	tasks[taskId] = task
	tasksMu.Unlock()

	go func() {
		defer func() {
			tasksMu.Lock()
			delete(tasks, taskId)
			tasksMu.Unlock()
			C.freeCallbackHandle(callbackHandle)
		}()

		cTID := C.CString(taskId)
		defer C.free(unsafe.Pointer(cTID))

		opts := []engine.Option{
			engine.WithConcurrency(int(concurrency)),
			engine.WithChunkSize(int64(chunkSize)),
			engine.WithMaxRetries(int(maxRetries)),
			engine.WithProgressCallback(func(s engine.ProgressSnapshot) {
				C.notifyJavaProgress(
					callbackHandle,
					cTID,
					C.int64_t(s.DownloadedBytes),
					C.int64_t(s.TotalBytes),
					C.double(s.SpeedBytesPerSec),
					C.double(s.ETA.Seconds()),
					C.int(s.ActiveWorkers),
					C.int(s.CompletedChunks),
					C.int(s.TotalChunks),
				)
			}, 300*time.Millisecond),
		}

		eng := engine.New(opts...)
		info, err := eng.DownloadToFileWithResume(ctx, rawURL, destPath, statePath)

		task.mu.Lock()
		wasPaused := task.paused
		task.mu.Unlock()

		if err != nil {
			if wasPaused || ctx.Err() == context.Canceled {
				// Clean pause, do not emit fatal error
				return
			}
			cErr := C.CString(err.Error())
			defer C.free(unsafe.Pointer(cErr))
			C.notifyJavaError(callbackHandle, cTID, 1, cErr)
			return
		}

		cDest := C.CString(info.Filename)
		if destPath != "" {
			cDest = C.CString(destPath)
		}
		defer C.free(unsafe.Pointer(cDest))
		C.notifyJavaCompleted(callbackHandle, cTID, cDest)
	}()

	return 0
}

//export goStartDownloadFD
func goStartDownloadFD(
	cTaskId *C.char,
	cRawURL *C.char,
	fd C.int,
	cStatePath *C.char,
	concurrency C.int,
	chunkSize C.int64_t,
	maxRetries C.int,
	callbackHandle C.uintptr_t,
) C.int {
	taskId := C.GoString(cTaskId)
	rawURL := C.GoString(cRawURL)
	statePath := C.GoString(cStatePath)

	ctx, cancel := context.WithCancel(context.Background())
	task := &activeTask{cancel: cancel, ctx: ctx}

	tasksMu.Lock()
	tasks[taskId] = task
	tasksMu.Unlock()

	go func() {
		defer func() {
			tasksMu.Lock()
			delete(tasks, taskId)
			tasksMu.Unlock()
			C.freeCallbackHandle(callbackHandle)
		}()

		cTID := C.CString(taskId)
		defer C.free(unsafe.Pointer(cTID))

		opts := []engine.Option{
			engine.WithConcurrency(int(concurrency)),
			engine.WithChunkSize(int64(chunkSize)),
			engine.WithMaxRetries(int(maxRetries)),
			engine.WithProgressCallback(func(s engine.ProgressSnapshot) {
				C.notifyJavaProgress(
					callbackHandle,
					cTID,
					C.int64_t(s.DownloadedBytes),
					C.int64_t(s.TotalBytes),
					C.double(s.SpeedBytesPerSec),
					C.double(s.ETA.Seconds()),
					C.int(s.ActiveWorkers),
					C.int(s.CompletedChunks),
					C.int(s.TotalChunks),
				)
			}, 300*time.Millisecond),
		}

		eng := engine.New(opts...)
		info, err := eng.DownloadToFD(ctx, rawURL, int(fd), statePath)

		task.mu.Lock()
		wasPaused := task.paused
		task.mu.Unlock()

		if err != nil {
			if wasPaused || ctx.Err() == context.Canceled {
				return
			}
			cErr := C.CString(err.Error())
			defer C.free(unsafe.Pointer(cErr))
			C.notifyJavaError(callbackHandle, cTID, 1, cErr)
			return
		}

		cDest := C.CString(info.Filename)
		defer C.free(unsafe.Pointer(cDest))
		C.notifyJavaCompleted(callbackHandle, cTID, cDest)
	}()

	return 0
}

//export goPauseDownload
func goPauseDownload(cTaskId *C.char) C.bool {
	taskId := C.GoString(cTaskId)
	tasksMu.Lock()
	task, exists := tasks[taskId]
	tasksMu.Unlock()

	if !exists || task == nil {
		return C.bool(false)
	}

	task.mu.Lock()
	task.paused = true
	task.cancel()
	task.mu.Unlock()

	return C.bool(true)
}

//export goCancelDownload
func goCancelDownload(cTaskId *C.char, cStatePath *C.char, deleteFile C.bool, cDestPath *C.char) C.bool {
	taskId := C.GoString(cTaskId)
	statePath := C.GoString(cStatePath)
	destPath := C.GoString(cDestPath)

	tasksMu.Lock()
	task, exists := tasks[taskId]
	if exists && task != nil {
		task.mu.Lock()
		task.cancel()
		task.mu.Unlock()
		delete(tasks, taskId)
	}
	tasksMu.Unlock()

	if statePath != "" {
		engine.RemoveCheckpoint(statePath)
	}
	if bool(deleteFile) && destPath != "" {
		_ = os.Remove(destPath)
	}

	return C.bool(true)
}

func main() {}
