//go:build windows

package tunnel

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// IsAdmin returns true if the current process has elevated Administrator privileges.
func IsAdmin() bool {
	token := windows.GetCurrentProcessToken()
	return token.IsElevated()
}

// VPNController manages the TAP-Windows adapter and badvpn-tun2socks process.
type VPNController struct {
	mu          sync.Mutex
	active      atomic.Bool
	starting    atomic.Bool
	cmd         *exec.Cmd
	tapName     string
	bugHostIP   string
	origGateway string
	tapIP       string
	tapGateway  string
	tapSubnet   string
}

func NewVPNController() *VPNController {
	return &VPNController{
		tapName:    "Local Area Connection", // Standard TAP-Windows Adapter V9 name
		tapIP:      "10.4.2.2",
		tapGateway: "10.4.2.1",
		tapSubnet:  "255.255.255.0",
	}
}

// Start activates the TAP adapter, launches badvpn-tun2socks, and sets system routing table.
// It is non-blocking to other subsystems and NEVER holds locks across external commands or logging.
func (vc *VPNController) Start(socksPort int, bugHost string, logFn func(string)) error {
	if vc.active.Load() {
		return nil
	}
	if !vc.starting.CompareAndSwap(false, true) {
		return fmt.Errorf("VPN is already starting")
	}
	defer vc.starting.Store(false)

	// Check Administrator privileges
	if !IsAdmin() {
		logFn("[!] ERROR: Administrator privileges required for TAP VPN Mode.")
		logFn("[!] Please right-click wstunnel.exe and select 'Run as administrator',")
		logFn("[!] or uncheck 'VPN Mode' to use SOCKS5 proxy without admin rights.")
		return fmt.Errorf("administrator privileges required for VPN mode")
	}

	// 1. Locate badvpn-tun2socks.exe
	binPath := vc.findTun2Socks()
	if binPath == "" {
		return fmt.Errorf("badvpn-tun2socks.exe not found in bin directory")
	}

	// 2. Resolve Bug Host IPv4
	logFn(fmt.Sprintf("Resolving Bug Host IPv4 for %s...", bugHost))
	ips, err := net.LookupIP(bugHost)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("failed to resolve bug host %s: %v", bugHost, err)
	}
	var targetIP string
	for _, ip := range ips {
		if ip.To4() != nil {
			targetIP = ip.String()
			break
		}
	}
	if targetIP == "" {
		targetIP = ips[0].String()
	}

	// 3. Find physical default gateway
	gateway, err := vc.getPhysicalGateway()
	if err != nil {
		logFn(fmt.Sprintf("Warning: could not detect physical gateway: %v", err))
	} else {
		logFn(fmt.Sprintf("Physical Gateway detected: %s", gateway))
	}

	vc.mu.Lock()
	vc.bugHostIP = targetIP
	vc.origGateway = gateway
	vc.mu.Unlock()

	// 4. Configure TAP adapter IP & DNS
	logFn(fmt.Sprintf("Configuring TAP adapter '%s' (IP: %s, GW: %s)...", vc.tapName, vc.tapIP, vc.tapGateway))
	_ = runWinCmd("netsh", "interface", "ip", "set", "address",
		fmt.Sprintf("name=%s", vc.tapName),
		"static", vc.tapIP, vc.tapSubnet, vc.tapGateway)

	_ = runWinCmd("netsh", "interface", "ip", "set", "dns",
		fmt.Sprintf("name=%s", vc.tapName),
		"static", "1.1.1.1")

	// 5. Add bypass route for Bug Host IP through physical gateway
	if gateway != "" && targetIP != "" {
		logFn(fmt.Sprintf("Adding route bypass for Bug Host %s -> %s", targetIP, gateway))
		_ = runWinCmd("route", "add", targetIP, "mask", "255.255.255.255", gateway, "metric", "1")
	}

	// 6. Terminate conflicting badvpn-tun2socks instance if any
	_ = runWinCmd("taskkill", "/F", "/IM", "badvpn-tun2socks.exe")
	time.Sleep(150 * time.Millisecond)

	// 7. Start badvpn-tun2socks.exe
	tunDevSpec := fmt.Sprintf("tap0901:%s:%s:10.4.2.0:%s", vc.tapName, vc.tapIP, vc.tapSubnet)
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)

	logFn(fmt.Sprintf("Launching badvpn-tun2socks (%s -> %s)...", vc.tapName, socksAddr))
	cmd := exec.Command(binPath,
		"--tundev", tunDevSpec,
		"--netif-ipaddr", vc.tapIP,
		"--netif-netmask", vc.tapSubnet,
		"--socks-server-addr", socksAddr,
		"--loglevel", "warning",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}

	// Stream stderr to log if any issues occur
	stderrPipe, err := cmd.StderrPipe()
	if err == nil {
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				text := strings.TrimSpace(scanner.Text())
				if text != "" {
					logFn(fmt.Sprintf("[Tun2Socks] %s", text))
				}
			}
		}()
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start badvpn-tun2socks: %w", err)
	}

	vc.mu.Lock()
	vc.cmd = cmd
	vc.mu.Unlock()

	// Monitor child process exit
	go func() {
		_ = cmd.Wait()
		vc.active.Store(false)
		vc.mu.Lock()
		vc.cmd = nil
		vc.mu.Unlock()
	}()

	time.Sleep(300 * time.Millisecond)

	// 8. Route all system traffic into TAP adapter gateway (metric 1)
	logFn(fmt.Sprintf("Routing system traffic through VPN gateway %s...", vc.tapGateway))
	_ = runWinCmd("route", "add", "0.0.0.0", "mask", "128.0.0.0", vc.tapGateway, "metric", "1")
	_ = runWinCmd("route", "add", "128.0.0.0", "mask", "128.0.0.0", vc.tapGateway, "metric", "1")

	vc.active.Store(true)
	logFn("✓ System-Wide VPN Active! All PC traffic (browsers, apps, games) routed via TUN.")
	return nil
}

// Stop restores routes and terminates tun2socks.
func (vc *VPNController) Stop(logFn func(string)) {
	if !vc.active.Swap(false) && vc.cmd == nil {
		return
	}

	vc.mu.Lock()
	cmd := vc.cmd
	vc.cmd = nil
	bugHost := vc.bugHostIP
	vc.mu.Unlock()

	logFn("Restoring Windows network routing table...")
	_ = runWinCmd("route", "delete", "0.0.0.0", "mask", "128.0.0.0", vc.tapGateway)
	_ = runWinCmd("route", "delete", "128.0.0.0", "mask", "128.0.0.0", vc.tapGateway)

	if bugHost != "" {
		_ = runWinCmd("route", "delete", bugHost)
	}

	if cmd != nil && cmd.Process != nil {
		logFn("Stopping badvpn-tun2socks process...")
		_ = cmd.Process.Kill()
	}

	logFn("✓ VPN deactivated. Standard network routing restored.")
}

// IsActive returns whether VPN mode is currently running (lock-free).
func (vc *VPNController) IsActive() bool {
	return vc.active.Load()
}

func (vc *VPNController) findTun2Socks() string {
	candidates := []string{
		filepath.Join("bin", "badvpn-tun2socks.exe"),
		filepath.Join(".", "badvpn-tun2socks.exe"),
		`C:\Program Files (x86)\NetMod\bin\badvpn-tun2socks.exe`,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, err := filepath.Abs(p)
			if err == nil {
				return abs
			}
			return p
		}
	}
	return ""
}

func (vc *VPNController) getPhysicalGateway() (string, error) {
	cmd := exec.Command("route", "print", "0.0.0.0")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	out, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 5 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
				gw := fields[2]
				if !strings.HasPrefix(gw, "10.4.") && gw != "0.0.0.0" && !strings.HasPrefix(gw, "127.") {
					return gw, nil
				}
			}
		}
	}

	// Fallback to PowerShell if needed
	psCmd := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Where-Object { $_.NextHop -notlike '10.4.*' -and $_.NextHop -ne '0.0.0.0' } | Sort-Object RouteMetric | Select-Object -First 1).NextHop`)
	psCmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	psOut, psErr := psCmd.Output()
	if psErr == nil {
		gw := strings.TrimSpace(string(psOut))
		if gw != "" {
			return gw, nil
		}
	}
	return "", fmt.Errorf("no physical gateway found")
}

func runWinCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd.Run()
}
