//go:build windows

package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"download-engine/pkg/tunnel"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW      = user32.NewProc("RegisterClassExW")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procShowWindow            = user32.NewProc("ShowWindow")
	procUpdateWindow          = user32.NewProc("UpdateWindow")
	procGetMessageW           = user32.NewProc("GetMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procDispatchMessageW      = user32.NewProc("DispatchMessageW")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procSetWindowTextW        = user32.NewProc("SetWindowTextW")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	procSetTimer              = user32.NewProc("SetTimer")
	procKillTimer             = user32.NewProc("KillTimer")
	procGetKeyState           = user32.NewProc("GetKeyState")
	procGetFocus              = user32.NewProc("GetFocus")
	procIsDialogMessageW      = user32.NewProc("IsDialogMessageW")
	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	procCreateFontW           = gdi32.NewProc("CreateFontW")
)

const (
	WS_OVERLAPPED   = 0x00000000
	WS_CAPTION      = 0x00C00000
	WS_SYSMENU      = 0x00080000
	WS_THICKFRAME   = 0x00040000
	WS_MINIMIZEBOX  = 0x00020000
	WS_MAXIMIZEBOX  = 0x00010000
	WS_OVERLAPPEDWINDOW = (WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_MINIMIZEBOX | WS_MAXIMIZEBOX)
	WS_VISIBLE      = 0x10000000
	WS_CHILD        = 0x40000000
	WS_TABSTOP      = 0x00010000
	WS_BORDER       = 0x00800000
	WS_VSCROLL      = 0x00200000

	ES_LEFT         = 0x0000
	ES_AUTOHSCROLL  = 0x0080
	ES_AUTOVSCROLL  = 0x0040
	ES_MULTILINE    = 0x0004
	ES_PASSWORD     = 0x0020
	ES_READONLY     = 0x0800
	ES_WANTRETURN   = 0x1000

	BS_PUSHBUTTON   = 0x00000000
	BS_DEFPUSHBUTTON= 0x00000001
	BS_AUTOCHECKBOX = 0x00000003

	VK_CONTROL      = 0x11
	WM_KEYDOWN      = 0x0100
	WM_KEYUP        = 0x0101
	WM_CHAR         = 0x0102
	WM_CUT          = 0x0300
	WM_COPY         = 0x0301
	WM_PASTE        = 0x0302
	WM_CLEAR        = 0x0303
	WM_UNDO         = 0x0304

	WM_DESTROY      = 0x0002
	WM_COMMAND      = 0x0111
	WM_TIMER        = 0x0113
	WM_SETFONT      = 0x0030
	WM_GETTEXT      = 0x000D
	WM_SETTEXT      = 0x000C
	WM_GETTEXTLENGTH= 0x000E

	BM_GETCHECK     = 0x00F0
	BM_SETCHECK     = 0x00F1
	BST_UNCHECKED   = 0x0000
	BST_CHECKED     = 0x0001

	EM_SETSEL       = 0x00B1
	EM_REPLACESEL   = 0x00C2
	EM_SCROLLCARET  = 0x00B7

	SW_SHOW         = 5
	COLOR_BTNFACE   = 15
)

// Control IDs
const (
	ID_BUGHOST_EDIT = 101
	ID_BUGPORT_EDIT = 102
	ID_SSHHOST_EDIT = 103
	ID_SSHPORT_EDIT = 104
	ID_USER_EDIT    = 105
	ID_PASS_EDIT    = 106
	ID_PAYLOAD_EDIT = 107
	ID_SOCKS_EDIT   = 108
	ID_SYSPROXY_CHK = 109
	ID_AUTOREC_CHK  = 110
	ID_USETLS_CHK   = 111
	ID_VPNMODE_CHK  = 112
	ID_NODELAY_CHK  = 113

	ID_CONNECT_BTN  = 201
	ID_SAVECFG_BTN  = 202
	ID_CLEARLOG_BTN = 203
	ID_TAG_CRLF     = 204
	ID_TAG_HOST     = 205
	ID_TAG_PORT     = 206
	ID_TAG_UA       = 207
	ID_TAG_NETMOD   = 208
	ID_TAG_SPLIT    = 209

	ID_STATUS_TXT   = 301
	ID_SPEED_TXT    = 302
	ID_LOG_EDIT     = 303
)

type WNDCLASSEXW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type POINT struct {
	x, y int32
}

type MSG struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      POINT
}

type NativeGUI struct {
	hwndMain  uintptr
	hFont     uintptr
	hFontBold uintptr

	manager *tunnel.TunnelManager
	store   *tunnel.ConfigStore

	// Control Handles
	hBugHost   uintptr
	hBugPort   uintptr
	hSSHHost   uintptr
	hSSHPort   uintptr
	hUser      uintptr
	hPass      uintptr
	hPayload   uintptr
	hSocksPort uintptr
	hSysProxy  uintptr
	hAutoRec   uintptr
	hUseTLS    uintptr
	hNoDelay   uintptr
	hVPNMode   uintptr

	hConnectBtn uintptr
	hStatusTxt  uintptr
	hSpeedTxt   uintptr
	hLogEdit    uintptr

	lastLogIndex int
}

var guiInstance *NativeGUI

func runNativeGUI(manager *tunnel.TunnelManager, store *tunnel.ConfigStore) {
	// Lock OS thread to prevent Go runtime thread-hops from breaking the Windows message pump
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	gui := &NativeGUI{
		manager: manager,
		store:   store,
	}
	guiInstance = gui

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className := syscall.StringToUTF16Ptr("GoTunnelNativeWinClass")

	wndproc := syscall.NewCallback(guiWndProc)

	wc := WNDCLASSEXW{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		style:         0,
		lpfnWndProc:   wndproc,
		hInstance:     hInstance,
		hbrBackground: uintptr(COLOR_BTNFACE + 1),
		lpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// Create clean Segoe UI fonts
	fontName := syscall.StringToUTF16Ptr("Segoe UI")
	hFont, _, _ := procCreateFontW.Call(
		15, 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)),
	)
	hFontBold, _, _ := procCreateFontW.Call(
		16, 0, 0, 0, 700, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)),
	)
	gui.hFont = hFont
	gui.hFontBold = hFontBold

	var titleStr string
	if tunnel.IsAdmin() {
		titleStr = "GoTunnel — Native Windows SSH+WS Engine [Admin Privileges]"
	} else {
		titleStr = "GoTunnel — Native Windows SSH+WS Engine [User Mode — Run as Admin for TAP VPN]"
	}
	windowTitle := syscall.StringToUTF16Ptr(titleStr)

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowTitle)),
		WS_OVERLAPPEDWINDOW|WS_VISIBLE,
		100, 100, 760, 710,
		0, 0, hInstance, 0,
	)

	gui.hwndMain = hwnd
	gui.createControls(hInstance)
	gui.loadConfigToUI()

	procShowWindow.Call(hwnd, SW_SHOW)
	procUpdateWindow.Call(hwnd)

	// Set 500ms telemetry update timer
	procSetTimer.Call(hwnd, 1, 500, 0)

	var msg MSG
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}

		// Handle keyboard shortcuts (Ctrl+A, Ctrl+C, Ctrl+V, Ctrl+X, Ctrl+Z) for Edit controls
		if msg.message == WM_KEYDOWN {
			ctrlState, _, _ := procGetKeyState.Call(VK_CONTROL)
			if int16(ctrlState) < 0 {
				hFocus, _, _ := procGetFocus.Call()
				if hFocus != 0 {
					switch msg.wParam {
					case 'A', 'a': // Select All
						procSendMessageW.Call(hFocus, EM_SETSEL, 0, ^uintptr(0))
						continue
					case 'C', 'c': // Copy
						procSendMessageW.Call(hFocus, WM_COPY, 0, 0)
						continue
					case 'V', 'v': // Paste
						procSendMessageW.Call(hFocus, WM_PASTE, 0, 0)
						continue
					case 'X', 'x': // Cut
						procSendMessageW.Call(hFocus, WM_CUT, 0, 0)
						continue
					case 'Z', 'z': // Undo
						procSendMessageW.Call(hFocus, WM_UNDO, 0, 0)
						continue
					}
				}
			}
		}

		// Handle Tab navigation between dialog controls (only for keyboard messages)
		if msg.message >= 0x0100 && msg.message <= 0x0109 {
			hFocus, _, _ := procGetFocus.Call()
			if hFocus != gui.hPayload {
				isDlg, _, _ := procIsDialogMessageW.Call(hwnd, uintptr(unsafe.Pointer(&msg)))
				if isDlg != 0 {
					continue
				}
			}
		}

		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (g *NativeGUI) createControls(hInst uintptr) {
	mkCtrl := func(className, text string, style uintptr, x, y, w, h int, id int) uintptr {
		cName := syscall.StringToUTF16Ptr(className)
		cText := syscall.StringToUTF16Ptr(text)
		hCtrl, _, _ := procCreateWindowExW.Call(
			0,
			uintptr(unsafe.Pointer(cName)),
			uintptr(unsafe.Pointer(cText)),
			WS_CHILD|WS_VISIBLE|style,
			uintptr(x), uintptr(y), uintptr(w), uintptr(h),
			g.hwndMain, uintptr(id), hInst, 0,
		)
		procSendMessageW.Call(hCtrl, WM_SETFONT, g.hFont, 1)
		return hCtrl
	}

	// 1. Bug Host & SSH Target
	mkCtrl("STATIC", "Bug Host (Address):", 0, 20, 18, 125, 20, 0)
	g.hBugHost = mkCtrl("EDIT", "", WS_BORDER|WS_TABSTOP|ES_AUTOHSCROLL, 150, 15, 185, 24, ID_BUGHOST_EDIT)

	mkCtrl("STATIC", "Port:", 0, 345, 18, 35, 20, 0)
	g.hBugPort = mkCtrl("EDIT", "80", WS_BORDER|WS_TABSTOP|ES_AUTOHSCROLL, 385, 15, 50, 24, ID_BUGPORT_EDIT)

	g.hUseTLS = mkCtrl("BUTTON", "TLS", BS_AUTOCHECKBOX|WS_TABSTOP, 445, 15, 50, 24, ID_USETLS_CHK)
	g.hNoDelay = mkCtrl("BUTTON", "Disable TCP Delay", BS_AUTOCHECKBOX|WS_TABSTOP, 505, 15, 160, 24, ID_NODELAY_CHK)

	mkCtrl("STATIC", "SSH Server Host:", 0, 20, 48, 130, 20, 0)
	g.hSSHHost = mkCtrl("EDIT", "", WS_BORDER|WS_TABSTOP|ES_AUTOHSCROLL, 155, 45, 230, 24, ID_SSHHOST_EDIT)

	mkCtrl("STATIC", "Port:", 0, 395, 48, 40, 20, 0)
	g.hSSHPort = mkCtrl("EDIT", "80", WS_BORDER|WS_TABSTOP|ES_AUTOHSCROLL, 435, 45, 60, 24, ID_SSHPORT_EDIT)

	mkCtrl("STATIC", "Username:", 0, 20, 78, 130, 20, 0)
	g.hUser = mkCtrl("EDIT", "", WS_BORDER|WS_TABSTOP|ES_AUTOHSCROLL, 155, 75, 230, 24, ID_USER_EDIT)

	mkCtrl("STATIC", "Password:", 0, 395, 78, 65, 20, 0)
	g.hPass = mkCtrl("EDIT", "", WS_BORDER|WS_TABSTOP|ES_PASSWORD|ES_AUTOHSCROLL, 465, 75, 170, 24, ID_PASS_EDIT)

	// 2. HTTP Request Payload
	mkCtrl("STATIC", "HTTP WebSocket Payload (Format tags supported):", 0, 20, 110, 600, 20, 0)
	g.hPayload = mkCtrl("EDIT", "", WS_BORDER|WS_TABSTOP|WS_VSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN, 20, 132, 705, 72, ID_PAYLOAD_EDIT)

	// Quick Insert Buttons
	mkCtrl("BUTTON", "[crlf]", BS_PUSHBUTTON|WS_TABSTOP, 20, 210, 55, 24, ID_TAG_CRLF)
	mkCtrl("BUTTON", "[host]", BS_PUSHBUTTON|WS_TABSTOP, 80, 210, 55, 24, ID_TAG_HOST)
	mkCtrl("BUTTON", "[port]", BS_PUSHBUTTON|WS_TABSTOP, 140, 210, 55, 24, ID_TAG_PORT)
	mkCtrl("BUTTON", "[ua]", BS_PUSHBUTTON|WS_TABSTOP, 200, 210, 55, 24, ID_TAG_UA)
	mkCtrl("BUTTON", "NetMod Standard WS", BS_PUSHBUTTON|WS_TABSTOP, 265, 210, 155, 24, ID_TAG_NETMOD)
	mkCtrl("BUTTON", "Anti-DPI Split", BS_PUSHBUTTON|WS_TABSTOP, 430, 210, 110, 24, ID_TAG_SPLIT)

	// 3. Routing & Settings
	mkCtrl("STATIC", "Local SOCKS5 Port:", 0, 20, 248, 125, 20, 0)
	g.hSocksPort = mkCtrl("EDIT", "10808", WS_BORDER|WS_TABSTOP|ES_AUTOHSCROLL, 150, 245, 65, 24, ID_SOCKS_EDIT)

	g.hVPNMode = mkCtrl("BUTTON", "VPN Mode (TAP / Tun2Socks)", BS_AUTOCHECKBOX|WS_TABSTOP, 225, 245, 195, 24, ID_VPNMODE_CHK)
	g.hSysProxy = mkCtrl("BUTTON", "System Proxy", BS_AUTOCHECKBOX|WS_TABSTOP, 430, 245, 110, 24, ID_SYSPROXY_CHK)
	g.hAutoRec = mkCtrl("BUTTON", "Auto Reconnect", BS_AUTOCHECKBOX|WS_TABSTOP, 550, 245, 130, 24, ID_AUTOREC_CHK)

	// 4. Action Buttons
	g.hConnectBtn = mkCtrl("BUTTON", "CONNECT", BS_DEFPUSHBUTTON|WS_TABSTOP, 20, 282, 210, 38, ID_CONNECT_BTN)
	procSendMessageW.Call(g.hConnectBtn, WM_SETFONT, g.hFontBold, 1)

	mkCtrl("BUTTON", "Save Configuration", BS_PUSHBUTTON|WS_TABSTOP, 240, 282, 140, 38, ID_SAVECFG_BTN)
	mkCtrl("BUTTON", "Clear Log", BS_PUSHBUTTON|WS_TABSTOP, 390, 282, 90, 38, ID_CLEARLOG_BTN)

	// 5. Telemetry / Status Labels
	g.hStatusTxt = mkCtrl("STATIC", "Status: DISCONNECTED | Click CONNECT to begin", 0, 20, 330, 705, 20, ID_STATUS_TXT)
	procSendMessageW.Call(g.hStatusTxt, WM_SETFONT, g.hFontBold, 1)

	g.hSpeedTxt = mkCtrl("STATIC", "Download: 0.0 KB/s | Upload: 0.0 KB/s | Ping: -- ms | Total: 0.0 MB", 0, 20, 352, 705, 20, ID_SPEED_TXT)

	// 6. Live Log Terminal
	mkCtrl("STATIC", "Live Connection Console:", 0, 20, 380, 200, 18, 0)
	g.hLogEdit = mkCtrl("EDIT", "", WS_BORDER|WS_VSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY, 20, 400, 705, 255, ID_LOG_EDIT)
}

func (g *NativeGUI) loadConfigToUI() {
	cfg := g.store.Get()
	setCtrlText(g.hBugHost, cfg.BugHost)
	setCtrlText(g.hBugPort, strconv.Itoa(cfg.BugPort))
	setCtrlText(g.hSSHHost, cfg.SSHHost)
	setCtrlText(g.hSSHPort, strconv.Itoa(cfg.SSHPort))
	setCtrlText(g.hUser, cfg.Username)
	setCtrlText(g.hPass, cfg.Password)
	setCtrlText(g.hPayload, cfg.Payload)
	setCtrlText(g.hSocksPort, strconv.Itoa(cfg.SocksPort))

	if cfg.BugPort == 80 {
		setCheck(g.hUseTLS, false)
	} else {
		setCheck(g.hUseTLS, cfg.UseTLS)
	}
	setCheck(g.hVPNMode, cfg.UseVPN)
	setCheck(g.hSysProxy, cfg.AutoSetSystemProxy)
	setCheck(g.hAutoRec, cfg.AutoReconnect)
	setCheck(g.hNoDelay, cfg.DisableTCPDelay)
}

func (g *NativeGUI) saveConfigFromUI() tunnel.Config {
	cfg := g.store.Get()
	cfg.BugHost = strings.TrimSpace(getCtrlText(g.hBugHost))
	cfg.BugPort, _ = strconv.Atoi(strings.TrimSpace(getCtrlText(g.hBugPort)))
	if cfg.BugPort <= 0 {
		cfg.BugPort = 80
	}

	cfg.SSHHost = strings.TrimSpace(getCtrlText(g.hSSHHost))
	cfg.SSHPort, _ = strconv.Atoi(strings.TrimSpace(getCtrlText(g.hSSHPort)))
	if cfg.SSHPort <= 0 {
		cfg.SSHPort = 80
	}

	cfg.Username = strings.TrimSpace(getCtrlText(g.hUser))
	cfg.Password = getCtrlText(g.hPass)
	cfg.Payload = strings.TrimSpace(getCtrlText(g.hPayload))

	cfg.SocksPort, _ = strconv.Atoi(strings.TrimSpace(getCtrlText(g.hSocksPort)))
	if cfg.SocksPort <= 0 {
		cfg.SocksPort = 10808
	}

	cfg.UseTLS = getCheck(g.hUseTLS)
	cfg.UseVPN = getCheck(g.hVPNMode)
	cfg.AutoSetSystemProxy = getCheck(g.hSysProxy)
	cfg.AutoReconnect = getCheck(g.hAutoRec)
	cfg.DisableTCPDelay = getCheck(g.hNoDelay)

	_ = g.store.Update(cfg)
	return cfg
}

func (g *NativeGUI) onConnectToggle() {
	tel := g.manager.GetTelemetry()
	if tel.State == tunnel.StateConnected || tel.State == tunnel.StateConnecting || tel.State == tunnel.StateInjecting || tel.State == tunnel.StateSSHAuth {
		// Stop
		g.manager.Log("Stopping tunnel...")
		setCtrlText(g.hConnectBtn, "CONNECT")
		go func() {
			_ = g.manager.Stop()
		}()
	} else {
		// Save then Start
		cfg := g.saveConfigFromUI()
		if cfg.BugHost == "" || cfg.SSHHost == "" {
			g.manager.Log("ERROR: Please specify Bug Host and SSH Server!")
			return
		}

		g.manager.Log("Starting tunnel connection...")
		setCtrlText(g.hConnectBtn, "DISCONNECT")
		go func() {
			if err := g.manager.Start(); err != nil {
				g.manager.Log(fmt.Sprintf("Failed to start: %v", err))
				setCtrlText(g.hConnectBtn, "CONNECT")
			}
		}()
	}
}

func (g *NativeGUI) onTimer() {
	tel := g.manager.GetTelemetry()

	// Update status text
	var statusMsg string
	switch tel.State {
	case tunnel.StateConnected:
		statusMsg = fmt.Sprintf("Status: [● CONNECTED]  via %s", tel.StateMessage)
		setCtrlText(g.hConnectBtn, "DISCONNECT")
	case tunnel.StateConnecting, tunnel.StateInjecting, tunnel.StateSSHAuth:
		statusMsg = fmt.Sprintf("Status: [◐ %s]  %s", tel.State, tel.StateMessage)
		setCtrlText(g.hConnectBtn, "DISCONNECT")
	case tunnel.StateError:
		statusMsg = fmt.Sprintf("Status: [✕ ERROR]  %s", tel.StateMessage)
		setCtrlText(g.hConnectBtn, "CONNECT")
	default:
		statusMsg = "Status: [○ DISCONNECTED]  Ready. Click CONNECT to begin"
		setCtrlText(g.hConnectBtn, "CONNECT")
	}
	setCtrlText(g.hStatusTxt, statusMsg)

	// Format speeds & data
	downStr := formatSpeed(tel.DownloadSpeed)
	upStr := formatSpeed(tel.UploadSpeed)
	pingStr := "-- ms"
	if tel.PingMs > 0 {
		pingStr = fmt.Sprintf("%d ms", tel.PingMs)
	}
	totalStr := formatBytes(tel.TotalBytesIn + tel.TotalBytesOut)

	vpnStatus := "OFF"
	if tel.VPNActive {
		vpnStatus = "ACTIVE (TAP)"
	}

	speedMsg := fmt.Sprintf("DL: %s | UL: %s | Latency: %s | Total: %s | Conns: %d | VPN: %s",
		downStr, upStr, pingStr, totalStr, tel.ActiveConns, vpnStatus,
	)
	setCtrlText(g.hSpeedTxt, speedMsg)

	// Append new logs to edit box
	if len(tel.Logs) > g.lastLogIndex {
		newLines := strings.Join(tel.Logs[g.lastLogIndex:], "\r\n") + "\r\n"
		appendLogText(g.hLogEdit, newLines)
		g.lastLogIndex = len(tel.Logs)
	}
}

func (g *NativeGUI) insertPayloadTag(tag string) {
	cur := getCtrlText(g.hPayload)
	setCtrlText(g.hPayload, cur+tag)
}

func guiWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	gui := guiInstance
	switch uint32(msg) {
	case WM_COMMAND:
		ctrlID := int(wParam & 0xFFFF)
		switch ctrlID {
		case ID_CONNECT_BTN:
			if gui != nil {
				gui.onConnectToggle()
			}
		case ID_SAVECFG_BTN:
			if gui != nil {
				gui.saveConfigFromUI()
				gui.manager.Log("Settings saved to config.")
			}
		case ID_CLEARLOG_BTN:
			if gui != nil {
				setCtrlText(gui.hLogEdit, "")
				gui.lastLogIndex = len(gui.manager.GetTelemetry().Logs)
			}
		case ID_TAG_CRLF:
			if gui != nil {
				gui.insertPayloadTag("[crlf]")
			}
		case ID_TAG_HOST:
			if gui != nil {
				gui.insertPayloadTag("[host]")
			}
		case ID_TAG_PORT:
			if gui != nil {
				gui.insertPayloadTag("[port]")
			}
		case ID_TAG_UA:
			if gui != nil {
				gui.insertPayloadTag("[ua]")
			}
		case ID_TAG_NETMOD:
			if gui != nil {
				setCtrlText(gui.hPayload, "GET / HTTP/1.1[crlf]Host: [host][crlf]Upgrade: websocket[crlf]Connection: Upgrade[crlf][crlf]")
			}
		case ID_TAG_SPLIT:
			if gui != nil {
				setCtrlText(gui.hPayload, "GET / HTTP/1.1[crlf]Host: [host][crlf][split]Upgrade: websocket[crlf]Connection: Upgrade[crlf][crlf]")
			}
		}
		return 0

	case WM_TIMER:
		if gui != nil {
			gui.onTimer()
		}
		return 0

	case WM_DESTROY:
		if gui != nil {
			procKillTimer.Call(hwnd, 1)
			_ = gui.manager.Stop()
		}
		procPostQuitMessage.Call(0)
		return 0
	}

	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// Helpers for Win32 Edit / Checkbox controls
func setCtrlText(hCtrl uintptr, text string) {
	uText := syscall.StringToUTF16Ptr(text)
	procSendMessageW.Call(hCtrl, WM_SETTEXT, 0, uintptr(unsafe.Pointer(uText)))
}

func getCtrlText(hCtrl uintptr) string {
	lenVal, _, _ := procSendMessageW.Call(hCtrl, WM_GETTEXTLENGTH, 0, 0)
	if lenVal == 0 {
		return ""
	}
	buf := make([]uint16, lenVal+1)
	procSendMessageW.Call(hCtrl, WM_GETTEXT, uintptr(lenVal+1), uintptr(unsafe.Pointer(&buf[0])))
	return syscall.UTF16ToString(buf)
}

func setCheck(hCtrl uintptr, checked bool) {
	val := uintptr(BST_UNCHECKED)
	if checked {
		val = uintptr(BST_CHECKED)
	}
	procSendMessageW.Call(hCtrl, BM_SETCHECK, val, 0)
}

func getCheck(hCtrl uintptr) bool {
	res, _, _ := procSendMessageW.Call(hCtrl, BM_GETCHECK, 0, 0)
	return res == uintptr(BST_CHECKED)
}

func appendLogText(hCtrl uintptr, text string) {
	lenVal, _, _ := procSendMessageW.Call(hCtrl, WM_GETTEXTLENGTH, 0, 0)
	procSendMessageW.Call(hCtrl, EM_SETSEL, lenVal, lenVal)
	uText := syscall.StringToUTF16Ptr(text)
	procSendMessageW.Call(hCtrl, EM_REPLACESEL, 0, uintptr(unsafe.Pointer(uText)))
	procSendMessageW.Call(hCtrl, EM_SCROLLCARET, 0, 0)
}

func formatSpeed(bytesPerSec uint64) string {
	if bytesPerSec >= 1024*1024 {
		return fmt.Sprintf("%.2f MB/s", float64(bytesPerSec)/(1024*1024))
	}
	return fmt.Sprintf("%.1f KB/s", float64(bytesPerSec)/1024)
}

func formatBytes(bytes uint64) string {
	if bytes >= 1024*1024*1024 {
		return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}
