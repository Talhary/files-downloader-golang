package com.downloadengine.app.browser;

import android.webkit.JavascriptInterface;
import android.webkit.WebView;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Locale;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;

public class MediaSniffer {

    public static class SniffedMediaItem {
        public String url;
        public String title;
        public String mimeType;
        public String format;
        public List<String> availableQualities = new ArrayList<>();

        public SniffedMediaItem(String url, String title, String mimeType) {
            this.url = url;
            this.title = title != null && !title.isEmpty() ? title : "Discovered Media";
            this.mimeType = mimeType;
            this.format = detectFormat(url, mimeType);

            // Populate multi-resolution choices
            if (format.equals("HLS")) {
                availableQualities.add("HLS playlist");
            } else {
                availableQualities.add("Original (" + format + ")");
            }
        }

        private static String detectFormat(String url, String mime) {
            String lower = url.toLowerCase(Locale.US);
            if (lower.contains(".m3u8")) return "HLS";
            if (lower.contains(".mp4")) return "MP4";
            if (lower.contains(".mkv")) return "MKV";
            if (lower.contains(".webm")) return "WEBM";
            if (lower.contains(".mp3")) return "MP3";
            if (lower.contains(".m4a")) return "M4A";
            if (lower.contains(".zip")) return "ZIP";
            if (lower.contains(".rar")) return "RAR";
            if (lower.contains(".pdf")) return "PDF";
            if (lower.contains(".apk")) return "APK";
            if (mime != null && mime.contains("video")) return "MP4";
            if (mime != null && mime.contains("audio")) return "MP3";
            return "FILE";
        }
    }

    public interface OnMediaDetectedListener {
        void onMediaDetected(SniffedMediaItem item, int totalCount);
    }

    private final Set<String> mSniffedUrls = Collections.newSetFromMap(new ConcurrentHashMap<>());
    private final List<SniffedMediaItem> mDiscoveredItems = new CopyOnWriteArrayList<>();
    private OnMediaDetectedListener mListener;

    public void setListener(OnMediaDetectedListener listener) {
        this.mListener = listener;
    }

    public void clear() {
        mSniffedUrls.clear();
        mDiscoveredItems.clear();
        if (mListener != null) {
            mListener.onMediaDetected(null, 0);
        }
    }

    public List<SniffedMediaItem> getDiscoveredItems() {
        return Collections.unmodifiableList(mDiscoveredItems);
    }

    public void inspectUrl(String url, String pageTitle) {
        if (url == null || mSniffedUrls.contains(url)) return;
        if (AdBlocker.isAd(url)) return;

        String lower = url.toLowerCase(Locale.US);
        boolean isMedia = lower.contains(".mp4") || lower.contains(".mkv") || lower.contains(".webm") ||
                lower.contains(".mp3") || lower.contains(".m4a") || lower.contains(".flac") ||
                lower.contains(".wav") || lower.contains(".zip") || lower.contains(".rar") ||
                lower.contains(".7z") || lower.contains(".pdf") || lower.contains(".apk") ||
                lower.contains(".m3u8");

        if (isMedia) {
            mSniffedUrls.add(url);
            String title = pageTitle;
            if (title == null || title.isEmpty()) {
                int lastSlash = url.lastIndexOf('/');
                title = lastSlash >= 0 ? url.substring(lastSlash + 1) : "Media Stream";
            }
            String mimeType = lower.contains(".m3u8") ? "application/vnd.apple.mpegurl"
                    : lower.contains(".mp3") || lower.contains(".m4a") ? "audio/mpeg"
                    : "video/mp4";
            SniffedMediaItem item = new SniffedMediaItem(url, title, mimeType);
            mDiscoveredItems.add(item);

            if (mListener != null) {
                mListener.onMediaDetected(item, mDiscoveredItems.size());
            }
        }
    }

    public void injectDOMInspector(WebView webView, String pageTitle) {
        if (webView == null) return;
        String js = "(function() {" +
                "  var nodes = document.querySelectorAll('video, audio, source, a[download]');" +
                "  for (var i = 0; i < nodes.length; i++) {" +
                "    var src = nodes[i].src || nodes[i].href;" +
                "    if (src && src.indexOf('http') === 0) {" +
                "      window.mediaBridge.onDomMedia(src, document.title);" +
                "    }" +
                "  }" +
                "})();";
        webView.evaluateJavascript(js, null);
    }

    public static class JsBridge {
        private final MediaSniffer mSniffer;

        public JsBridge(MediaSniffer sniffer) {
            this.mSniffer = sniffer;
        }

        @JavascriptInterface
        public void onDomMedia(String url, String pageTitle) {
            if (mSniffer != null) {
                mSniffer.inspectUrl(url, pageTitle);
            }
        }
    }
}
