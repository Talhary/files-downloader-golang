param (
    [switch]$SkipNative = $false,
    [string]$TargetAbi = "all",
    [string]$KeystorePath = $env:DLENGINE_KEYSTORE
)

$ErrorActionPreference = "Stop"

$AppDir = $PSScriptRoot
$RootDir = Split-Path $AppDir -Parent
$SdkRoot = $env:ANDROID_HOME
if (-not $SdkRoot) { $SdkRoot = $env:ANDROID_SDK_ROOT }
if (-not $SdkRoot) {
    $sdkCandidates = @(
        (Join-Path $env:LOCALAPPDATA "Android\Sdk"),
        (Join-Path $env:USERPROFILE "AppData\Local\Android\Sdk"),
        "C:\Android\Sdk"
    )
    $SdkRoot = $sdkCandidates | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
}
if (-not $SdkRoot -or -not (Test-Path -LiteralPath $SdkRoot)) {
    throw "Android SDK not found. Set ANDROID_HOME or ANDROID_SDK_ROOT."
}

$PlatformDir = Get-ChildItem -LiteralPath (Join-Path $SdkRoot "platforms") -Directory -ErrorAction Stop |
    Sort-Object { [int]($_.Name -replace '^android-', '') } -Descending |
    Select-Object -First 1
$PlatformJar = Join-Path $PlatformDir.FullName "android.jar"

$BuildTools = Get-ChildItem -LiteralPath (Join-Path $SdkRoot "build-tools") -Directory -ErrorAction Stop |
    Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName "aapt2.exe") } |
    Sort-Object { try { [version]$_.Name } catch { [version]"0.0.0" } } -Descending |
    Select-Object -First 1
if (-not $BuildTools) {
    throw "No Android build-tools installation found under $SdkRoot."
}

$Aapt2 = Join-Path $BuildTools.FullName "aapt2.exe"
$D8Jar = Join-Path $BuildTools.FullName "lib\d8.jar"
$Zipalign = Join-Path $BuildTools.FullName "zipalign.exe"
$ApkSigner = Join-Path $BuildTools.FullName "apksigner.bat"

$JdkBin = $null
if ($env:JAVA_HOME -and (Test-Path -LiteralPath (Join-Path $env:JAVA_HOME "bin\javac.exe"))) {
    $JdkBin = Join-Path $env:JAVA_HOME "bin"
} else {
    $JavacCommand = Get-Command javac.exe -ErrorAction SilentlyContinue
    if ($JavacCommand) { $JdkBin = Split-Path $JavacCommand.Source -Parent }
}
if (-not $JdkBin) {
    throw "JDK 17 or newer not found. Set JAVA_HOME or add javac.exe to PATH."
}
$JarTool = Join-Path $JdkBin "jar.exe"
$Javac = Join-Path $JdkBin "javac.exe"
$Java = Join-Path $JdkBin "java.exe"
$Keytool = Join-Path $JdkBin "keytool.exe"
foreach ($Tool in @($Aapt2, $D8Jar, $Zipalign, $ApkSigner, $JarTool, $Javac, $Java, $Keytool)) {
    if (-not (Test-Path -LiteralPath $Tool)) { throw "Required tool not found: $Tool" }
}

$NdkDir = Get-ChildItem -LiteralPath (Join-Path $SdkRoot "ndk") -Directory -ErrorAction Stop |
    Sort-Object { try { [version]$_.Name } catch { [version]"0.0.0" } } -Descending |
    Select-Object -First 1
if (-not $NdkDir) { throw "Android NDK not found under $SdkRoot." }
$LlvmBin = Get-ChildItem -LiteralPath (Join-Path $NdkDir.FullName "toolchains\llvm\prebuilt") -Directory |
    Select-Object -First 1 | ForEach-Object { Join-Path $_.FullName "bin" }
if (-not $LlvmBin -or -not (Test-Path -LiteralPath $LlvmBin)) {
    throw "LLVM toolchain not found in NDK $($NdkDir.FullName)."
}

$Keystore = if ($KeystorePath) { [IO.Path]::GetFullPath($KeystorePath) } else { $null }
$KeystorePassword = $env:DLENGINE_KEYSTORE_PASSWORD
$KeyPassword = $env:DLENGINE_KEY_PASSWORD
$KeyAlias = $env:DLENGINE_KEY_ALIAS
if (-not $Keystore -or -not (Test-Path -LiteralPath $Keystore)) {
    throw "Release keystore not found. Set DLENGINE_KEYSTORE or pass -KeystorePath."
}
if (-not $KeystorePassword -or -not $KeyPassword -or -not $KeyAlias) {
    throw "Set DLENGINE_KEYSTORE_PASSWORD, DLENGINE_KEY_PASSWORD, and DLENGINE_KEY_ALIAS."
}

Write-Host "=========================================================" -ForegroundColor Cyan
Write-Host " [1/7] Validating Toolchain" -ForegroundColor Cyan
Write-Host "=========================================================" -ForegroundColor Cyan
Write-Host "SDK Platform: $PlatformJar"
Write-Host "Build Tools:  $($BuildTools.FullName)"
Write-Host "NDK Root:     $($NdkDir.FullName)"
Write-Host "JDK:          $JdkBin"
& $Javac -version
& $Aapt2 version
& $ApkSigner version

$BuildDir = Join-Path $AppDir "build"
$BinDir = Join-Path $AppDir "bin"
foreach ($Directory in @(
    (Join-Path $BuildDir "compiled_res"),
    (Join-Path $BuildDir "gen"),
    (Join-Path $BuildDir "classes"),
    (Join-Path $BuildDir "dex"),
    $BinDir
)) {
    if (-not (Test-Path -LiteralPath $Directory)) {
        New-Item -ItemType Directory -Path $Directory -Force | Out-Null
    }
}

Write-Host "`n[2/7] Compiling Go Native Engine" -ForegroundColor Cyan
$compileAbi = {
    param($abi, $arch, $clangCmd)
    $outDir = Join-Path $BuildDir "lib\$abi"
    if (-not (Test-Path -LiteralPath $outDir)) { New-Item -ItemType Directory -Path $outDir -Force | Out-Null }
    $outFile = Join-Path $outDir "libdlengine.so"
    $env:CGO_ENABLED = "1"
    $env:GOOS = "android"
    $env:GOARCH = $arch
    $env:CC = Join-Path $LlvmBin $clangCmd
    Push-Location $RootDir
    try {
        go build -trimpath -buildmode=c-shared -ldflags="-s -w" -o $outFile ./cmd/libdlengine
        if ($LASTEXITCODE -ne 0) { throw "Go native compilation failed for $abi" }
    } finally {
        Pop-Location
    }
    Write-Host "Built: $outFile" -ForegroundColor Green
}

if (-not $SkipNative) {
    if ($TargetAbi -notin @("all", "arm64", "x86_64")) {
        throw "TargetAbi must be all, arm64, or x86_64."
    }
    if ($TargetAbi -in @("all", "arm64")) {
        & $compileAbi "arm64-v8a" "arm64" "aarch64-linux-android29-clang.cmd"
    }
    if ($TargetAbi -in @("all", "x86_64")) {
        & $compileAbi "x86_64" "amd64" "x86_64-linux-android29-clang.cmd"
    }
}

Write-Host "`n[3/7] Compiling Android Resources" -ForegroundColor Cyan
& $Aapt2 compile --dir (Join-Path $AppDir "app\src\main\res") -o (Join-Path $BuildDir "compiled_res.zip")
if ($LASTEXITCODE -ne 0) { throw "aapt2 compile failed" }
& $Aapt2 link -o (Join-Path $BuildDir "app-base.apk") `
    -I $PlatformJar `
    --manifest (Join-Path $AppDir "app\src\main\AndroidManifest.xml") `
    --java (Join-Path $BuildDir "gen") `
    (Join-Path $BuildDir "compiled_res.zip") `
    --auto-add-overlay
if ($LASTEXITCODE -ne 0) { throw "aapt2 link failed" }

Write-Host "`n[4/7] Compiling Java Sources" -ForegroundColor Cyan
$JavaFiles = Get-ChildItem -Path (Join-Path $AppDir "app\src\main\java"), (Join-Path $BuildDir "gen") -Filter "*.java" -Recurse | Select-Object -ExpandProperty FullName
& $Javac -g:none -encoding UTF-8 -cp "$PlatformJar;$(Join-Path $BuildDir 'gen')" -d (Join-Path $BuildDir "classes") $JavaFiles
if ($LASTEXITCODE -ne 0) { throw "javac failed" }

Write-Host "`n[5/7] Converting Bytecode to DEX" -ForegroundColor Cyan
$ClassFiles = Get-ChildItem -Path (Join-Path $BuildDir "classes") -Filter "*.class" -Recurse | Select-Object -ExpandProperty FullName
& $Java -cp $D8Jar com.android.tools.r8.D8 --release --min-api 24 --lib $PlatformJar --output (Join-Path $BuildDir "dex") $ClassFiles
if ($LASTEXITCODE -ne 0) { throw "D8 conversion failed" }

Write-Host "`n[6/7] Packaging Unsigned APK" -ForegroundColor Cyan
Copy-Item (Join-Path $BuildDir "app-base.apk") (Join-Path $BuildDir "app-unsigned.apk") -Force
Push-Location (Join-Path $BuildDir "dex")
try {
    & $JarTool -uf (Join-Path $BuildDir "app-unsigned.apk") classes.dex
} finally {
    Pop-Location
}
$NativeLibPaths = Get-ChildItem -Path (Join-Path $BuildDir "lib") -Filter "*.so" -Recurse -ErrorAction SilentlyContinue | ForEach-Object {
    $_.FullName.Substring($BuildDir.Length + 1).Replace('\', '/')
}
if ($NativeLibPaths) {
    Push-Location $BuildDir
    try {
        & $JarTool -u0f (Join-Path $BuildDir "app-unsigned.apk") $NativeLibPaths
    } finally {
        Pop-Location
    }
}

Write-Host "`n[7/7] Aligning and Signing APK" -ForegroundColor Cyan
& $Zipalign -p -f 4 (Join-Path $BuildDir "app-unsigned.apk") (Join-Path $BuildDir "app-aligned.apk")
if ($LASTEXITCODE -ne 0) { throw "zipalign failed" }
$FinalApk = Join-Path $BinDir "DL_Engine_Pro-release.apk"
& $ApkSigner sign --ks $Keystore --ks-key-alias $KeyAlias --ks-pass "pass:$KeystorePassword" --key-pass "pass:$KeyPassword" --out $FinalApk (Join-Path $BuildDir "app-aligned.apk")
if ($LASTEXITCODE -ne 0) { throw "apksigner failed" }
& $ApkSigner verify --verbose $FinalApk
if ($LASTEXITCODE -ne 0) { throw "APK signature verification failed" }

Write-Host "`nBUILD SUCCESSFUL: $FinalApk" -ForegroundColor Green
