# DL Engine Pro — Android Application

A high-performance, production-ready Android download manager powered by our multi-threaded Go download engine (`pkg/engine`) compiled directly into native C-shared shared libraries (`libdlengine.so`).

---

## 🚀 Key Features

### 1. Core Downloading Features
- **Pause & Resume**: Fully state-checkpointed downloads via `.dlstate.json`. Resumes interrupted downloads at exact chunk byte offsets after app restart or device reboot.
- **Multi-threaded Acceleration**: Dynamic chunk chunking (4, 8, 16, or 32 parallel worker connections) bypassing server bandwidth limits.
- **Background Foreground Service**: Android 14 compliant (`FOREGROUND_SERVICE_TYPE_DATA_SYNC`) with CPU `WakeLock` to ensure large files continue downloading when screen is off.
- **Concurrent Download Queue**: Configurable concurrent active downloads limit with automatic queued task promotion.
- **Scoped Storage & SAF (Storage Access Framework)**: Supports writing directly into Linux File Descriptors (`int fd`) via `os.NewFile()`, allowing seamless downloads to custom folders and external SD cards on Android 10–14.
- **Universal Format Support**: Automatic MIME and extension categorization into Videos, Music, Documents, Images, Archives, and Apps.

### 2. Built-in Browser & Sniffing
- **Chromium WebView**: Integrated modern browser with dark theme, omnibox, and search suggestions.
- **Automatic Media Sniffing**: Inspects network requests (`shouldInterceptRequest`) and DOM elements (`<video>`, `<audio>`, `<source>`, download links) for streaming URLs.
- **Floating Sniffer Pill**: Illuminates when media is detected on a page, opening a format and resolution selector (1080p, 720p, 480p, MP3).
- **Ad & Tracker Blocker**: Built-in fast domain blocker for popups, redirect ads, and tracking networks.
- **External Browser Integration**: `IntentFilter` intercepts links shared from Chrome, Firefox, or social media apps.

### 3. File Management & UI/UX
- **AMOLED Dark Theme**: Slate and electric cyan aesthetic tailored for modern Android devices.
- **Live Real-time Metrics**: Smooth 60fps card updates showing speed (`MB/s`), ETA countdown, downloaded/total bytes, and parallel worker count.
- **Interactive Notification Center**: Persistent notification shade with live progress and `[Pause]`, `[Resume]`, and `[Cancel]` action buttons.
- **Wi-Fi Only Mode**: Automatically pauses ongoing downloads when leaving Wi-Fi and resumes when Wi-Fi is restored.
- **File Actions**: 1-tap Open with system default player (`FileProvider`), Share Intent, and Delete with disk cleanup.

---

## 🛠️ Automated Build Pipeline (`build.ps1`)

The app uses a portable PowerShell build script matching the high-efficiency architecture documented in `docs/old_app/DOCS.md`. Set `ANDROID_HOME` (or `ANDROID_SDK_ROOT`), `JAVA_HOME`, `DLENGINE_KEYSTORE`, `DLENGINE_KEYSTORE_PASSWORD`, `DLENGINE_KEY_PASSWORD`, and `DLENGINE_KEY_ALIAS` before building:

```powershell
# From workspace root or android directory:
.\android\build.ps1
```

### Build Options:
- `-TargetAbi arm64` — Compiles specifically for ARM64 devices.
- `-TargetAbi x86_64` — Compiles specifically for emulators.
- `-TargetAbi all` — Compiles multi-ABI package (arm64-v8a + x86_64).
- `-SkipNative` — Skips Go recompilation if native `.so` files are already built.
- `-KeystorePath <path>` — Overrides `DLENGINE_KEYSTORE`.

Generated APKs, signing keys, native libraries, and intermediate files are ignored by Git. This app is covered by the repository [MIT License](../LICENSE).

### Output:
The signed, 4-byte aligned production APK is generated at:
`android/bin/DL_Engine_Pro-release.apk`
