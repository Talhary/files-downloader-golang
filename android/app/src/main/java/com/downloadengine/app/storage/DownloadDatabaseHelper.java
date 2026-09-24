package com.downloadengine.app.storage;

import android.content.ContentValues;
import android.content.Context;
import android.database.Cursor;
import android.database.sqlite.SQLiteDatabase;
import android.database.sqlite.SQLiteOpenHelper;

import com.downloadengine.app.engine.DownloadState;
import com.downloadengine.app.engine.DownloadTask;

import java.util.ArrayList;
import java.util.List;

public class DownloadDatabaseHelper extends SQLiteOpenHelper {

    private static final String DATABASE_NAME = "dlengine_tasks.db";
    private static final int DATABASE_VERSION = 1;

    public static final String TABLE_TASKS = "tasks";
    public static final String COL_ID = "id";
    public static final String COL_URL = "url";
    public static final String COL_FILENAME = "filename";
    public static final String COL_DEST_PATH = "dest_path";
    public static final String COL_STATE_PATH = "state_path";
    public static final String COL_CATEGORY = "category";
    public static final String COL_TOTAL_BYTES = "total_bytes";
    public static final String COL_DOWNLOADED_BYTES = "downloaded_bytes";
    public static final String COL_STATUS = "status";
    public static final String COL_ERROR = "error";
    public static final String COL_CREATED_AT = "created_at";
    public static final String COL_COMPLETED_AT = "completed_at";

    private static DownloadDatabaseHelper sInstance;

    public static synchronized DownloadDatabaseHelper getInstance(Context context) {
        if (sInstance == null) {
            sInstance = new DownloadDatabaseHelper(context.getApplicationContext());
        }
        return sInstance;
    }

    public DownloadDatabaseHelper(Context context) {
        super(context, DATABASE_NAME, null, DATABASE_VERSION);
    }

    @Override
    public void onCreate(SQLiteDatabase db) {
        String sql = "CREATE TABLE " + TABLE_TASKS + " (" +
                COL_ID + " TEXT PRIMARY KEY, " +
                COL_URL + " TEXT, " +
                COL_FILENAME + " TEXT, " +
                COL_DEST_PATH + " TEXT, " +
                COL_STATE_PATH + " TEXT, " +
                COL_CATEGORY + " TEXT, " +
                COL_TOTAL_BYTES + " INTEGER, " +
                COL_DOWNLOADED_BYTES + " INTEGER, " +
                COL_STATUS + " TEXT, " +
                COL_ERROR + " TEXT, " +
                COL_CREATED_AT + " INTEGER, " +
                COL_COMPLETED_AT + " INTEGER" +
                ")";
        db.execSQL(sql);
    }

    @Override
    public void onUpgrade(SQLiteDatabase db, int oldVersion, int newVersion) {
        db.execSQL("DROP TABLE IF EXISTS " + TABLE_TASKS);
        onCreate(db);
    }

    public synchronized void insertOrUpdateTask(DownloadTask task) {
        SQLiteDatabase db = getWritableDatabase();
        ContentValues cv = new ContentValues();
        cv.put(COL_ID, task.id);
        cv.put(COL_URL, task.url);
        cv.put(COL_FILENAME, task.filename);
        cv.put(COL_DEST_PATH, task.destPath);
        cv.put(COL_STATE_PATH, task.statePath);
        cv.put(COL_CATEGORY, task.category);
        cv.put(COL_TOTAL_BYTES, task.totalBytes);
        cv.put(COL_DOWNLOADED_BYTES, task.downloadedBytes);
        cv.put(COL_STATUS, task.status.name());
        cv.put(COL_ERROR, task.errorMessage);
        cv.put(COL_CREATED_AT, task.createdAt);
        cv.put(COL_COMPLETED_AT, task.completedAt);
        db.insertWithOnConflict(TABLE_TASKS, null, cv, SQLiteDatabase.CONFLICT_REPLACE);
    }

    public synchronized void insertBatchTasks(List<DownloadTask> tasks) {
        if (tasks == null || tasks.isEmpty()) return;
        SQLiteDatabase db = getWritableDatabase();
        db.beginTransaction();
        try {
            for (DownloadTask task : tasks) {
                ContentValues cv = new ContentValues();
                cv.put(COL_ID, task.id);
                cv.put(COL_URL, task.url);
                cv.put(COL_FILENAME, task.filename);
                cv.put(COL_DEST_PATH, task.destPath);
                cv.put(COL_STATE_PATH, task.statePath);
                cv.put(COL_CATEGORY, task.category);
                cv.put(COL_TOTAL_BYTES, task.totalBytes);
                cv.put(COL_DOWNLOADED_BYTES, task.downloadedBytes);
                cv.put(COL_STATUS, task.status.name());
                cv.put(COL_ERROR, task.errorMessage);
                cv.put(COL_CREATED_AT, task.createdAt);
                cv.put(COL_COMPLETED_AT, task.completedAt);
                db.insertWithOnConflict(TABLE_TASKS, null, cv, SQLiteDatabase.CONFLICT_REPLACE);
            }
            db.setTransactionSuccessful();
        } finally {
            db.endTransaction();
        }
    }

    public synchronized void updateProgress(String taskId, long downloadedBytes, long totalBytes, String status) {
        SQLiteDatabase db = getWritableDatabase();
        ContentValues cv = new ContentValues();
        cv.put(COL_DOWNLOADED_BYTES, downloadedBytes);
        if (totalBytes > 0) {
            cv.put(COL_TOTAL_BYTES, totalBytes);
        }
        if (status != null) {
            cv.put(COL_STATUS, status);
        }
        db.update(TABLE_TASKS, cv, COL_ID + "=?", new String[]{taskId});
    }

    public synchronized void updateStatus(String taskId, DownloadState status, String error) {
        SQLiteDatabase db = getWritableDatabase();
        ContentValues cv = new ContentValues();
        cv.put(COL_STATUS, status.name());
        if (error != null) {
            cv.put(COL_ERROR, error);
        }
        if (status == DownloadState.COMPLETED) {
            cv.put(COL_COMPLETED_AT, System.currentTimeMillis());
        }
        db.update(TABLE_TASKS, cv, COL_ID + "=?", new String[]{taskId});
    }

    public synchronized void deleteTask(String taskId) {
        SQLiteDatabase db = getWritableDatabase();
        db.delete(TABLE_TASKS, COL_ID + "=?", new String[]{taskId});
    }

    public synchronized void deleteAllTasks() {
        SQLiteDatabase db = getWritableDatabase();
        db.delete(TABLE_TASKS, null, null);
    }

    public synchronized void deleteCompletedTasks() {
        SQLiteDatabase db = getWritableDatabase();
        db.delete(TABLE_TASKS, COL_STATUS + "=?", new String[]{DownloadState.COMPLETED.name()});
    }

    public synchronized List<DownloadTask> getAllTasks() {
        List<DownloadTask> list = new ArrayList<>();
        SQLiteDatabase db = getReadableDatabase();
        Cursor c = db.query(TABLE_TASKS, null, null, null, null, null, COL_CREATED_AT + " DESC");
        if (c != null) {
            while (c.moveToNext()) {
                DownloadTask t = new DownloadTask();
                t.id = c.getString(c.getColumnIndexOrThrow(COL_ID));
                t.url = c.getString(c.getColumnIndexOrThrow(COL_URL));
                t.filename = c.getString(c.getColumnIndexOrThrow(COL_FILENAME));
                t.destPath = c.getString(c.getColumnIndexOrThrow(COL_DEST_PATH));
                t.statePath = c.getString(c.getColumnIndexOrThrow(COL_STATE_PATH));
                t.category = c.getString(c.getColumnIndexOrThrow(COL_CATEGORY));
                t.totalBytes = c.getLong(c.getColumnIndexOrThrow(COL_TOTAL_BYTES));
                t.downloadedBytes = c.getLong(c.getColumnIndexOrThrow(COL_DOWNLOADED_BYTES));
                t.status = DownloadState.valueOf(c.getString(c.getColumnIndexOrThrow(COL_STATUS)));
                t.errorMessage = c.getString(c.getColumnIndexOrThrow(COL_ERROR));
                t.createdAt = c.getLong(c.getColumnIndexOrThrow(COL_CREATED_AT));
                t.completedAt = c.getLong(c.getColumnIndexOrThrow(COL_COMPLETED_AT));
                list.add(t);
            }
            c.close();
        }
        return list;
    }
}
