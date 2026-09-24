package com.downloadengine.app.engine;

/**
 * JNI callback interface invoked directly from Go native workers in libdlengine.so.
 */
public interface NativeCallback {
    void onProgress(String taskId, long downloadedBytes, long totalBytes, double speedBytesSec, double etaSeconds, int activeWorkers, int completedChunks, int totalChunks);
    void onCompleted(String taskId, String destPath);
    void onError(String taskId, int errorCode, String errorMessage);
}
