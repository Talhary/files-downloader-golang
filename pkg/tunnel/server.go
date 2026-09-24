package tunnel

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

//go:embed ui/index.html
var uiFS embed.FS

type WebServer struct {
	manager  *TunnelManager
	store    *ConfigStore
	listener string
	server   *http.Server
	mu       sync.Mutex
}

func NewWebServer(manager *TunnelManager, store *ConfigStore, port int) *WebServer {
	ws := &WebServer{
		manager:  manager,
		store:    store,
		listener: fmt.Sprintf("127.0.0.1:%d", port),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", ws.handleIndex)
	mux.HandleFunc("/api/config", ws.handleConfig)
	mux.HandleFunc("/api/telemetry", ws.handleTelemetry)
	mux.HandleFunc("/api/connect", ws.handleConnect)
	mux.HandleFunc("/api/disconnect", ws.handleDisconnect)
	mux.HandleFunc("/api/toggle-sysproxy", ws.handleToggleSysProxy)

	ws.server = &http.Server{
		Addr:    ws.listener,
		Handler: mux,
	}

	return ws
}

func (ws *WebServer) Start() error {
	return ws.server.ListenAndServe()
}

func (ws *WebServer) Close() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.server != nil {
		return ws.server.Close()
	}
	return nil
}

func (ws *WebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := uiFS.ReadFile("ui/index.html")
	if err != nil {
		http.Error(w, "UI template missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (ws *WebServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		cfg := ws.store.Get()
		_ = json.NewEncoder(w).Encode(cfg)
		return
	}

	if r.Method == http.MethodPost {
		var cfg Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := ws.store.Update(cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (ws *WebServer) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tel := ws.manager.GetTelemetry()
	_ = json.NewEncoder(w).Encode(tel)
}

func (ws *WebServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := ws.manager.Start(); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (ws *WebServer) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = ws.manager.Stop()
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (ws *WebServer) handleToggleSysProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	active, err := ws.manager.ToggleSystemProxy()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "active": active})
}
