package com.downloadengine.app.engine;

import java.util.Locale;

public class DownloadTask {
    public String id;
    public String url;
    public String filename;
    public String destPath;
    public String statePath;
    public String category;
    public long totalBytes;
    public long downloadedBytes;
    public double speedBytesSec;
    public double etaSeconds;
    public int activeWorkers;
    public int completedChunks;
    public int totalChunks;
    public DownloadState status = DownloadState.QUEUED;
    public long createdAt;
    public long completedAt;
    public String errorMessage;
    public int fileDescriptor = -1;

    public DownloadTask() {
        this.createdAt = System.currentTimeMillis();
    }

    public int getPercent() {
        if (totalBytes <= 0) return 0;
        return (int) Math.min(100, (downloadedBytes * 100) / totalBytes);
    }

    public String getFormattedSize() {
        return formatBytes(downloadedBytes) + " / " + (totalBytes > 0 ? formatBytes(totalBytes) : "Unknown");
    }

    public String getFormattedTotalSize() {
        return totalBytes > 0 ? formatBytes(totalBytes) : "Unknown Size";
    }

    public String getFormattedSpeed() {
        if (status != DownloadState.DOWNLOADING || speedBytesSec <= 0) {
            return "0.0 MB/s";
        }
        return String.format(Locale.US, "%.1f MB/s", speedBytesSec / (1024.0 * 1024.0));
    }

    public String getFormattedEta() {
        if (status != DownloadState.DOWNLOADING || etaSeconds <= 0) {
            return "--";
        }
        int seconds = (int) etaSeconds;
        int mins = seconds / 60;
        int secs = seconds % 60;
        int hrs = mins / 60;
        mins = mins % 60;
        if (hrs > 0) {
            return String.format(Locale.US, "%02dh %02dm", hrs, mins);
        }
        return String.format(Locale.US, "%02dm %02ds", mins, secs);
    }

    public static String formatBytes(long bytes) {
        if (bytes <= 0) return "0 B";
        if (bytes < 1024) return bytes + " B";
        int exp = (int) (Math.log(bytes) / Math.log(1024));
        String pre = "KMGTPE".charAt(exp - 1) + "";
        return String.format(Locale.US, "%.1f %sB", bytes / Math.pow(1024, exp), pre);
    }
}
