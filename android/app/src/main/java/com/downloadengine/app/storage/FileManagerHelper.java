package com.downloadengine.app.storage;

import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.webkit.MimeTypeMap;
import android.widget.Toast;

import com.downloadengine.app.engine.DownloadTask;

import java.io.File;

public class FileManagerHelper {

    public static void openFile(Context context, DownloadTask task) {
        if (task.destPath == null) return;
        if (task.destPath.startsWith("content:")) {
            try {
                Uri contentUri = Uri.parse(task.destPath);
                Intent intent = new Intent(Intent.ACTION_VIEW);
                intent.setDataAndType(contentUri, context.getContentResolver().getType(contentUri));
                intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_ACTIVITY_NEW_TASK);
                context.startActivity(Intent.createChooser(intent, "Open with..."));
            } catch (Exception e) {
                Toast.makeText(context, "No app found to open this file", Toast.LENGTH_SHORT).show();
            }
            return;
        }
        File file = new File(task.destPath);
        if (!file.exists()) {
            Toast.makeText(context, "File does not exist: " + file.getName(), Toast.LENGTH_SHORT).show();
            return;
        }

        try {
            Uri contentUri = DownloadFileProvider.getUriForFile(file);
            Intent intent = new Intent(Intent.ACTION_VIEW);
            String mimeType = getMimeType(file.getName());
            intent.setDataAndType(contentUri, mimeType);
            intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
            context.startActivity(Intent.createChooser(intent, "Open with..."));
        } catch (Exception e) {
            Toast.makeText(context, "No app found to open this file", Toast.LENGTH_SHORT).show();
        }
    }

    public static void shareFile(Context context, DownloadTask task) {
        if (task.destPath == null) return;
        if (task.destPath.startsWith("content:")) {
            try {
                Uri contentUri = Uri.parse(task.destPath);
                Intent intent = new Intent(Intent.ACTION_SEND);
                intent.setType(context.getContentResolver().getType(contentUri));
                intent.putExtra(Intent.EXTRA_STREAM, contentUri);
                intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_ACTIVITY_NEW_TASK);
                context.startActivity(Intent.createChooser(intent, "Share file..."));
            } catch (Exception e) {
                Toast.makeText(context, "Failed to share file", Toast.LENGTH_SHORT).show();
            }
            return;
        }
        File file = new File(task.destPath);
        if (!file.exists()) {
            Toast.makeText(context, "File does not exist", Toast.LENGTH_SHORT).show();
            return;
        }

        try {
            Uri contentUri = DownloadFileProvider.getUriForFile(file);
            Intent intent = new Intent(Intent.ACTION_SEND);
            intent.setType(getMimeType(file.getName()));
            intent.putExtra(Intent.EXTRA_STREAM, contentUri);
            intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
            context.startActivity(Intent.createChooser(intent, "Share file..."));
        } catch (Exception e) {
            Toast.makeText(context, "Failed to share file: " + e.getMessage(), Toast.LENGTH_SHORT).show();
        }
    }

    public static void deleteTaskAndFile(Context context, DownloadTask task) {
        com.downloadengine.app.service.DownloadQueueManager.getInstance(context).removeTask(task, true);
    }

    private static String getMimeType(String filename) {
        int dot = filename.lastIndexOf('.');
        if (dot >= 0) {
            String ext = filename.substring(dot + 1).toLowerCase();
            String mime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext);
            if (mime != null) return mime;
        }
        return "application/octet-stream";
    }
}
