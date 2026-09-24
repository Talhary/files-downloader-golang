# Telenor Claim Pro — Complete System, Security & Build Documentation

---

## Table of Contents
1. [APK Overview & Metadata](#1-apk-overview--metadata)
2. [Host System Build Tools & Dependencies](#2-host-system-build-tools--dependencies)
3. [Automated Build Pipeline (`build.ps1`)](#3-automated-build-pipeline-buildps1)
4. [Security Architecture & Protocols](#4-security-architecture--protocols)
   - [Ring 1: Native Self-Integrity & Anti-Repackaging](#ring-1-native-self-integrity--anti-repackaging)
   - [Ring 2: Native Dynamic Hook & Debugger Resistance](#ring-2-native-dynamic-hook--debugger-resistance)
   - [Ring 3: Anti-Root & Virtual Container Sandboxing](#ring-3-anti-root--virtual-container-sandboxing)
   - [Ring 4: Network Security, TLS & Anti-MITM Controls](#ring-4-network-security-tls--anti-mitm-controls)
   - [Ring 5: Memory Obfuscation, Cryptography & UI Shielding](#ring-5-memory-obfuscation-cryptography--ui-shielding)
5. [Application Architecture & Network Engine](#5-application-architecture--network-engine)
   - [Architectural Topology](#architectural-topology)
   - [End-to-End Operational Flow](#end-to-end-operational-flow)
   - [High-Concurrency Burst Claim Engine (Dual CountDownLatch)](#high-concurrency-burst-claim-engine-dual-countdownlatch)
6. [Complete Codebase File Directory Reference](#6-complete-codebase-file-directory-reference)

---

## 1. APK Overview & Metadata

| Attribute | Specification |
| :--- | :--- |
| **Application Name** | Telenor Claim Pro |
| **Package Name** | `com.telenor.claim` |
| **Version Code** | `4` |
| **Version Name** | `1.3.0` |
| **Min SDK** | API Level 21 (Android 5.0 Lollipop) |
| **Target SDK** | API Level 34 (Android 14) |
| **Output Path** | `bin/TelenorClaimPro-release.apk` |
| **Binary Size** | ~150 KB (Fully self-contained, no external runtime bloat) |
| **Native Library** | `libtelenorcore.so` |
| **Target ABIs** | `arm64-v8a`, `armeabi-v7a`, `x86_64`, `x86` |
| **Permissions** | `android.permission.INTERNET`, `android.permission.ACCESS_NETWORK_STATE` |
| **Application Attributes** | `android:allowBackup="false"`, `android:usesCleartextTraffic="false"`, `android:networkSecurityConfig="@xml/network_security_config"` |

---

## 2. Host System Build Tools & Dependencies

The application does not rely on Gradle wrappers or daemon processes. Instead, it is built directly with native platform binaries and SDK utilities:

### 2.1 Java Development Kit (JDK)
- **Version**: OpenJDK 17.0.18 LTS (`Microsoft-13106358 (build 17.0.18+8-LTS)`)
- **Installation Directory**: `C:\Program Files\Microsoft\jdk-17.0.18.8-hotspot\bin`
- **Utilized Binaries**:
  - `javac.exe` (Java compiler)
  - `java.exe` (Java runtime for R8/D8 execution)
  - `jar.exe` (Archive assembly)
  - `keytool.exe` (Keystore and certificate management)

### 2.2 Android SDK & Platform
- **SDK Root**: `C:\android`
- **Platform Framework**: `C:\android\platforms\android-34\android.jar` (Android 14, API 34)

### 2.3 Android Build Tools
- **Version**: `34.0.0`
- **Directory**: `C:\android\build-tools\34.0.0`
- **Utilized Binaries**:
  - `aapt2.exe`: Compiles resource trees and links manifests into base APK containers.
  - `lib\d8.jar`: Executes the Google R8 engine (`com.android.tools.r8.R8`) for dead-code elimination, member inlining, and bytecode obfuscation.
  - `zipalign.exe`: Page-aligns uncompressed resources and libraries to 4-byte boundaries.
  - `apksigner.bat`: Implements APK Signature Schemes v1 (JAR), v2 (APK Signing Block), and v3.

### 2.4 Android NDK (Native Development Kit)
- **Version**: `26.1.10909125` (NDK r26b)
- **Installation Directory**: `C:\android\ndk\26.1.10909125`
- **Cross-Compiler Toolchain**: `C:\android\ndk\26.1.10909125\toolchains\llvm\prebuilt\windows-x86_64\bin`
  - `aarch64-linux-android29-clang.cmd` (ARM 64-bit)
  - `armv7a-linux-androideabi29-clang.cmd` (ARM 32-bit)
  - `x86_64-linux-android29-clang.cmd` (x86 64-bit)
  - `i686-linux-android29-clang.cmd` (x86 32-bit)

### 2.5 Keystore
- **Path**: `release.keystore`
- **Algorithm**: RSA 2048-bit, self-signed certificate, valid for 10,000 days.
- **Alias**: `telenor`

---

## 3. Automated Build Pipeline (`build.ps1`)

The build pipeline executes deterministically through PowerShell:

```
[1/7] Environment & NDK Discovery
      ├── Scan C:\android\ndk for latest NDK
      └── Validate SDK Platform & Build-Tools 34.0.0
      ↓
[2/7] Keystore Extraction & Cryptographic Salt Ingestion
      ├── Extract SHA-256 certificate fingerprint from release.keystore
      ├── Generate 64-byte random XOR masks (RNG)
      ├── XOR-mask certificate fingerprint into secret_data.c
      └── Extract APP_PAYLOAD_KEY, compute SHA-256, and XOR-mask into secret_data.c
      ↓
[3/7] Native C/C++ Cross-Compilation (Clang)
      ├── Compile sha256.c, aes.c, native-sec.c, telenor-crypto.c, telenor-engine.c, secret_data.c
      ├── Flags: -shared -fPIC -Os -s -fvisibility=hidden -fdata-sections -ffunction-sections -Wl,--gc-sections -DNDEBUG
      └── Output: lib/arm64-v8a, lib/armeabi-v7a, lib/x86_64, lib/x86 (libtelenorcore.so)
      ↓
[4/7] Resource Compilation & Linking (AAPT2)
      ├── Compile app/src/main/res/ into compiled_res.zip
      └── Link with AndroidManifest.xml and android.jar -> generates R.java and app-base.apk
      ↓
[5/7] Java Source Compilation (Javac)
      └── Compile Java classes with -g:none (completely strips all debug symbols and line numbers)
      ↓
[6/7] Bytecode Optimization, Shrinking & Obfuscation (R8)
      ├── Run com.android.tools.r8.R8 --release --min-api 21 --pg-conf proguard-rules.pro
      ├── Repackage internal classes to com.telenor.claim.o
      ├── Strip android.util.Log calls
      └── Produce classes.dex
      ↓
[7/7] Packaging, 4-Byte Alignment & Cryptographic Signing
      ├── Merge classes.dex and uncompressed .so libraries into APK
      ├── Execute zipalign -p -f -v 4
      ├── Sign with apksigner (v1, v2, v3 schemes)
      └── Verify signature integrity and output to bin/TelenorClaimPro-release.apk
```

---

## 4. Security Architecture & Protocols

The security architecture implements defense-in-depth across five isolated layers:

### Ring 1: Native Self-Integrity & Anti-Repackaging
- **Binary-Level Certificate Pinning**: At runtime, `verify_apk_signature()` queries the Android `PackageManager` via JNI (`GET_SIGNATURES` / `GET_SIGNING_CERTIFICATES`).
- **Cryptographic Hash Comparison**: Computes the SHA-256 hash of the DER-encoded certificate bytes.
- **Constant-Time Memory Validation**: Unmasks the compile-time embedded certificate fingerprint on the stack and conducts a constant-time XOR comparison against the running certificate.
- **Fail-Closed Abort**: If the APK is re-signed with any key other than `release.keystore`, it executes `security_abort("REPACKAGED_APK_SIGNATURE_MISMATCH")`, instantly sending `SIGKILL` to its own PID and executing `abort()`.

### Ring 2: Native Dynamic Hook & Debugger Resistance
- **Process Memory Scanning**: `is_frida_detected()` continuously parses `/proc/self/maps` to catch injected libraries:
  - `frida-gadget`, `frida-agent`, `libgum.so`, `xposed.installer`, `substrate.so`.
- **Frida Daemon Port Probing**: Probes Frida’s default TCP ports (`27042` and `27043`) on `127.0.0.1` with a non-blocking 50ms socket timeout.
- **TracerPid Monitoring**: `is_debugger_attached()` inspects `/proc/self/status` for `TracerPid > 0`.
- **Dual-Layer Anti-Debugging**:
  - Native layer scans `TracerPid`.
  - Java layer checks `Debug.isDebuggerConnected()` and `Debug.waitingForDebugger()`.
- **Cryptographic Guard Gate**: `getSignature()` and `decryptPayload()` in `libtelenorcore.so` enforce a volatile state check (`s_security_verified == 0x5A7E1989`) and re-run root/Frida checks before completing any operation.

### Ring 3: Anti-Root & Virtual Container Sandboxing
- **Root Artifact Discovery**: `is_device_rooted()` probes 12 filesystem paths for root binaries (`/system/bin/su`, `/system/xbin/su`, `/sbin/su`, `/data/local/su`, `/sbin/.magisk`, `/system/app/Superuser.apk`).
- **Mount Integrity Inspection**: Inspects `/proc/mounts` for Magisk mounts (`magisk`, `core/mirror`).
- **Virtual Container / Clone Detection**:
  - Scans `/proc/self/maps` for virtual container hooks: `libva.so`, `libvapp.so`, `libiohook.so`, `libva++.so`, `libsandhook.so`, `libepic.so`, `libpine.so`, `libxhook.so`, `libdexposed.so`, `com.lbe.parallel`, `virtualapp`, `dualspace`, `sandv`, `vmos`.
  - Verifies private storage directories (`context.getFilesDir()`) against container keywords (`parallel`, `virtual`, `dual`, `clone`, `vapp`, `vmos`).
  - Verifies host process UID against package manager (`pm.getPackagesForUid(Process.myUid())`).

### Ring 4: Network Security, TLS & Anti-MITM Controls
- **Virtual Interface Auditing**: Scans `/proc/net/dev` and `/proc/net/route` for packet sniffing and VPN interfaces (`tun*`, `ppp*`, `tap*`, `canary*`, `p2p*`).
- **Active VPN & Proxy Probing**:
  - Inspects `NetworkCapabilities.TRANSPORT_VPN` via `ConnectivityManager`.
  - Probes local proxy ports (`8080`, `8888`, `7777`, `8787`).
  - Checks system HTTP proxies (`http.proxyHost`, `Settings.Global.HTTP_PROXY`).
  - Scans package manager for known sniffer utilities (`app.greyshirts.sslcapture`, `com.minhui.networkcapture`, `com.emanuelef.remote_capture`, `org.sandrop.webscarab`).
- **Proxy Neutralization**: All HTTP requests pass `Proxy.NO_PROXY` via `url.openConnection(Proxy.NO_PROXY)` to bypass system and Wi-Fi proxy configurations.
- **Strict Hostname Verification**: `STRICT_HOSTNAME_VERIFIER` ensures TLS handshakes are only permitted with `gw-asargon.kryptons.com.pk`, `galvorn.telenor.com.pk`, and authorized gateway hosts.
- **DigiCert CA Pinning**: Custom `X509TrustManager` enforces that all Telenor endpoints must have certificates signed by DigiCert, and explicitly inspects the entire chain to reject sniffer root CAs (`httpcanary`, `charles`, `burp`, `fiddler`, `portswigger`, `mitmproxy`).
- **Network Security Configuration**: `network_security_config.xml` pins trust anchors strictly to system CAs and blocks cleartext traffic.

### Ring 5: Memory Obfuscation, Cryptography & UI Shielding
- **Zero Plaintext Secrets**: Cryptographic salts and payload encryption keys are stored as XOR-masked byte arrays. Decryption occurs only in CPU registers and volatile stack space during hash execution.
- **Memory Zeroization**: Stack buffers are sanitized using `secure_wipe()` (volatile byte zeroing with an inline memory barrier) immediately after execution to prevent RAM dump forensics.
- **Encrypted Local Storage**: `DeviceHelper.java` encrypts device identifiers in `SharedPreferences` with AES-256-GCM using hardware-derived keys (`Build.MANUFACTURER + Build.MODEL + Build.BOARD`).
- **UI Shielding**: `WindowManager.LayoutParams.FLAG_SECURE` prevents screenshots, screen recordings, and preview caching in the Android Recent Apps switcher.
- **Real-Time Security Telemetry**: Security violations trigger an immediate synchronous telemetry payload to `/api/app/telemetry/security` prior to terminating the process.

---

## 5. Application Architecture & Network Engine

### Architectural Topology

```
┌─────────────────────────────────────────────────────────────┐
│                    MainActivity.java                        │
│  - Window FLAG_SECURE                                       │
│  - Form Controls, Live Real-time Statistics, Logs           │
└──────────────────────────────┬──────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                     TelenorApi.java                         │
│  - SSL Pinning & DigiCert Enforcement                       │
│  - Pre-warmed Socket Engine (High Concurrency)              │
│  - Telemetry & Encrypted Backend Handshake                  │
└──────────────┬──────────────────────────────┬───────────────┘
               │                              │
               ▼                              ▼
┌──────────────────────────────┐ ┌────────────────────────────┐
│      SecurityHelper.java     │ │     NativeBridge.java      │
│  - Virtual Container Guard   │ │  - JNI Native Interface    │
│  - VPN & Sniffer Detection   │ └────────────┬───────────────┘
└──────────────────────────────┘              │
                                              ▼
                                 ┌────────────────────────────┐
                                 │    libtelenorcore.so       │
                                 │  - native-sec.c            │
                                 │  - telenor-crypto.c        │
                                 │  - telenor-engine.c        │
                                 │  - sha256.c & aes.c        │
                                 └────────────────────────────┘
```

### End-to-End Operational Flow

1. **Gate Verification & Remote Handshake**:
   - `NativeBridge.verifySecurity(this)` validates all Ring 1–3 security requirements.
   - Handshake with `/api/app/handshake` authenticates the client and retrieves dynamic endpoint configurations (encrypted via AES-256-CBC + HMAC-SHA256).
2. **Authentication Flow**:
   - `login(phone)` sends MSISDN to `https://gw-asargon.kryptons.com.pk/api/v1/login`.
   - `verify(phone, otp)` submits SMS OTP to `/api/v1/verify` and obtains an `accessToken`.
   - `tokenSign(accessToken)` calls `https://galvorn.telenor.com.pk/api/v2/token/sign` to generate the `xenonToken` (V2).
   - `getCustomerInfo(phone, token)` queries `/api/v4/customer/info/{phone}` to retrieve `customerCode` and `packagePlan`.
3. **Daily Questions Acquisition**:
   - `fetchDailyQuestions()` requests daily trivia questions from `/api/qplayandwin/questions/daily/v2/{phone}`.
   - Includes fallback mechanisms to locally cached questions and embedded offline questions if network calls fail.
4. **500MB Target Auto-Roll**:
   - When enabled, repeatedly submits answers to `/api/qplayandwin/questions/answer/v3/submit/` until the server awards a 500MB allowance reward.

### High-Concurrency Burst Claim Engine (Dual CountDownLatch)

To maximize throughput against server rate limits, the batch claim engine uses a pre-warmed connection blast mechanism:

1. **Preparation Phase**:
   - Spawns a cached thread pool matching the target batch size (50 or 100 requests).
   - Each thread creates an `HttpURLConnection` to `/api/qplayandwin/questions/answer/v3/claim/` using `Proxy.NO_PROXY`.
   - Pre-negotiates TCP and TLS handshakes, computes native signatures, and opens output streams.
   - Each thread signals `readyGate.countDown()` and waits on `startGate.await()`.
2. **Burst Execution Phase**:
   - Once all connections are established (`readyGate.await(3000)`), the main orchestrator triggers `startGate.countDown()`.
   - All 50–100 threads write their payloads and flush their output streams simultaneously.
3. **Telemetry & Aggregation**:
   - Responses are read, reward MBs are summed, and results are emitted to the UI in real time.
   - Telemetry is dispatched to `/api/app/telemetry/claim`.

---

## 6. Complete Codebase File Directory Reference

```
d:\telenor-claim-bot\android-app\
│
├── AndroidManifest.xml          # App manifest, permissions, FLAG_SECURE config
├── build.ps1                    # Full automated 7-step build & signing script
├── proguard-rules.pro           # R8 obfuscation and optimization rules
├── release.keystore             # RSA-2048 signing keystore
├── README.md                    # Quickstart guide
├── DOCUMENTATION.md             # This comprehensive technical document
│
├── bin/
│   ├── TelenorClaimPro-release.apk       # Final production signed APK (~150 KB)
│   └── TelenorClaimPro-release.apk.idsig # APK signature scheme v4 idsig
│
└── app/src/main/
    ├── AndroidManifest.xml
    │
    ├── res/
    │   ├── drawable/            # Vector graphics, icons, button gradients
    │   ├── layout/
    │   │   └── activity_main.xml# Dark theme UI with stats and live logs
    │   ├── values/              # Strings, theme colors, styles
    │   └── xml/
    │       └── network_security_config.xml # Strict system-only CA configuration
    │
    ├── java/com/telenor/claim/
    │   ├── MainActivity.java    # UI controller, life-cycle, FLAG_SECURE
    │   ├── TelenorApi.java      # HTTP/TLS engine, socket burst, API calls
    │   ├── NativeBridge.java    # JNI interop bridge for libtelenorcore.so
    │   ├── SecurityHelper.java  # Java-layer VPN, sniffer, and container checks
    │   └── DeviceHelper.java    # Encrypted device metadata storage
    │
    └── cpp/
        ├── native-sec.c         # Root, Frida, TracerPid, cert hash checks
        ├── native-sec.h
        ├── telenor-crypto.c     # SHA-256 signature generator & payload decryptor
        ├── telenor-crypto.h
        ├── telenor-engine.c     # JNI exports and execution gatekeepers
        ├── secret_data.c        # In-memory XOR-masked salt and certificate hashes
        ├── secret_data.h
        ├── sha256.c             # Standalone C SHA-256 implementation
        ├── sha256.h
        ├── aes.c                # Standalone C AES-256 implementation
        └── aes.h
```
