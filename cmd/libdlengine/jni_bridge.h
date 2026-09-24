#ifndef JNI_BRIDGE_H
#define JNI_BRIDGE_H

#include <jni.h>
#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

// Java Callback invocation wrappers called from Go
void notifyJavaProgress(uintptr_t callbackHandle, const char *taskId, int64_t downloaded, int64_t total, double speed, double eta, int activeWorkers, int completedChunks, int totalChunks);
void notifyJavaCompleted(uintptr_t callbackHandle, const char *taskId, const char *destPath);
void notifyJavaError(uintptr_t callbackHandle, const char *taskId, int errorCode, const char *errorMessage);
void freeCallbackHandle(uintptr_t callbackHandle);

#ifdef __cplusplus
}
#endif

#endif // JNI_BRIDGE_H
