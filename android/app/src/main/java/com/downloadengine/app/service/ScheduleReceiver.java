package com.downloadengine.app.service;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

public class ScheduleReceiver extends BroadcastReceiver {

    public static final String ACTION_SCHEDULED_START = "com.downloadengine.app.ACTION_SCHEDULED_START";
    public static final String EXTRA_TASK_ID = "scheduled_task_id";

    @Override
    public void onReceive(Context context, Intent intent) {
        if (intent == null) return;
        String taskId = intent.getStringExtra(EXTRA_TASK_ID);

        DownloadQueueManager queueMgr = DownloadQueueManager.getInstance(context);
        if (taskId != null) {
            queueMgr.resume(taskId);
        } else {
            queueMgr.pumpQueue();
        }

        Intent serviceIntent = new Intent(context, DownloadService.class);
        serviceIntent.setAction(DownloadService.ACTION_UPDATE_NOTIFICATION);
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.O) {
            context.startForegroundService(serviceIntent);
        } else {
            context.startService(serviceIntent);
        }
    }
}
