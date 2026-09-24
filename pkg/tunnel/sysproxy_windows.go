//go:build windows

package tunnel

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

var (
	wininet                 = syscall.NewLazyDLL("wininet.dll")
	procInternetSetOption   = wininet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

// SetWindowsSystemProxy enables the system-wide SOCKS proxy in Windows Internet Settings.
func SetWindowsSystemProxy(socksPort int) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()

	proxyVal := fmt.Sprintf("socks=127.0.0.1:%d", socksPort)
	if err := k.SetStringValue("ProxyServer", proxyVal); err != nil {
		return fmt.Errorf("failed to set ProxyServer: %w", err)
	}

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("failed to set ProxyEnable: %w", err)
	}

	// Bypass local addresses
	_ = k.SetStringValue("ProxyOverride", "<local>;localhost;127.*")

	refreshInternetOptions()
	return nil
}

// ClearWindowsSystemProxy disables the system-wide proxy in Windows.
func ClearWindowsSystemProxy() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()

	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		return fmt.Errorf("failed to disable ProxyEnable: %w", err)
	}

	refreshInternetOptions()
	return nil
}

func refreshInternetOptions() {
	_, _, _ = procInternetSetOption.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	_, _, _ = procInternetSetOption.Call(0, uintptr(internetOptionRefresh), 0, 0)
}
