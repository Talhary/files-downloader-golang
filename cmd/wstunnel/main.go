package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"download-engine/pkg/tunnel"
)

var version = "2.0.0"

func main() {
	socksPort := flag.Int("socks", 10808, "Port for local SOCKS5 proxy")
	configPath := flag.String("config", "", "Path to custom JSON configuration file")
	noGUI := flag.Bool("no-gui", false, "Run in headless CLI mode without opening native GUI window")
	autoConnect := flag.Bool("connect", false, "Automatically connect to tunnel on launch")
	showVersion := flag.Bool("v", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("GoTunnel Native Windows v%s (%s)\n", version, runtime.GOARCH)
		return
	}

	// 1. Initialize Config Store
	store, err := tunnel.NewConfigStore(*configPath)
	if err != nil {
		fmt.Printf("[!] Error initializing config: %v\n", err)
		os.Exit(1)
	}

	cfg := store.Get()
	if *socksPort > 0 && cfg.SocksPort != *socksPort {
		cfg.SocksPort = *socksPort
		_ = store.Update(cfg)
	}

	// 2. Initialize Tunnel Manager
	manager := tunnel.NewTunnelManager(store)

	// Auto-connect if requested
	if *autoConnect {
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = manager.Start()
		}()
	}

	// 3. Launch Native Windows Win32 GUI
	if !*noGUI && runtime.GOOS == "windows" {
		// Runs the pure Windows native API GUI (Zero HTML, Zero Chromium, Zero WebServer)
		runNativeGUI(manager, store)
		return
	}

	// 4. Headless CLI / Background Mode
	fmt.Println("================================================================================")
	fmt.Printf("  ⚡ GoTunnel — Native High-Speed SSH+WS Engine v%s (CLI Mode)\n", version)
	fmt.Println("================================================================================")
	fmt.Printf("  [✓] SOCKS5 Proxy Target:  127.0.0.1:%d\n", store.Get().SocksPort)
	fmt.Printf("  [✓] Press Ctrl+C to terminate\n\n")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	fmt.Println("\n[!] Shutting down GoTunnel...")
	_ = manager.Stop()
	fmt.Println("[✓] Clean exit completed.")
}
