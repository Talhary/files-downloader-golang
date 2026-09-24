package com.downloadengine.app;

import android.Manifest;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.Intent;
import android.graphics.Bitmap;
import android.net.Uri;
import android.os.Bundle;
import android.content.pm.PackageManager;
import android.os.Environment;
import android.os.StatFs;
import android.view.LayoutInflater;
import android.view.View;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.RadioGroup;
import android.widget.Spinner;
import android.widget.AdapterView;
import android.widget.ArrayAdapter;
import android.widget.Switch;
import android.widget.TextView;
import android.widget.Toast;

import com.downloadengine.app.engine.NativeBridge;

import java.util.LinkedHashSet;
import java.util.Set;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

import com.downloadengine.app.browser.AdBlocker;
import com.downloadengine.app.browser.MediaSniffer;
import com.downloadengine.app.engine.DownloadState;
import com.downloadengine.app.engine.DownloadTask;
import com.downloadengine.app.service.DownloadQueueManager;
import com.downloadengine.app.storage.FileManagerHelper;
import com.downloadengine.app.storage.SmartCategoryManager;

import java.io.File;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

public class MainActivity extends Activity implements DownloadQueueManager.DownloadListener {

    private DownloadQueueManager mQueueMgr;
    private MediaSniffer mMediaSniffer;

    // Root views
    private View mViewDownloads;
    private View mViewBrowser;
    private View mViewFiles;
    private View mViewSettings;

    // Navigation indicators
    private TextView mTvNavDownloads;
    private TextView mTvNavBrowser;
    private TextView mTvNavFiles;
    private TextView mTvNavSettings;
    private TextView mTvGlobalSpeed;

    // Downloads Tab views
    private LinearLayout mActiveContainer;
    private LinearLayout mCompletedContainer;
    private TextView mTvActiveHeader;
    private TextView mTvCompletedHeader;
    private String mCurrentCategoryFilter = SmartCategoryManager.CAT_OTHER; // "ALL"
    private boolean mFilterAll = true;
    private int mSortMode = 0; // 0: Date DESC, 1: Size DESC, 2: Name ASC

    // Browser Tab views
    private WebView mWebView;
    private EditText mEtBrowserUrl;
    private ProgressBar mPbBrowserLoading;
    private LinearLayout mSnifferPill;
    private TextView mTvSnifferText;

    // Files Tab views
    private LinearLayout mFilesContainer;
    private TextView mTvStorageDetails;

    // Cache of active card views for smooth real-time 60fps updates
    private final Map<String, View> mActiveCardViews = new HashMap<>();
    private volatile String mCurrentPageTitle = "Google";

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_main);

        mQueueMgr = DownloadQueueManager.getInstance(this);
        mQueueMgr.addListener(this);
        mMediaSniffer = new MediaSniffer();

        initViews();
        setupNavigation();
        setupBrowser();
        setupSettings();
        setupDownloadsFab();
        setupBatchToolbar();

        requestRuntimePermissions();
        handleIncomingIntent(getIntent());
        refreshDownloadsUi();
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        handleIncomingIntent(intent);
    }

    private void handleIncomingIntent(Intent intent) {
        if (intent == null) return;
        String action = intent.getAction();

        if (Intent.ACTION_SEND.equals(action) && "text/plain".equals(intent.getType())) {
            String sharedText = intent.getStringExtra(Intent.EXTRA_TEXT);
            if (sharedText != null && sharedText.startsWith("http")) {
                showAddDownloadDialog(sharedText);
            }
        } else if (Intent.ACTION_VIEW.equals(action) && intent.getData() != null) {
            String url = intent.getData().toString();
            if (url.startsWith("http")) {
                showAddDownloadDialog(url);
            }
        }
    }

    private void requestRuntimePermissions() {
        List<String> permissions = new ArrayList<>();
        if (android.os.Build.VERSION.SDK_INT >= 33
                && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            permissions.add(Manifest.permission.POST_NOTIFICATIONS);
        }
        if (android.os.Build.VERSION.SDK_INT <= 28
                && checkSelfPermission(Manifest.permission.WRITE_EXTERNAL_STORAGE) != PackageManager.PERMISSION_GRANTED) {
            permissions.add(Manifest.permission.WRITE_EXTERNAL_STORAGE);
        }
        if (!permissions.isEmpty()) requestPermissions(permissions.toArray(new String[0]), 42);
    }

    private void initViews() {
        mViewDownloads = findViewById(R.id.viewDownloads);
        mViewBrowser = findViewById(R.id.viewBrowser);
        mViewFiles = findViewById(R.id.viewFiles);
        mViewSettings = findViewById(R.id.viewSettings);

        mTvNavDownloads = findViewById(R.id.tvNavDownloads);
        mTvNavBrowser = findViewById(R.id.tvNavBrowser);
        mTvNavFiles = findViewById(R.id.tvNavFiles);
        mTvNavSettings = findViewById(R.id.tvNavSettings);
        mTvGlobalSpeed = findViewById(R.id.tvGlobalSpeed);

        mActiveContainer = findViewById(R.id.activeContainer);
        mCompletedContainer = findViewById(R.id.completedContainer);
        mTvActiveHeader = findViewById(R.id.tvActiveHeader);
        mTvCompletedHeader = findViewById(R.id.tvCompletedHeader);

        mFilesContainer = findViewById(R.id.filesContainer);
        mTvStorageDetails = findViewById(R.id.tvStorageDetails);

        // Sorting toggle button
        TextView tvSortToggle = findViewById(R.id.tvSortToggle);
        if (tvSortToggle != null) {
            tvSortToggle.setOnClickListener(v -> {
                mSortMode = (mSortMode + 1) % 3;
                if (mSortMode == 0) tvSortToggle.setText("Sort: Date ▼");
                else if (mSortMode == 1) tvSortToggle.setText("Sort: Size ▼");
                else tvSortToggle.setText("Sort: Name ▲");
                refreshDownloadsUi();
            });
        }

        // Category filter buttons
        setupCategoryFilterButtons();
    }

    private void setupCategoryFilterButtons() {
        Button btnAll = findViewById(R.id.btnCatAll);
        Button btnVideos = findViewById(R.id.btnCatVideos);
        Button btnMusic = findViewById(R.id.btnCatMusic);
        Button btnDocs = findViewById(R.id.btnCatDocs);
        Button btnArchives = findViewById(R.id.btnCatArchives);
        Button btnApps = findViewById(R.id.btnCatApps);

        View.OnClickListener listener = v -> {
            mFilterAll = false;
            resetCategoryBtnStyles();
            Button b = (Button) v;
            b.setBackgroundResource(R.drawable.bg_button_cyan);
            b.setTextColor(getResources().getColor(R.color.bg_dark));

            if (v.getId() == R.id.btnCatAll) {
                mFilterAll = true;
            } else if (v.getId() == R.id.btnCatVideos) {
                mCurrentCategoryFilter = SmartCategoryManager.CAT_VIDEOS;
            } else if (v.getId() == R.id.btnCatMusic) {
                mCurrentCategoryFilter = SmartCategoryManager.CAT_MUSIC;
            } else if (v.getId() == R.id.btnCatDocs) {
                mCurrentCategoryFilter = SmartCategoryManager.CAT_DOCS;
            } else if (v.getId() == R.id.btnCatArchives) {
                mCurrentCategoryFilter = SmartCategoryManager.CAT_ARCHIVES;
            } else if (v.getId() == R.id.btnCatApps) {
                mCurrentCategoryFilter = SmartCategoryManager.CAT_APPS;
            }
            refreshDownloadsUi();
        };

        btnAll.setOnClickListener(listener);
        btnVideos.setOnClickListener(listener);
        btnMusic.setOnClickListener(listener);
        btnDocs.setOnClickListener(listener);
        btnArchives.setOnClickListener(listener);
        btnApps.setOnClickListener(listener);
    }

    private void resetCategoryBtnStyles() {
        int[] ids = {R.id.btnCatAll, R.id.btnCatVideos, R.id.btnCatMusic, R.id.btnCatDocs, R.id.btnCatArchives, R.id.btnCatApps};
        for (int id : ids) {
            Button b = findViewById(id);
            if (b != null) {
                b.setBackgroundResource(R.drawable.bg_button_dark);
                b.setTextColor(getResources().getColor(R.color.text_white));
            }
        }
    }

    private void setupNavigation() {
        findViewById(R.id.navTabDownloads).setOnClickListener(v -> switchTab(0));
        findViewById(R.id.navTabBrowser).setOnClickListener(v -> switchTab(1));
        findViewById(R.id.navTabFiles).setOnClickListener(v -> switchTab(2));
        findViewById(R.id.navTabSettings).setOnClickListener(v -> switchTab(3));
    }

    private void switchTab(int index) {
        mViewDownloads.setVisibility(index == 0 ? View.VISIBLE : View.GONE);
        mViewBrowser.setVisibility(index == 1 ? View.VISIBLE : View.GONE);
        mViewFiles.setVisibility(index == 2 ? View.VISIBLE : View.GONE);
        mViewSettings.setVisibility(index == 3 ? View.VISIBLE : View.GONE);

        int activeColor = getResources().getColor(R.color.primary_cyan);
        int inactiveColor = getResources().getColor(R.color.text_muted);

        mTvNavDownloads.setTextColor(index == 0 ? activeColor : inactiveColor);
        mTvNavBrowser.setTextColor(index == 1 ? activeColor : inactiveColor);
        mTvNavFiles.setTextColor(index == 2 ? activeColor : inactiveColor);
        mTvNavSettings.setTextColor(index == 3 ? activeColor : inactiveColor);

        if (index == 0) refreshDownloadsUi();
        if (index == 2) refreshFilesUi();
    }

    private void setupDownloadsFab() {
        Button fab = findViewById(R.id.btnFabAdd);
        if (fab != null) {
            fab.setOnClickListener(v -> showAddDownloadDialog(""));
        }
        Button btnClipboard = findViewById(R.id.btnClipboardBatch);
        if (btnClipboard != null) {
            btnClipboard.setOnClickListener(v -> handleClipboardImport());
        }
    }

    private void setupBatchToolbar() {
        Button btnResumeAll = findViewById(R.id.btnBatchResumeAll);
        Button btnStopAll = findViewById(R.id.btnBatchStopAll);
        Button btnCancelAll = findViewById(R.id.btnBatchCancelAll);
        Button btnDeleteAll = findViewById(R.id.btnBatchDeleteAll);

        if (btnResumeAll != null) {
            btnResumeAll.setOnClickListener(v -> {
                mQueueMgr.resumeAll();
                Toast.makeText(this, "Resuming all downloads", Toast.LENGTH_SHORT).show();
            });
        }

        if (btnStopAll != null) {
            btnStopAll.setOnClickListener(v -> {
                mQueueMgr.stopAll();
                Toast.makeText(this, "Stopped all active downloads", Toast.LENGTH_SHORT).show();
            });
        }

        if (btnCancelAll != null) {
            btnCancelAll.setOnClickListener(v -> {
                new AlertDialog.Builder(this)
                        .setTitle("Cancel All Active Downloads")
                        .setMessage("Do you want to cancel all queued and downloading tasks?")
                        .setPositiveButton("Cancel & Delete Partial Files", (dialog, which) -> {
                            mQueueMgr.cancelAll(true);
                            Toast.makeText(this, "Cancelled all active downloads", Toast.LENGTH_SHORT).show();
                        })
                        .setNeutralButton("Cancel Tasks Only", (dialog, which) -> {
                            mQueueMgr.cancelAll(false);
                            Toast.makeText(this, "Cancelled active tasks", Toast.LENGTH_SHORT).show();
                        })
                        .setNegativeButton("Back", null)
                        .show();
            });
        }

        if (btnDeleteAll != null) {
            btnDeleteAll.setOnClickListener(v -> {
                new AlertDialog.Builder(this)
                        .setTitle("Delete All Downloads")
                        .setMessage("Are you sure you want to remove all downloads?")
                        .setPositiveButton("Delete Files & Records", (dialog, which) -> {
                            mQueueMgr.deleteAll(true);
                            Toast.makeText(this, "Cleared all tasks and deleted files", Toast.LENGTH_SHORT).show();
                            refreshDownloadsUi();
                        })
                        .setNeutralButton("Clear List Only", (dialog, which) -> {
                            mQueueMgr.deleteAll(false);
                            Toast.makeText(this, "Cleared all tasks from list", Toast.LENGTH_SHORT).show();
                            refreshDownloadsUi();
                        })
                        .setNegativeButton("Cancel", null)
                        .show();
            });
        }
    }

    private List<String> getUrlsFromClipboard() {
        List<String> urls = new ArrayList<>();
        try {
            ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
            if (cm == null || !cm.hasPrimaryClip()) return urls;
            ClipData clip = cm.getPrimaryClip();
            if (clip == null || clip.getItemCount() == 0) return urls;

            StringBuilder sb = new StringBuilder();
            for (int i = 0; i < clip.getItemCount(); i++) {
                CharSequence text = clip.getItemAt(i).getText();
                if (text != null) {
                    sb.append(text).append("\n");
                }
            }

            String content = sb.toString();
            if (content.trim().isEmpty()) return urls;

            Pattern pattern = Pattern.compile("https?://[^\\s\"'<>]+", Pattern.CASE_INSENSITIVE);
            Matcher matcher = pattern.matcher(content);
            Set<String> seen = new LinkedHashSet<>();

            while (matcher.find()) {
                String u = matcher.group().trim();
                while (u.endsWith(".") || u.endsWith(",") || u.endsWith(")") || u.endsWith(";") || u.endsWith("]") || u.endsWith("}")) {
                    u = u.substring(0, u.length() - 1);
                }
                if (!u.isEmpty() && seen.add(u)) {
                    urls.add(u);
                }
            }
        } catch (Exception e) {
            e.printStackTrace();
        }
        return urls;
    }

    private void handleClipboardImport() {
        List<String> urls = getUrlsFromClipboard();
        if (urls.isEmpty()) {
            Toast.makeText(this, "No valid links found in clipboard", Toast.LENGTH_SHORT).show();
            return;
        }

        if (urls.size() == 1) {
            showAddDownloadDialog(urls.get(0));
        } else {
            showBatchClipboardDialog(urls);
        }
    }

    private void showBatchClipboardDialog(List<String> urls) {
        AlertDialog.Builder builder = new AlertDialog.Builder(this);
        View dialogView = LayoutInflater.from(this).inflate(R.layout.dialog_batch_clipboard, null);
        builder.setView(dialogView);
        AlertDialog dialog = builder.create();

        TextView tvCount = dialogView.findViewById(R.id.tvBatchCount);
        TextView tvPreview = dialogView.findViewById(R.id.tvBatchPreview);
        Button btnCancel = dialogView.findViewById(R.id.btnBatchCancel);
        Button btnAddAll = dialogView.findViewById(R.id.btnBatchAddAll);

        tvCount.setText("Found " + urls.size() + " links ready to download");
        btnAddAll.setText("Add All (" + urls.size() + ")");

        StringBuilder preview = new StringBuilder();
        int maxShow = Math.min(urls.size(), 20);
        for (int i = 0; i < maxShow; i++) {
            preview.append(i + 1).append(". ").append(urls.get(i)).append("\n");
        }
        if (urls.size() > maxShow) {
            preview.append("\n... and ").append(urls.size() - maxShow).append(" more links.");
        }
        tvPreview.setText(preview.toString());

        btnCancel.setOnClickListener(v -> dialog.dismiss());
        btnAddAll.setOnClickListener(v -> {
            mQueueMgr.enqueueBatch(urls);
            dialog.dismiss();
            Toast.makeText(this, "Added " + urls.size() + " downloads to queue!", Toast.LENGTH_SHORT).show();
            switchTab(0);
        });

        dialog.show();
    }

    private void showAddDownloadDialog(String defaultUrl) {
        AlertDialog.Builder builder = new AlertDialog.Builder(this);
        View dialogView = LayoutInflater.from(this).inflate(R.layout.dialog_idm_download, null);
        builder.setView(dialogView);
        AlertDialog dialog = builder.create();

        EditText etUrl = dialogView.findViewById(R.id.etIdmUrl);
        Button btnPaste = dialogView.findViewById(R.id.btnIdmPaste);
        LinearLayout layoutProbing = dialogView.findViewById(R.id.layoutIdmProbing);
        TextView tvProbingStatus = dialogView.findViewById(R.id.tvIdmProbingStatus);

        EditText etFilename = dialogView.findViewById(R.id.etIdmFilename);
        TextView tvSizeBadge = dialogView.findViewById(R.id.tvIdmSizeBadge);
        TextView tvResumeBadge = dialogView.findViewById(R.id.tvIdmResumeBadge);
        TextView tvTypeBadge = dialogView.findViewById(R.id.tvIdmTypeBadge);
        Spinner spCategory = dialogView.findViewById(R.id.spIdmCategory);
        TextView tvSavePath = dialogView.findViewById(R.id.tvIdmSavePath);

        Button btnCancel = dialogView.findViewById(R.id.btnIdmCancel);
        Button btnQueueLater = dialogView.findViewById(R.id.btnIdmQueueLater);
        Button btnStartNow = dialogView.findViewById(R.id.btnIdmStartNow);

        final String[] categories = new String[]{
                "Auto-Detect",
                SmartCategoryManager.CAT_VIDEOS,
                SmartCategoryManager.CAT_MUSIC,
                SmartCategoryManager.CAT_DOCS,
                SmartCategoryManager.CAT_IMAGES,
                SmartCategoryManager.CAT_ARCHIVES,
                SmartCategoryManager.CAT_APPS,
                SmartCategoryManager.CAT_OTHER
        };

        ArrayAdapter<String> adapter = new ArrayAdapter<>(this, android.R.layout.simple_spinner_dropdown_item, categories);
        spCategory.setAdapter(adapter);

        final String[] detectedCategory = new String[]{SmartCategoryManager.CAT_OTHER};

        Runnable updateFolderPreview = () -> {
            int selectedPos = spCategory.getSelectedItemPosition();
            String cat = selectedPos > 0 ? categories[selectedPos] : detectedCategory[0];
            File dir = SmartCategoryManager.getCategoryDir(this, cat);
            tvSavePath.setText("Save to: " + dir.getAbsolutePath());
        };

        spCategory.setOnItemSelectedListener(new AdapterView.OnItemSelectedListener() {
            @Override
            public void onItemSelected(AdapterView<?> parent, View view, int position, long id) {
                updateFolderPreview.run();
            }
            @Override
            public void onNothingSelected(AdapterView<?> parent) {}
        });

        // Async probing worker
        Runnable probeTask = () -> {
            String url = etUrl.getText().toString().trim();
            if (url.isEmpty() || !url.startsWith("http")) return;

            layoutProbing.setVisibility(View.VISIBLE);
            tvProbingStatus.setText("Contacting server & probing metadata...");

            new Thread(() -> {
                NativeBridge.ProbeResult probe = NativeBridge.probe(url);
                runOnUiThread(() -> {
                    layoutProbing.setVisibility(View.GONE);
                    if (probe != null && probe.error == null) {
                        String fname = (probe.filename != null && !probe.filename.isEmpty())
                                ? probe.filename
                                : DownloadQueueManager.extractFilenameFromUrl(url, 0);
                        etFilename.setText(fname);

                        if (probe.contentLength > 0) {
                            tvSizeBadge.setText("Size: " + DownloadTask.formatBytes(probe.contentLength));
                        } else {
                            tvSizeBadge.setText("Size: Dynamic / Unknown");
                        }

                        if (probe.acceptRanges) {
                            tvResumeBadge.setText("✓ Resumable");
                            tvResumeBadge.setTextColor(getResources().getColor(R.color.accent_emerald));
                        } else {
                            tvResumeBadge.setText("⚠ Non-resumable");
                            tvResumeBadge.setTextColor(getResources().getColor(R.color.accent_amber));
                        }

                        if (probe.contentType != null && !probe.contentType.isEmpty()) {
                            tvTypeBadge.setText("Type: " + probe.contentType);
                        }

                        // Auto-classify category based on probed name & mime type
                        String autoCat = SmartCategoryManager.classify(fname, probe.contentType);
                        detectedCategory[0] = autoCat;
                        if (spCategory.getSelectedItemPosition() == 0) {
                            for (int i = 1; i < categories.length; i++) {
                                if (categories[i].equalsIgnoreCase(autoCat)) {
                                    spCategory.setSelection(i);
                                    break;
                                }
                            }
                        }
                        updateFolderPreview.run();
                    } else {
                        // Fallback gracefully
                        String fallbackName = DownloadQueueManager.extractFilenameFromUrl(url, 0);
                        etFilename.setText(fallbackName);
                        tvSizeBadge.setText("Size: Unknown");
                        tvResumeBadge.setText("Single-stream fallback");
                        tvResumeBadge.setTextColor(getResources().getColor(R.color.accent_amber));
                        updateFolderPreview.run();
                    }
                });
            }).start();
        };

        if (defaultUrl != null && !defaultUrl.isEmpty()) {
            etUrl.setText(defaultUrl);
            probeTask.run();
        }

        btnPaste.setOnClickListener(v -> {
            try {
                ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
                if (cm != null && cm.hasPrimaryClip() && cm.getPrimaryClip().getItemCount() > 0) {
                    CharSequence text = cm.getPrimaryClip().getItemAt(0).getText();
                    if (text != null) {
                        String clean = text.toString().trim();
                        etUrl.setText(clean);
                        probeTask.run();
                    }
                }
            } catch (Exception e) {
                e.printStackTrace();
            }
        });

        // Trigger probe on focus change
        etUrl.setOnFocusChangeListener((v, hasFocus) -> {
            if (!hasFocus) {
                probeTask.run();
            }
        });

        btnCancel.setOnClickListener(v -> dialog.dismiss());

        btnQueueLater.setOnClickListener(v -> {
            String url = etUrl.getText().toString().trim();
            String filename = etFilename.getText().toString().trim();
            if (url.isEmpty()) {
                Toast.makeText(this, "Please enter a valid URL", Toast.LENGTH_SHORT).show();
                return;
            }
            int selectedPos = spCategory.getSelectedItemPosition();
            String cat = selectedPos > 0 ? categories[selectedPos] : detectedCategory[0];

            mQueueMgr.enqueue(url, filename, cat, false);
            dialog.dismiss();
            Toast.makeText(this, "Added to queue: " + filename, Toast.LENGTH_SHORT).show();
            switchTab(0);
        });

        btnStartNow.setOnClickListener(v -> {
            String url = etUrl.getText().toString().trim();
            String filename = etFilename.getText().toString().trim();
            if (url.isEmpty()) {
                Toast.makeText(this, "Please enter a valid URL", Toast.LENGTH_SHORT).show();
                return;
            }
            int selectedPos = spCategory.getSelectedItemPosition();
            String cat = selectedPos > 0 ? categories[selectedPos] : detectedCategory[0];

            mQueueMgr.enqueue(url, filename, cat, true);
            dialog.dismiss();
            Toast.makeText(this, "Downloading: " + filename, Toast.LENGTH_SHORT).show();
            switchTab(0);
        });

        dialog.show();
    }

    // ==========================================
    // Browser & Media Sniffing Integration
    // ==========================================
    private void setupBrowser() {
        mWebView = findViewById(R.id.webView);
        mEtBrowserUrl = findViewById(R.id.etBrowserUrl);
        mPbBrowserLoading = findViewById(R.id.pbBrowserLoading);
        mSnifferPill = findViewById(R.id.snifferPill);
        mTvSnifferText = findViewById(R.id.tvSnifferText);

        Button btnBack = findViewById(R.id.btnBrowserBack);
        Button btnForward = findViewById(R.id.btnBrowserForward);
        Button btnGo = findViewById(R.id.btnBrowserGo);

        WebSettings ws = mWebView.getSettings();
        ws.setJavaScriptEnabled(true);
        ws.setDomStorageEnabled(true);
        ws.setMediaPlaybackRequiresUserGesture(true);
        ws.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);

        mWebView.addJavascriptInterface(new MediaSniffer.JsBridge(mMediaSniffer), "mediaBridge");

        mWebView.setWebViewClient(new WebViewClient() {
            @Override
            public WebResourceResponse shouldInterceptRequest(WebView view, WebResourceRequest request) {
                String url = request.getUrl().toString();
                if (AdBlocker.isAd(url)) {
                    return AdBlocker.createEmptyResource();
                }
                mMediaSniffer.inspectUrl(url, mCurrentPageTitle);
                return super.shouldInterceptRequest(view, request);
            }

            @Override
            public void onPageStarted(WebView view, String url, Bitmap favicon) {
                mPbBrowserLoading.setVisibility(View.VISIBLE);
                mEtBrowserUrl.setText(url);
                mCurrentPageTitle = url;
                mMediaSniffer.clear();
            }

            @Override
            public void onPageFinished(WebView view, String url) {
                mPbBrowserLoading.setVisibility(View.GONE);
                mMediaSniffer.injectDOMInspector(view, mCurrentPageTitle);
            }
        });

        mWebView.setWebChromeClient(new WebChromeClient() {
            @Override
            public void onProgressChanged(WebView view, int newProgress) {
                mPbBrowserLoading.setProgress(newProgress);
            }

            @Override
            public void onReceivedTitle(WebView view, String title) {
                super.onReceivedTitle(view, title);
                if (title != null && !title.trim().isEmpty()) {
                    mCurrentPageTitle = title;
                }
            }
        });

        btnBack.setOnClickListener(v -> { if (mWebView.canGoBack()) mWebView.goBack(); });
        btnForward.setOnClickListener(v -> { if (mWebView.canGoForward()) mWebView.goForward(); });

        View.OnClickListener goListener = v -> {
            String input = mEtBrowserUrl.getText().toString().trim();
            if (input.isEmpty()) return;
            if (!input.startsWith("http://") && !input.startsWith("https://")) {
                if (input.contains(".") && !input.contains(" ")) {
                    input = "https://" + input;
                } else {
                    input = "https://www.google.com/search?q=" + Uri.encode(input);
                }
            }
            mWebView.loadUrl(input);
        };
        btnGo.setOnClickListener(goListener);

        mMediaSniffer.setListener((item, count) -> runOnUiThread(() -> {
            if (count > 0 && item != null) {
                mSnifferPill.setVisibility(View.VISIBLE);
                mTvSnifferText.setText("📥 " + count + " Media Item(s) Detected - Tap to Download");
                mSnifferPill.setOnClickListener(v -> showSniffedMediaPicker(item));
            } else {
                mSnifferPill.setVisibility(View.GONE);
            }
        }));

        // Default homepage
        mWebView.loadUrl("https://www.google.com");
    }

    private void showSniffedMediaPicker(MediaSniffer.SniffedMediaItem item) {
        AlertDialog.Builder builder = new AlertDialog.Builder(this);
        View dialogView = LayoutInflater.from(this).inflate(R.layout.dialog_sniffed_media, null);
        builder.setView(dialogView);
        AlertDialog dialog = builder.create();

        TextView tvTitle = dialogView.findViewById(R.id.tvSniffedTitle);
        TextView tvUrl = dialogView.findViewById(R.id.tvSniffedUrl);
        Button btnCancel = dialogView.findViewById(R.id.btnSniffedCancel);
        Button btnDownload = dialogView.findViewById(R.id.btnSniffedDownload);

        tvTitle.setText("📥 " + item.title);
        tvUrl.setText(item.url);

        btnCancel.setOnClickListener(v -> dialog.dismiss());
        btnDownload.setOnClickListener(v -> {
            mQueueMgr.enqueue(item.url, item.title, null);
            dialog.dismiss();
            Toast.makeText(this, "Download added to queue!", Toast.LENGTH_SHORT).show();
            switchTab(0);
        });

        dialog.show();
    }

    // ==========================================
    // UI Refresh Logic for Active & Done Cards
    // ==========================================
    private void refreshDownloadsUi() {
        List<DownloadTask> allTasks = new ArrayList<>(mQueueMgr.getTasks());

        // Sort tasks
        if (mSortMode == 0) { // Date DESC
            Collections.sort(allTasks, (a, b) -> Long.compare(b.createdAt, a.createdAt));
        } else if (mSortMode == 1) { // Size DESC
            Collections.sort(allTasks, (a, b) -> Long.compare(b.totalBytes, a.totalBytes));
        } else { // Name ASC
            Collections.sort(allTasks, (a, b) -> a.filename.compareToIgnoreCase(b.filename));
        }

        List<DownloadTask> activeTasks = new ArrayList<>();
        List<DownloadTask> completedTasks = new ArrayList<>();
        double globalSpeed = 0;

        for (DownloadTask t : allTasks) {
            if (!mFilterAll && !t.category.equalsIgnoreCase(mCurrentCategoryFilter)) {
                continue;
            }
            if (t.status == DownloadState.COMPLETED) {
                completedTasks.add(t);
            } else {
                activeTasks.add(t);
                if (t.status == DownloadState.DOWNLOADING) {
                    globalSpeed += t.speedBytesSec;
                }
            }
        }

        mTvGlobalSpeed.setText(String.format(java.util.Locale.US, "%.1f MB/s", globalSpeed / (1024.0 * 1024.0)));
        mTvActiveHeader.setText("Active Downloads (" + activeTasks.size() + ")");
        mTvCompletedHeader.setText("Completed Downloads (" + completedTasks.size() + ")");

        // Rebuild Active Cards
        mActiveContainer.removeAllViews();
        mActiveCardViews.clear();
        LayoutInflater inflater = LayoutInflater.from(this);

        for (DownloadTask task : activeTasks) {
            View card = inflater.inflate(R.layout.item_download_active, mActiveContainer, false);
            bindActiveCard(card, task);
            mActiveContainer.addView(card);
            mActiveCardViews.put(task.id, card);
        }

        // Rebuild Completed Cards
        mCompletedContainer.removeAllViews();
        for (DownloadTask task : completedTasks) {
            View card = inflater.inflate(R.layout.item_download_done, mCompletedContainer, false);
            bindCompletedCard(card, task);
            mCompletedContainer.addView(card);
        }
    }

    private void bindActiveCard(View card, DownloadTask task) {
        TextView tvFilename = card.findViewById(R.id.tvActiveFilename);
        TextView tvBadge = card.findViewById(R.id.tvActiveCategoryBadge);
        ProgressBar pb = card.findViewById(R.id.pbActiveProgress);
        TextView tvBytes = card.findViewById(R.id.tvActiveBytes);
        TextView tvPercent = card.findViewById(R.id.tvActivePercent);
        TextView tvSpeedEta = card.findViewById(R.id.tvActiveSpeedEta);
        Button btnPause = card.findViewById(R.id.btnActivePauseResume);
        Button btnCancel = card.findViewById(R.id.btnActiveCancel);

        tvFilename.setText(task.filename);
        tvBadge.setText(task.category);
        pb.setProgress(task.getPercent() * 10);
        tvBytes.setText(task.getFormattedSize());
        tvPercent.setText(task.getPercent() + "%");

        if (task.status == DownloadState.DOWNLOADING) {
            tvSpeedEta.setText("⚡ " + task.getFormattedSpeed() + " • ETA: " + task.getFormattedEta() + " • " + task.activeWorkers + "w");
            btnPause.setText("Pause");
            btnPause.setBackgroundResource(R.drawable.bg_button_cyan);
            btnPause.setTextColor(getResources().getColor(R.color.bg_dark));
            btnPause.setOnClickListener(v -> mQueueMgr.pause(task.id));
        } else if (task.status == DownloadState.PAUSED) {
            tvSpeedEta.setText("⏸ Paused");
            btnPause.setText("Resume");
            btnPause.setBackgroundResource(R.drawable.bg_button_dark);
            btnPause.setTextColor(getResources().getColor(R.color.text_white));
            btnPause.setOnClickListener(v -> mQueueMgr.resume(task.id));
        } else if (task.status == DownloadState.QUEUED) {
            tvSpeedEta.setText("⏳ Queued in line...");
            btnPause.setText("Pause");
            btnPause.setOnClickListener(v -> mQueueMgr.pause(task.id));
        } else if (task.status == DownloadState.ERROR) {
            tvSpeedEta.setText("❌ " + (task.errorMessage != null ? task.errorMessage : "Error"));
            btnPause.setText("Retry");
            btnPause.setOnClickListener(v -> mQueueMgr.resume(task.id));
        }

        btnCancel.setOnClickListener(v -> mQueueMgr.cancel(task.id, true));
    }

    private void bindCompletedCard(View card, DownloadTask task) {
        TextView tvFilename = card.findViewById(R.id.tvDoneFilename);
        TextView tvBadge = card.findViewById(R.id.tvDoneCategoryBadge);
        TextView tvDetails = card.findViewById(R.id.tvDoneDetails);
        Button btnOpen = card.findViewById(R.id.btnDoneOpen);
        Button btnShare = card.findViewById(R.id.btnDoneShare);
        Button btnDelete = card.findViewById(R.id.btnDoneDelete);

        tvFilename.setText(task.filename);
        tvBadge.setText(task.category);
        tvDetails.setText(task.getFormattedTotalSize() + " • " + (task.destPath != null ? task.destPath : "Saved"));

        btnOpen.setOnClickListener(v -> FileManagerHelper.openFile(this, task));
        btnShare.setOnClickListener(v -> FileManagerHelper.shareFile(this, task));
        btnDelete.setOnClickListener(v -> {
            FileManagerHelper.deleteTaskAndFile(this, task);
            refreshDownloadsUi();
        });
    }

    // ==========================================
    // Files Explorer & Settings
    // ==========================================
    private void refreshFilesUi() {
        mFilesContainer.removeAllViews();
        List<DownloadTask> tasks = mQueueMgr.getTasks();
        long totalDownloadedBytes = 0;

        LayoutInflater inflater = LayoutInflater.from(this);
        for (DownloadTask t : tasks) {
            if (t.status == DownloadState.COMPLETED) {
                totalDownloadedBytes += t.totalBytes;
                View card = inflater.inflate(R.layout.item_download_done, mFilesContainer, false);
                bindCompletedCard(card, t);
                mFilesContainer.addView(card);
            }
        }

        // Compute Free Storage
        try {
            File path = Environment.getExternalStorageDirectory();
            StatFs stat = new StatFs(path.getPath());
            long freeBytes = stat.getAvailableBlocksLong() * stat.getBlockSizeLong();
            mTvStorageDetails.setText("Downloaded: " + DownloadTask.formatBytes(totalDownloadedBytes) +
                    " • Free Storage: " + DownloadTask.formatBytes(freeBytes));
        } catch (Exception e) {
            mTvStorageDetails.setText("Downloaded: " + DownloadTask.formatBytes(totalDownloadedBytes));
        }
    }

    private void setupSettings() {
        Switch switchWifi = findViewById(R.id.switchWifiOnly);
        Switch switchAd = findViewById(R.id.switchAdBlock);
        TextView tvCurrentStorage = findViewById(R.id.tvCurrentStoragePath);
        Button btnChangeStorage = findViewById(R.id.btnChangeStorageLocation);

        switchWifi.setChecked(mQueueMgr.isWifiOnly());
        switchWifi.setOnCheckedChangeListener((btn, isChecked) -> mQueueMgr.setWifiOnly(isChecked));

        switchAd.setChecked(AdBlocker.isEnabled());
        switchAd.setOnCheckedChangeListener((btn, isChecked) -> AdBlocker.setEnabled(isChecked));

        btnChangeStorage.setOnClickListener(v -> {
            Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE);
            startActivityForResult(intent, 2001);
        });

        setupConcurrencyButtons();
    }

    private void setupConcurrencyButtons() {
        Button b4 = findViewById(R.id.btnConc4);
        Button b8 = findViewById(R.id.btnConc8);
        Button b16 = findViewById(R.id.btnConc16);
        Button b32 = findViewById(R.id.btnConc32);
        TextView tvDesc = findViewById(R.id.tvConcurrencyDesc);

        View.OnClickListener listener = v -> {
            b4.setBackgroundResource(R.drawable.bg_button_dark); b4.setTextColor(getResources().getColor(R.color.text_white));
            b8.setBackgroundResource(R.drawable.bg_button_dark); b8.setTextColor(getResources().getColor(R.color.text_white));
            b16.setBackgroundResource(R.drawable.bg_button_dark); b16.setTextColor(getResources().getColor(R.color.text_white));
            b32.setBackgroundResource(R.drawable.bg_button_dark); b32.setTextColor(getResources().getColor(R.color.text_white));

            Button chosen = (Button) v;
            chosen.setBackgroundResource(R.drawable.bg_button_cyan);
            chosen.setTextColor(getResources().getColor(R.color.bg_dark));

            int conc = Integer.parseInt(chosen.getText().toString());
            mQueueMgr.setEngineConcurrency(conc);
            tvDesc.setText("Current: " + conc + " connections");
        };

        b4.setOnClickListener(listener);
        b8.setOnClickListener(listener);
        b16.setOnClickListener(listener);
        b32.setOnClickListener(listener);
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == 2001 && resultCode == RESULT_OK && data != null && data.getData() != null) {
            Uri treeUri = data.getData();
            int flags = data.getFlags() & (Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
            getContentResolver().takePersistableUriPermission(treeUri, flags);
            mQueueMgr.setCustomTree(treeUri);
            TextView tv = findViewById(R.id.tvCurrentStoragePath);
            tv.setText(treeUri.toString());
            Toast.makeText(this, "Storage location updated!", Toast.LENGTH_SHORT).show();
        }
    }

    // ==========================================
    // Engine Callback Implementations
    // ==========================================
    @Override
    public void onQueueUpdated() {
        runOnUiThread(this::refreshDownloadsUi);
    }

    @Override
    public void onTaskProgress(DownloadTask task) {
        runOnUiThread(() -> {
            View card = mActiveCardViews.get(task.id);
            if (card != null) {
                ProgressBar pb = card.findViewById(R.id.pbActiveProgress);
                TextView tvBytes = card.findViewById(R.id.tvActiveBytes);
                TextView tvPercent = card.findViewById(R.id.tvActivePercent);
                TextView tvSpeedEta = card.findViewById(R.id.tvActiveSpeedEta);

                pb.setProgress(task.getPercent() * 10);
                tvBytes.setText(task.getFormattedSize());
                tvPercent.setText(task.getPercent() + "%");
                tvSpeedEta.setText("⚡ " + task.getFormattedSpeed() + " • ETA: " + task.getFormattedEta() + " • " + task.activeWorkers + "w");
            }
        });
    }

    @Override
    public void onTaskCompleted(DownloadTask task) {
        runOnUiThread(this::refreshDownloadsUi);
    }

    @Override
    public void onTaskFailed(DownloadTask task, String error) {
        runOnUiThread(this::refreshDownloadsUi);
    }

    @Override
    protected void onDestroy() {
        super.onDestroy();
        mQueueMgr.removeListener(this);
    }
}
