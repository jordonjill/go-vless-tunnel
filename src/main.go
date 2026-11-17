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
// their proxy software. wsPath must be the path the server actually serves.
func generateVlessURL(uuid, domain, wsPath string) string {
	return fmt.Sprintf(
		"vless://%s@%s:443?encryption=none&security=tls&sni=%s&fp=chrome&type=ws&host=%s&path=%s",
		uuid, domain, domain, domain, url.QueryEscape(wsPath),
	)
}

func buildWSPath(pathPrefix, name string) string {
	path := pathPrefix + name
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("[ERROR] Failed to start service: %v", err)
	}

	// UUID without hyphens is what the VLESS header embeds.
	uuidHex := strings.ReplaceAll(cfg.UUID, "-", "")
	wsHandler := setupWebSocketHandler(uuidHex)

	wsPath := buildWSPath(cfg.VlessPathPrefix, cfg.Name)
	vlessURL := generateVlessURL(cfg.UUID, cfg.TunnelDomain, wsPath)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if websocket.IsWebSocketUpgrade(r) {
			if r.URL.Path != wsPath {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintln(w, "Not Found")
				return
			}
			wsHandler(w, r)
			return
		}

		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintln(w, "Hello world!")

		case "/" + cfg.UUID:
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
