package com.downloadengine.app.storage;

import android.content.Context;
import android.os.Environment;

import java.io.File;
import java.util.Locale;

public class SmartCategoryManager {

    public static final String CAT_VIDEOS = "VIDEOS";
    public static final String CAT_MUSIC = "MUSIC";
    public static final String CAT_DOCS = "DOCUMENTS";
    public static final String CAT_IMAGES = "IMAGES";
    public static final String CAT_ARCHIVES = "ARCHIVES";
    public static final String CAT_APPS = "APPS";
    public static final String CAT_OTHER = "OTHER";

    public static String classify(String filename, String contentType) {
        if (filename == null) filename = "";
        String lower = filename.toLowerCase(Locale.US);

        if (lower.endsWith(".mp4") || lower.endsWith(".mkv") || lower.endsWith(".avi") ||
            lower.endsWith(".mov") || lower.endsWith(".webm") || lower.endsWith(".flv") ||
            (contentType != null && contentType.startsWith("video/"))) {
            return CAT_VIDEOS;
        }

        if (lower.endsWith(".mp3") || lower.endsWith(".m4a") || lower.endsWith(".wav") ||
            lower.endsWith(".flac") || lower.endsWith(".aac") || lower.endsWith(".ogg") ||
            (contentType != null && contentType.startsWith("audio/"))) {
            return CAT_MUSIC;
        }

        if (lower.endsWith(".pdf") || lower.endsWith(".doc") || lower.endsWith(".docx") ||
            lower.endsWith(".xls") || lower.endsWith(".xlsx") || lower.endsWith(".ppt") ||
            lower.endsWith(".pptx") || lower.endsWith(".txt") || lower.endsWith(".epub")) {
            return CAT_DOCS;
        }

        if (lower.endsWith(".jpg") || lower.endsWith(".jpeg") || lower.endsWith(".png") ||
            lower.endsWith(".webp") || lower.endsWith(".gif") || lower.endsWith(".svg") ||
            (contentType != null && contentType.startsWith("image/"))) {
            return CAT_IMAGES;
        }

        if (lower.endsWith(".zip") || lower.endsWith(".rar") || lower.endsWith(".7z") ||
            lower.endsWith(".tar") || lower.endsWith(".gz") || lower.endsWith(".iso")) {
            return CAT_ARCHIVES;
        }

        if (lower.endsWith(".apk") || lower.endsWith(".xapk") || lower.endsWith(".apks")) {
            return CAT_APPS;
        }

        return CAT_OTHER;
    }

    public static File getCategoryDir(Context context, String category) {
        String subName;
        switch (category) {
            case CAT_VIDEOS: subName = "Videos"; break;
            case CAT_MUSIC: subName = "Music"; break;
            case CAT_DOCS: subName = "Documents"; break;
            case CAT_IMAGES: subName = "Images"; break;
            case CAT_ARCHIVES: subName = "Archives"; break;
            case CAT_APPS: subName = "Apps"; break;
            default: subName = "Other"; break;
        }

        // 1. Try public Download folder if accessible and writable
        try {
            File publicDl = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS);
            if (publicDl != null) {
                File target = new File(new File(publicDl, "DL_Engine"), subName);
                if (target.exists() || target.mkdirs()) {
                    File testFile = new File(target, ".perm_test_" + System.currentTimeMillis());
                    if (testFile.createNewFile()) {
                        testFile.delete();
                        return target;
                    }
                }
            }
        } catch (Throwable ignored) {
        }

        // 2. Fallback to App External Files Dir (guaranteed writable without permissions on Android 10-14+)
        try {
            File extBase = context.getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS);
            if (extBase != null) {
                File target = new File(new File(extBase, "DL_Engine"), subName);
                if (!target.exists()) {
                    target.mkdirs();
                }
                return target;
            }
        } catch (Throwable ignored) {
        }

        // 3. Fallback to App Internal Files Dir
        File target = new File(new File(context.getFilesDir(), "DL_Engine"), subName);
        if (!target.exists()) {
            target.mkdirs();
        }
        return target;
    }
}
