package com.downloadengine.app.engine;

import org.json.JSONObject;

public class NativeBridge {

    private static boolean sLibraryLoaded = false;

    static {
        try {
            System.loadLibrary("dlengine");
            sLibraryLoaded = true;
        } catch (UnsatisfiedLinkError e) {
            e.printStackTrace();
            sLibraryLoaded = false;
        }
    }

    public static boolean isLoaded() {
        return sLibraryLoaded;
    }

    // Native JNI Declarations
    public static native String nativeProbe(String url, String headersJson);
    public static native int nativeStartDownload(String taskId, String url, String destPath, String statePath, int concurrency, long chunkSize, int maxRetries, NativeCallback callback);
    public static native int nativeStartDownloadFD(String taskId, String url, int fd, String statePath, int concurrency, long chunkSize, int maxRetries, NativeCallback callback);
    public static native boolean nativePause(String taskId);
    public static native boolean nativeCancel(String taskId, String statePath, boolean deleteFile, String destPath);

    public static class ProbeResult {
        public String url;
        public String finalUrl;
        public String filename;
        public long contentLength;
        public boolean acceptRanges;
        public String contentType;
        public int statusCode;
        public String error;
    }

    public static ProbeResult probe(String url) {
        ProbeResult res = new ProbeResult();
        res.url = url;
        if (!sLibraryLoaded) {
            res.error = "Native download engine is unavailable for this device";
            return res;
        }
        try {
            String jsonStr = nativeProbe(url, "{}");
            if (jsonStr != null && !jsonStr.isEmpty()) {
                JSONObject obj = new JSONObject(jsonStr);
                if (obj.has("error")) {
                    res.error = obj.getString("error");
                }
                res.finalUrl = obj.optString("final_url", url);
                res.filename = obj.optString("filename", "downloaded_file");
                res.contentLength = obj.optLong("content_length", 0);
                res.acceptRanges = obj.optBoolean("accept_ranges", false);
                res.contentType = obj.optString("content_type", "application/octet-stream");
                res.statusCode = obj.optInt("status_code", 200);
            }
        } catch (Throwable e) {
            res.error = e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName();
        }
        return res;
    }
}
