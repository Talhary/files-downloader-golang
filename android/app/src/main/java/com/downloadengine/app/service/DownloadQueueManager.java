package com.downloadengine.app.service;

import android.content.Context;
import android.content.Intent;
import android.os.Handler;
import android.os.Looper;
import android.os.ParcelFileDescriptor;
import android.provider.DocumentsContract;

import com.downloadengine.app.engine.DownloadState;
import com.downloadengine.app.engine.DownloadTask;
import com.downloadengine.app.engine.NativeBridge;
import com.downloadengine.app.engine.NativeCallback;
import com.downloadengine.app.storage.DownloadDatabaseHelper;
import com.downloadengine.app.storage.SmartCategoryManager;

import java.io.File;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

public class DownloadQueueManager implements NativeCallback, NetworkMonitor.NetworkListener {

    public interface DownloadListener {
        void onQueueUpdated();
        void onTaskProgress(DownloadTask task);
        void onTaskCompleted(DownloadTask task);
        void onTaskFailed(DownloadTask task, String error);
    }

    private static DownloadQueueManager sInstance;

    private final Context mContext;
    private final DownloadDatabaseHelper mDb;
    private final Handler mMainHandler = new Handler(Looper.getMainLooper());
    private final ExecutorService mExecutor = Executors.newCachedThreadPool();
    private final List<DownloadTask> mTasks = new CopyOnWriteArrayList<>();
    private final List<DownloadListener> mListeners = new CopyOnWriteArrayList<>();

    private int mMaxConcurrent = 3;
    private int mDefaultConcurrency = 16;
    private long mDefaultChunkSize = 8 * 1024 * 1024; // 8 MB
    private boolean mWifiOnly = false;
    private NetworkMonitor mNetworkMonitor;

    public static synchronized DownloadQueueManager getInstance(Context context) {
        if (sInstance == null) {
            sInstance = new DownloadQueueManager(context.getApplicationContext());
        }
        return sInstance;
    }

    private DownloadQueueManager(Context context) {
        mContext = context;
        mDb = DownloadDatabaseHelper.getInstance(context);
        mNetworkMonitor = new NetworkMonitor(context, this);
        loadTasksFromDatabase();
    }

    private void loadTasksFromDatabase() {
        mTasks.clear();
        List<DownloadTask> list = mDb.getAllTasks();
        for (DownloadTask t : list) {
            if (t.status == DownloadState.DOWNLOADING) {
                // Recovered from abnormal termination, mark paused for clean resume
                t.status = DownloadState.PAUSED;
                mDb.updateStatus(t.id, DownloadState.PAUSED, null);
            }
            mTasks.add(t);
        }
    }

    public List<DownloadTask> getTasks() {
        return Collections.unmodifiableList(mTasks);
    }

    public void addListener(DownloadListener l) {
        if (l != null && !mListeners.contains(l)) {
            mListeners.add(l);
        }
    }

    public void removeListener(DownloadListener l) {
        mListeners.remove(l);
    }

    public void setWifiOnly(boolean wifiOnly) {
        mWifiOnly = wifiOnly;
        if (mWifiOnly && !mNetworkMonitor.isWifi()) {
            pauseAllActive("Paused: Wi-Fi only mode active");
        } else {
            pumpQueue();
        }
    }

    public boolean isWifiOnly() { return mWifiOnly; }

    public void setEngineConcurrency(int concurrency) {
        this.mDefaultConcurrency = concurrency;
    }

    public int getEngineConcurrency() { return mDefaultConcurrency; }

    public void enqueue(String rawUrl, String customFilename, String customCategory) {
        enqueue(rawUrl, customFilename, customCategory, true);
    }

    private static final String PREFS = "download_settings";
    private static final String PREF_CUSTOM_TREE = "custom_tree_uri";

    public void setCustomTree(android.net.Uri treeUri) {
        mContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
                .putString(PREF_CUSTOM_TREE, treeUri == null ? null : treeUri.toString()).apply();
    }

    private android.net.Uri getCustomTree() {
        String value = mContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(PREF_CUSTOM_TREE, null);
        return value == null ? null : android.net.Uri.parse(value);
    }

    private void prepareDestination(DownloadTask task, String category) {
        android.net.Uri tree = getCustomTree();
        if (tree == null) {
            File targetDir = SmartCategoryManager.getCategoryDir(mContext, category);
            task.destPath = new File(targetDir, task.filename).getAbsolutePath();
            return;
        }
        try {
            String mime = task.url != null && task.url.toLowerCase().contains(".m3u8")
                    ? "application/vnd.apple.mpegurl" : "application/octet-stream";
            android.net.Uri parentDoc = DocumentsContract.isDocumentUri(mContext, tree) ? tree :
                    DocumentsContract.buildDocumentUriUsingTree(tree, DocumentsContract.getTreeDocumentId(tree));
            android.net.Uri document = DocumentsContract.createDocument(mContext.getContentResolver(), parentDoc, mime, task.filename);
            if (document == null) throw new IllegalStateException("Unable to create document");
            ParcelFileDescriptor descriptor = mContext.getContentResolver().openFileDescriptor(document, "rw");
            if (descriptor == null) throw new IllegalStateException("Unable to open document");
            task.fileDescriptor = descriptor.detachFd();
            descriptor.close();
            task.destPath = document.toString();
        } catch (Exception e) {
            task.fileDescriptor = -1;
            File targetDir = SmartCategoryManager.getCategoryDir(mContext, category);
            task.destPath = new File(targetDir, task.filename).getAbsolutePath();
        }
    }

    public void enqueue(String rawUrl, String customFilename, String customCategory, boolean startImmediately) {
        mExecutor.execute(() -> {
            NativeBridge.ProbeResult probe = NativeBridge.probe(rawUrl);

            String filename = customFilename;
            if (filename == null || filename.trim().isEmpty()) {
                filename = (probe.filename != null && !probe.filename.isEmpty()) ? probe.filename : "download_" + System.currentTimeMillis();
            }

            String category = customCategory;
            if (category == null || category.isEmpty()) {
                category = SmartCategoryManager.classify(filename, probe.contentType);
            }

            DownloadTask task = new DownloadTask();
            task.id = UUID.randomUUID().toString();
            task.url = rawUrl;
            task.filename = filename;
            prepareDestination(task, category);
            task.statePath = new File(mContext.getCacheDir(), task.id + ".dlstate.json").getAbsolutePath();
            task.category = category;
            task.totalBytes = probe.contentLength;
            task.status = startImmediately ? DownloadState.QUEUED : DownloadState.PAUSED;

            mDb.insertOrUpdateTask(task);
            mTasks.add(0, task);

            notifyQueueUpdated();
            if (startImmediately) {
                pumpQueue();
            }
        });
    }

    public void enqueueBatch(List<String> urls) {
        if (urls == null || urls.isEmpty()) return;
        mExecutor.execute(() -> {
            List<DownloadTask> newTasks = new ArrayList<>();
            long now = System.currentTimeMillis();
            int idx = 0;
            for (String rawUrl : urls) {
                if (rawUrl == null || rawUrl.trim().isEmpty()) continue;
                String cleanUrl = rawUrl.trim();
                String filename = extractFilenameFromUrl(cleanUrl, idx++);
                String category = SmartCategoryManager.classify(filename, null);

                DownloadTask task = new DownloadTask();
                task.id = UUID.randomUUID().toString();
                task.url = cleanUrl;
                task.filename = filename;
                prepareDestination(task, category);
                task.statePath = new File(mContext.getCacheDir(), task.id + ".dlstate.json").getAbsolutePath();
                task.category = category;
                task.totalBytes = 0;
                task.status = DownloadState.QUEUED;
                task.createdAt = now + idx;
                newTasks.add(task);
            }

            if (!newTasks.isEmpty()) {
                mDb.insertBatchTasks(newTasks);
                mTasks.addAll(0, newTasks);

                notifyQueueUpdated();
                pumpQueue();
            }
        });
    }

    public static String extractFilenameFromUrl(String url, int index) {
        if (url == null || url.trim().isEmpty()) return "download_" + System.currentTimeMillis() + "_" + index;
        try {
            android.net.Uri uri = android.net.Uri.parse(url);
            String path = uri.getPath();
            if (path != null && !path.isEmpty()) {
                int lastSlash = path.lastIndexOf('/');
                if (lastSlash >= 0 && lastSlash < path.length() - 1) {
                    String name = path.substring(lastSlash + 1);
                    if (name.contains(".")) {
                        return name;
                    }
                }
            }
        } catch (Exception ignored) {}
        return "file_" + System.currentTimeMillis() + "_" + index;
    }

    public synchronized void pumpQueue() {
        if (mWifiOnly && !mNetworkMonitor.isWifi()) {
            return;
        }

        int activeCount = 0;
        for (DownloadTask t : mTasks) {
            if (t.status == DownloadState.DOWNLOADING) {
                activeCount++;
            }
        }

        if (activeCount >= mMaxConcurrent) return;

        for (DownloadTask t : mTasks) {
            if (t.status == DownloadState.QUEUED) {
                startTask(t);
                activeCount++;
                if (activeCount >= mMaxConcurrent) break;
            }
        }
    }

    private void startTask(DownloadTask task) {
        task.status = DownloadState.DOWNLOADING;
        mDb.updateStatus(task.id, DownloadState.DOWNLOADING, null);
        notifyQueueUpdated();

        // Start Foreground Service
        Intent intent = new Intent(mContext, DownloadService.class);
        intent.setAction(DownloadService.ACTION_UPDATE_NOTIFICATION);
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.O) {
            mContext.startForegroundService(intent);
        } else {
            mContext.startService(intent);
        }

        mExecutor.execute(() -> {
            try {
                if (!NativeBridge.isLoaded()) {
                    throw new UnsatisfiedLinkError("Native download engine is unavailable for this device");
                }
                if (task.fileDescriptor >= 0) {
                    NativeBridge.nativeStartDownloadFD(task.id, task.url, task.fileDescriptor, task.statePath,
                            mDefaultConcurrency, mDefaultChunkSize, 5, this);
                } else {
                    NativeBridge.nativeStartDownload(task.id, task.url, task.destPath, task.statePath,
                            mDefaultConcurrency, mDefaultChunkSize, 5, this);
                }
            } catch (Throwable e) {
                task.status = DownloadState.ERROR;
                task.errorMessage = e.getMessage() != null ? e.getMessage() : "Unable to start native download";
                mDb.updateStatus(task.id, DownloadState.ERROR, task.errorMessage);
                notifyQueueUpdated();
            }
        });
    }

    private void closeTaskDescriptor(DownloadTask task) {
        if (task.fileDescriptor >= 0) {
            try { ParcelFileDescriptor.adoptFd(task.fileDescriptor).close(); } catch (Exception ignored) {}
            task.fileDescriptor = -1;
        }
    }

    public void removeTask(DownloadTask task, boolean deleteFiles) {
        if (task == null) return;
        if (task.status == DownloadState.DOWNLOADING) {
            try { NativeBridge.nativeCancel(task.id, task.statePath, deleteFiles, task.destPath); } catch (Throwable ignored) {}
        } else if (deleteFiles) {
            if (task.destPath != null && !task.destPath.startsWith("content:")) {
                try { new File(task.destPath).delete(); } catch (Exception ignored) {}
            }
            if (task.statePath != null) try { new File(task.statePath).delete(); } catch (Exception ignored) {}
        }
        closeTaskDescriptor(task);
        mTasks.remove(task);
        mDb.deleteTask(task.id);
        notifyQueueUpdated();
    }

    public void pause(String taskId) {
        DownloadTask task = findTask(taskId);
        if (task != null && task.status == DownloadState.DOWNLOADING) {
            try { NativeBridge.nativePause(taskId); } catch (Throwable ignored) {}
            task.status = DownloadState.PAUSED;
            mDb.updateStatus(taskId, DownloadState.PAUSED, null);
            notifyQueueUpdated();
            pumpQueue();
        }
    }

    public void resume(String taskId) {
        DownloadTask task = findTask(taskId);
        if (task != null && (task.status == DownloadState.PAUSED || task.status == DownloadState.ERROR)) {
            task.status = DownloadState.QUEUED;
            mDb.updateStatus(taskId, DownloadState.QUEUED, null);
            notifyQueueUpdated();
            pumpQueue();
        }
    }

    public void cancel(String taskId, boolean deleteFile) {
        DownloadTask task = findTask(taskId);
        if (task != null) {
            try { NativeBridge.nativeCancel(taskId, task.statePath, deleteFile, task.destPath); } catch (Throwable ignored) {}
            mTasks.remove(task);
            mDb.deleteTask(taskId);
            notifyQueueUpdated();
            pumpQueue();
        }
    }

    public synchronized void resumeAll() {
        boolean changed = false;
        for (DownloadTask t : mTasks) {
            if (t.status == DownloadState.PAUSED || t.status == DownloadState.ERROR) {
                t.status = DownloadState.QUEUED;
                t.errorMessage = null;
                mDb.updateStatus(t.id, DownloadState.QUEUED, null);
                changed = true;
            }
        }
        if (changed) {
            notifyQueueUpdated();
            pumpQueue();
        }
    }

    public synchronized void stopAll() {
        boolean changed = false;
        for (DownloadTask t : mTasks) {
            if (t.status == DownloadState.DOWNLOADING) {
                NativeBridge.nativePause(t.id);
                t.status = DownloadState.PAUSED;
                mDb.updateStatus(t.id, DownloadState.PAUSED, null);
                changed = true;
            } else if (t.status == DownloadState.QUEUED) {
                t.status = DownloadState.PAUSED;
                mDb.updateStatus(t.id, DownloadState.PAUSED, null);
                changed = true;
            }
        }
        if (changed) {
            notifyQueueUpdated();
        }
    }

    public synchronized void cancelAll(boolean deleteFiles) {
        List<DownloadTask> toRemove = new ArrayList<>();
        for (DownloadTask t : mTasks) {
            if (t.status != DownloadState.COMPLETED) {
                if (t.status == DownloadState.DOWNLOADING) {
                    NativeBridge.nativeCancel(t.id, t.statePath, deleteFiles, t.destPath);
                } else if (deleteFiles) {
                    try {
                        if (t.destPath != null) {
                            File f = new File(t.destPath);
                            if (f.exists()) f.delete();
                        }
                        if (t.statePath != null) {
                            File sf = new File(t.statePath);
                            if (sf.exists()) sf.delete();
                        }
                    } catch (Exception ignored) {}
                }
                toRemove.add(t);
                mDb.deleteTask(t.id);
            }
        }
        mTasks.removeAll(toRemove);
        notifyQueueUpdated();
        pumpQueue();
    }

    public synchronized void deleteAll(boolean deleteFiles) {
        for (DownloadTask t : mTasks) {
            if (t.status == DownloadState.DOWNLOADING) {
                NativeBridge.nativeCancel(t.id, t.statePath, deleteFiles, t.destPath);
            } else if (deleteFiles) {
                try {
                    if (t.destPath != null) {
                        File f = new File(t.destPath);
                        if (f.exists()) f.delete();
                    }
                    if (t.statePath != null) {
                        File sf = new File(t.statePath);
                        if (sf.exists()) sf.delete();
                    }
                } catch (Exception ignored) {}
            }
        }
        mTasks.clear();
        mDb.deleteAllTasks();
        notifyQueueUpdated();
    }

    private void pauseAllActive(String reason) {
        for (DownloadTask t : mTasks) {
            if (t.status == DownloadState.DOWNLOADING) {
                pause(t.id);
            }
        }
    }

    public DownloadTask findTask(String taskId) {
        for (DownloadTask t : mTasks) {
            if (t.id.equals(taskId)) return t;
        }
        return null;
    }

    @Override
    public void onProgress(String taskId, long downloadedBytes, long totalBytes, double speedBytesSec, double etaSeconds, int activeWorkers, int completedChunks, int totalChunks) {
        DownloadTask task = findTask(taskId);
        if (task != null) {
            task.downloadedBytes = downloadedBytes;
            if (totalBytes > 0) task.totalBytes = totalBytes;
            task.speedBytesSec = speedBytesSec;
            task.etaSeconds = etaSeconds;
            task.activeWorkers = activeWorkers;
            task.completedChunks = completedChunks;
            task.totalChunks = totalChunks;

            mDb.updateProgress(taskId, downloadedBytes, totalBytes, DownloadState.DOWNLOADING.name());

            mMainHandler.post(() -> {
                for (DownloadListener l : mListeners) {
                    l.onTaskProgress(task);
                }
            });
        }
    }

    @Override
    public void onCompleted(String taskId, String destPath) {
        DownloadTask task = findTask(taskId);
        if (task != null) {
            task.status = DownloadState.COMPLETED;
            closeTaskDescriptor(task);
            task.completedAt = System.currentTimeMillis();
            if (destPath != null && !destPath.isEmpty()) {
                task.destPath = destPath;
            }
            mDb.updateStatus(taskId, DownloadState.COMPLETED, null);

            mMainHandler.post(() -> {
                notifyQueueUpdated();
                for (DownloadListener l : mListeners) {
                    l.onTaskCompleted(task);
                }
                pumpQueue();
            });
        }
    }

    @Override
    public void onError(String taskId, int errorCode, String errorMessage) {
        DownloadTask task = findTask(taskId);
        if (task != null) {
            task.status = DownloadState.ERROR;
            task.errorMessage = errorMessage;
            mDb.updateStatus(taskId, DownloadState.ERROR, errorMessage);

            mMainHandler.post(() -> {
                notifyQueueUpdated();
                for (DownloadListener l : mListeners) {
                    l.onTaskFailed(task, errorMessage);
                }
                pumpQueue();
            });
        }
    }

    @Override
    public void onNetworkChanged(boolean isConnected, boolean isWifi) {
        if (!isConnected) {
            pauseAllActive("Network disconnected");
        } else {
            if (mWifiOnly && !isWifi) {
                pauseAllActive("Switched to mobile data");
            } else {
                pumpQueue();
            }
        }
    }

    private void notifyQueueUpdated() {
        mMainHandler.post(() -> {
            for (DownloadListener l : mListeners) {
                l.onQueueUpdated();
            }
        });
    }
}
