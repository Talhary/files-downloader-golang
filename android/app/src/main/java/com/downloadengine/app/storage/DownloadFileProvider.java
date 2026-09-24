package com.downloadengine.app.storage;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;
import android.util.Base64;
import android.webkit.MimeTypeMap;

import java.io.File;
import java.io.FileNotFoundException;
import java.io.IOException;
import java.nio.charset.StandardCharsets;

public class DownloadFileProvider extends ContentProvider {

    public static final String AUTHORITY = "com.downloadengine.app.provider";

    public static Uri getUriForFile(File file) {
        if (file == null) throw new IllegalArgumentException("file is required");
        String encoded = Base64.encodeToString(file.getAbsolutePath().getBytes(StandardCharsets.UTF_8),
                Base64.URL_SAFE | Base64.NO_WRAP | Base64.NO_PADDING);
        return new Uri.Builder().scheme("content").authority(AUTHORITY).encodedPath(encoded).build();
    }

    private File resolve(Uri uri) throws FileNotFoundException {
        if (uri == null || !AUTHORITY.equals(uri.getAuthority()) || uri.getPath() == null) {
            throw new FileNotFoundException("Invalid file URI");
        }
        try {
            String path = new String(Base64.decode(uri.getPath(), Base64.URL_SAFE | Base64.NO_WRAP | Base64.NO_PADDING), StandardCharsets.UTF_8);
            File file = new File(path).getCanonicalFile();
            if (!file.isFile() || !isAllowed(file)) throw new FileNotFoundException("File is outside provider roots");
            return file;
        } catch (IllegalArgumentException | IOException e) {
            throw new FileNotFoundException("Invalid file URI");
        }
    }

    private boolean isAllowed(File file) {
        if (getContext() == null) return false;
        File[] roots = new File[] {
                android.os.Environment.getExternalStorageDirectory(),
                getContext().getExternalFilesDir(null),
                getContext().getFilesDir(),
                getContext().getCacheDir()
        };
        for (File root : roots) {
            if (root == null) continue;
            try {
                String rootPath = root.getCanonicalPath();
                String filePath = file.getCanonicalPath();
                if (filePath.equals(rootPath) || filePath.startsWith(rootPath + File.separator)) return true;
            } catch (IOException ignored) {}
        }
        return false;
    }

    @Override
    public boolean onCreate() { return true; }

    @Override
    public ParcelFileDescriptor openFile(Uri uri, String mode) throws FileNotFoundException {
        if (mode != null && !mode.equals("r")) throw new FileNotFoundException("Provider is read-only");
        return ParcelFileDescriptor.open(resolve(uri), ParcelFileDescriptor.MODE_READ_ONLY);
    }

    @Override
    public String getType(Uri uri) {
        try {
            String path = resolve(uri).getName();
            int dot = path.lastIndexOf('.');
            if (dot >= 0) {
                String mime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(path.substring(dot + 1).toLowerCase());
                if (mime != null) return mime;
            }
        } catch (FileNotFoundException ignored) {}
        return "application/octet-stream";
    }

    @Override
    public Cursor query(Uri uri, String[] projection, String selection, String[] selectionArgs, String sortOrder) {
        try {
            File file = resolve(uri);
            String[] cols = projection == null ? new String[] {OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE} : projection;
            for (String col : cols) {
                if (!OpenableColumns.DISPLAY_NAME.equals(col) && !OpenableColumns.SIZE.equals(col)) {
                    throw new IllegalArgumentException("Unsupported projection column");
                }
            }
            MatrixCursor cursor = new MatrixCursor(cols, 1);
            MatrixCursor.RowBuilder row = cursor.newRow();
            for (String col : cols) row.add(OpenableColumns.DISPLAY_NAME.equals(col) ? file.getName() : file.length());
            return cursor;
        } catch (Exception e) {
            return null;
        }
    }

    @Override public Uri insert(Uri uri, ContentValues values) { return null; }
    @Override public int delete(Uri uri, String selection, String[] selectionArgs) { return 0; }
    @Override public int update(Uri uri, ContentValues values, String selection, String[] selectionArgs) { return 0; }
}
