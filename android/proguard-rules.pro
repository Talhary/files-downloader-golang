# R8 / ProGuard Optimization & Shrinking Rules for DL Engine Pro

# Keep all JNI bridge and callback classes
-keep class com.downloadengine.app.engine.NativeBridge { *; }
-keep class com.downloadengine.app.engine.NativeCallback { *; }
-keep class com.downloadengine.app.engine.DownloadTask { *; }
-keep class com.downloadengine.app.engine.DownloadState { *; }

# Keep JavaScript bridge interface for WebView
-keepclassmembers class * {
    @android.webkit.JavascriptInterface <methods>;
}

# Keep Services, Receivers, and ContentProviders
-keep class com.downloadengine.app.service.DownloadService { *; }
-keep class com.downloadengine.app.service.ScheduleReceiver { *; }
-keep class com.downloadengine.app.storage.DownloadFileProvider { *; }

# Keep Activity lifecycle entrypoints
-keep class com.downloadengine.app.MainActivity {
    public void *(android.view.View);
}

-dontwarn **
