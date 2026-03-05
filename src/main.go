package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

// generateVlessURL builds the VLESS connection URI that clients copy into
// their proxy software, matching the format produced by the JS implementation.
func generateVlessURL(uuid, domain, name, pathPrefix string) string {
	path := url.QueryEscape(pathPrefix + name)
	return fmt.Sprintf(
		"vless://%s@%s:443?encryption=none&security=tls&sni=%s&fp=chrome&type=ws&host=%s&path=%s",
		uuid, domain, domain, domain, path,
	)
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("[ERROR] Failed to start service: %v", err)
	}

	// UUID without hyphens is what the VLESS header embeds.
	uuidHex := strings.ReplaceAll(cfg.UUID, "-", "")
	wsHandler := setupWebSocketHandler(uuidHex)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Upgrade WebSocket connections regardless of path.
		if websocket.IsWebSocketUpgrade(r) {
			wsHandler(w, r)
			return
		}

		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintln(w, "Hello world!")

		case "/" + cfg.UUID:
			vlessURL := generateVlessURL(cfg.UUID, cfg.TunnelDomain, cfg.Name, cfg.VlessPathPrefix)
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintln(w, vlessURL)

		default:
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintln(w, "Not Found")
		}
	})

	log.Printf("[INFO] Service started - port: %s", cfg.Port)
	if listenErr := http.ListenAndServe(":"+cfg.Port, nil); listenErr != nil {
		log.Fatalf("[ERROR] Failed to listen: %v", listenErr)
	}
}
