package com.downloadengine.app.service;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.os.Build;
import android.graphics.drawable.Icon;
import android.os.IBinder;
import android.os.PowerManager;

import com.downloadengine.app.MainActivity;
import com.downloadengine.app.R;
import com.downloadengine.app.engine.DownloadState;
import com.downloadengine.app.engine.DownloadTask;

import java.util.List;

public class DownloadService extends Service implements DownloadQueueManager.DownloadListener {

    public static final String CHANNEL_ID = "dl_engine_service_channel";
    public static final int NOTIFICATION_ID = 1001;

    public static final String ACTION_UPDATE_NOTIFICATION = "com.downloadengine.app.ACTION_UPDATE";
    public static final String ACTION_PAUSE = "com.downloadengine.app.ACTION_PAUSE";
    public static final String ACTION_RESUME = "com.downloadengine.app.ACTION_RESUME";
    public static final String ACTION_CANCEL = "com.downloadengine.app.ACTION_CANCEL";
    public static final String EXTRA_TASK_ID = "extra_task_id";

    private NotificationManager mNotifMgr;
    private PowerManager.WakeLock mWakeLock;
    private DownloadQueueManager mQueueMgr;

    @Override
    public void onCreate() {
        super.onCreate();
        mNotifMgr = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        mQueueMgr = DownloadQueueManager.getInstance(this);
        mQueueMgr.addListener(this);

        createNotificationChannel();
        acquireWakeLock();
    }

    private void acquireWakeLock() {
        PowerManager pm = (PowerManager) getSystemService(Context.POWER_SERVICE);
        if (pm != null && (mWakeLock == null || !mWakeLock.isHeld())) {
            mWakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "DLEngine:DownloadWakeLock");
            mWakeLock.acquire(12 * 60 * 60 * 1000L); // 12 hours max
        }
    }

    private void releaseWakeLock() {
        if (mWakeLock != null && mWakeLock.isHeld()) {
            mWakeLock.release();
            mWakeLock = null;
        }
    }

    private void createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel channel = new NotificationChannel(
                    CHANNEL_ID,
                    getString(R.string.notif_channel_name),
                    NotificationManager.IMPORTANCE_LOW
            );
            channel.setDescription(getString(R.string.notif_channel_desc));
            channel.setShowBadge(false);
            if (mNotifMgr != null) {
                mNotifMgr.createNotificationChannel(channel);
            }
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && intent.getAction() != null) {
            String action = intent.getAction();
            String taskId = intent.getStringExtra(EXTRA_TASK_ID);

            if (ACTION_PAUSE.equals(action) && taskId != null) {
                mQueueMgr.pause(taskId);
            } else if (ACTION_RESUME.equals(action) && taskId != null) {
                mQueueMgr.resume(taskId);
            } else if (ACTION_CANCEL.equals(action) && taskId != null) {
                mQueueMgr.cancel(taskId, true);
            }
        }

        updateForegroundNotification();
        return START_STICKY;
    }

    private synchronized void updateForegroundNotification() {
        List<DownloadTask> tasks = mQueueMgr.getTasks();
        DownloadTask primaryActive = null;
        int activeCount = 0;
        double totalSpeed = 0;

        for (DownloadTask t : tasks) {
            if (t.status == DownloadState.DOWNLOADING) {
                if (primaryActive == null) {
                    primaryActive = t;
                }
                activeCount++;
                totalSpeed += t.speedBytesSec;
            }
        }

        if (activeCount == 0) {
            stopForeground(true);
            releaseWakeLock();
            stopSelf();
            return;
        }

        acquireWakeLock();

        Intent openIntent = new Intent(this, MainActivity.class);
        PendingIntent pendingOpen = PendingIntent.getActivity(
                this, 0, openIntent,
                PendingIntent.FLAG_UPDATE_CURRENT | (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M ? PendingIntent.FLAG_IMMUTABLE : 0)
        );

        Notification.Builder builder;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            builder = new Notification.Builder(this, CHANNEL_ID);
        } else {
            builder = new Notification.Builder(this);
        }

        String title = primaryActive.filename;
        if (activeCount > 1) {
            title += " (+" + (activeCount - 1) + " more)";
        }

        String speedStr = String.format(java.util.Locale.US, "%.1f MB/s", totalSpeed / (1024.0 * 1024.0));
        String content = primaryActive.getFormattedSize() + " • " + speedStr + " • " + primaryActive.getFormattedEta();

        builder.setContentTitle(title)
                .setContentText(content)
                .setSmallIcon(R.drawable.ic_notification)
                .setContentIntent(pendingOpen)
                .setOngoing(true)
                .setProgress(100, primaryActive.getPercent(), false);

        // Pause Action Button
        Intent pauseIntent = new Intent(this, DownloadService.class);
        pauseIntent.setAction(ACTION_PAUSE);
        pauseIntent.putExtra(EXTRA_TASK_ID, primaryActive.id);
        PendingIntent piPause = PendingIntent.getService(
                this, 1, pauseIntent,
                PendingIntent.FLAG_UPDATE_CURRENT | (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M ? PendingIntent.FLAG_IMMUTABLE : 0)
        );
        builder.addAction(new Notification.Action.Builder(Icon.createWithResource(this, R.drawable.ic_notification), "Pause", piPause).build());

        // Cancel Action Button
        Intent cancelIntent = new Intent(this, DownloadService.class);
        cancelIntent.setAction(ACTION_CANCEL);
        cancelIntent.putExtra(EXTRA_TASK_ID, primaryActive.id);
        PendingIntent piCancel = PendingIntent.getService(
                this, 2, cancelIntent,
                PendingIntent.FLAG_UPDATE_CURRENT | (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M ? PendingIntent.FLAG_IMMUTABLE : 0)
        );
        builder.addAction(new Notification.Action.Builder(Icon.createWithResource(this, R.drawable.ic_notification), "Cancel", piCancel).build());

        Notification notif = builder.build();

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIFICATION_ID, notif, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC);
        } else {
            startForeground(NOTIFICATION_ID, notif);
        }
    }

    @Override
    public void onQueueUpdated() {
        updateForegroundNotification();
    }

    @Override
    public void onTaskProgress(DownloadTask task) {
        updateForegroundNotification();
    }

    @Override
    public void onTaskCompleted(DownloadTask task) {
        updateForegroundNotification();
        showCompletionNotification(task);
    }

    @Override
    public void onTaskFailed(DownloadTask task, String error) {
        updateForegroundNotification();
    }

    private void showCompletionNotification(DownloadTask task) {
        if (mNotifMgr == null) return;

        Intent openIntent = new Intent(this, MainActivity.class);
        PendingIntent pi = PendingIntent.getActivity(
                this, (int) System.currentTimeMillis(), openIntent,
                PendingIntent.FLAG_UPDATE_CURRENT | (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M ? PendingIntent.FLAG_IMMUTABLE : 0)
        );

        Notification.Builder builder;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            builder = new Notification.Builder(this, CHANNEL_ID);
        } else {
            builder = new Notification.Builder(this);
        }

        builder.setContentTitle("Download Complete")
                .setContentText(task.filename + " (" + task.getFormattedTotalSize() + ")")
                .setSmallIcon(R.drawable.ic_notification)
                .setContentIntent(pi)
                .setAutoCancel(true);

        mNotifMgr.notify((int) (System.currentTimeMillis() % 100000), builder.build());
    }

    @Override
    public void onDestroy() {
        super.onDestroy();
        mQueueMgr.removeListener(this);
        releaseWakeLock();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}
