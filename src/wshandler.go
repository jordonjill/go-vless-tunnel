package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

const (
	connectionTimeout = 30 * time.Second // initial-message deadline
	tcpDialTimeout    = 5 * time.Second
	// The first message (VLESS header + first payload chunk) is buffered in
	// memory before the UUID is checked, so cap it. Mainstream clients send
	// at most ~32 KiB here.
	maxFirstMessageSize = 64 << 10
)

var successResponse = []byte{0x00, 0x00}

// setupWebSocketHandler returns an http.HandlerFunc that upgrades HTTP
// connections to WebSocket and handles the VLESS protocol.
// uuidHex is the server UUID with hyphens removed (32 hex chars).
func setupWebSocketHandler(uuidHex string) http.HandlerFunc {
	uuidBytes, err := hex.DecodeString(uuidHex)
	if err != nil {
		log.Fatalf("[FATAL] Invalid UUID hex: %v", err)
	}

	// The zero value already rejects cross-origin browser requests and reuses
	// net/http's 4 KiB buffers.
	upgrader := websocket.Upgrader{}

	return func(w http.ResponseWriter, r *http.Request) {
		conn, upgradeErr := upgrader.Upgrade(w, r, nil)
		if upgradeErr != nil {
			log.Printf("[ERROR] WebSocket upgrade failed: %v", upgradeErr)
			return
		}
		go handleConnection(conn, uuidBytes)
	}
}

// handleConnection processes a single VLESS-over-WebSocket client connection.
func handleConnection(ws *websocket.Conn, uuidBytes []byte) {
	defer ws.Close()

	// Enforce a deadline and a size limit for the first (header) message.
	ws.SetReadLimit(maxFirstMessageSize)
	_ = ws.SetReadDeadline(time.Now().Add(connectionTimeout))

	msgType, msg, err := ws.ReadMessage()
	if err != nil {
		log.Printf("[WARN] Connection timeout or read error: %v", err)
		return
	}

	// Lift both for subsequent data messages, which are streamed.
	ws.SetReadLimit(0)
	_ = ws.SetReadDeadline(time.Time{})

	if msgType != websocket.BinaryMessage {
		log.Printf("[WARN] Expected binary message, got type: %d", msgType)
		return
	}

	// Minimum bytes before address payload is parsed by parseHost.
	if len(msg) < 22 {
		log.Printf("[WARN] Message too short: %d bytes", len(msg))
		return
	}

	// ── VLESS header layout ──────────────────────────────────────
	// [0]        version  (must be 0)
	// [1..16]    UUID     (16 bytes)
	// [17]       M        additional-data length
	// [18..17+M] additional data (ignored)
	// [18+M]     command  (1=TCP — only TCP is handled here)
	// [19+M..20+M] port   (uint16, big-endian)
	// [21+M]     addrType + address bytes
	// ─────────────────────────────────────────────────────────────

	if msg[0] != 0x00 {
		log.Printf("[WARN] Unsupported VLESS version: %d", msg[0])
		return
	}

	// Validate UUID (bytes 1-16).
	for i, b := range uuidBytes {
		if msg[1+i] != b {
			log.Printf("[WARN] Invalid UUID connection")
			return
		}
	}

	addonLen := int(msg[17])
	commandOffset := 18 + addonLen
	if len(msg) < commandOffset+1 {
		log.Printf("[ERROR] Message too short to parse command")
		return
	}

	if msg[commandOffset] != 0x01 {
		log.Printf("[WARN] Unsupported VLESS command: %d", msg[commandOffset])
		return
	}

	portOffset := commandOffset + 1

	if len(msg) < portOffset+3 { // need at least port(2) + addrType(1)
		log.Printf("[ERROR] Message too short to parse port and address")
		return
	}

	port := binary.BigEndian.Uint16(msg[portOffset : portOffset+2])
	addrOffset := portOffset + 2

	host, endIndex, parseErr := parseHost(msg, addrOffset)
	if parseErr != nil {
		log.Printf("[ERROR] Failed to parse target host: %v", parseErr)
		return
	}

	payload := msg[endIndex:]

	target := net.JoinHostPort(host, strconv.Itoa(int(port)))
	if relayErr := relay(ws, target, payload); relayErr != nil {
		log.Printf("[ERROR] Proxy to %s failed: %v", target, relayErr)
	}
}

// relay dials the TCP target and then bidirectionally proxies data between
// the WebSocket and the TCP connection.
func relay(ws *websocket.Conn, target string, payload []byte) error {
	tcpConn, err := net.DialTimeout("tcp", target, tcpDialTimeout)
	if err != nil {
		return err
	}
	defer tcpConn.Close()

	// Forward the initial payload that arrived with the VLESS header.
	if len(payload) > 0 {
		if _, wErr := tcpConn.Write(payload); wErr != nil {
			return fmt.Errorf("send initial payload: %w", wErr)
		}
	}

	if writeErr := ws.WriteMessage(websocket.BinaryMessage, successResponse); writeErr != nil {
		return fmt.Errorf("send success response: %w", writeErr)
	}

	wsStream := &wsBinaryStream{ws: ws}

	// Whichever direction ends first closes both sides, which unblocks the other.
	// Closing twice is harmless.
	closeBoth := func() {
		_ = ws.Close()
		_ = tcpConn.Close()
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if _, copyErr := io.Copy(tcpConn, wsStream); !isExpectedProxyClose(copyErr) {
			log.Printf("[ERROR] WebSocket -> TCP transfer error: %v", copyErr)
		}
		closeBoth()
	}()

	go func() {
		defer wg.Done()
		if _, copyErr := io.Copy(wsStream, tcpConn); !isExpectedProxyClose(copyErr) {
			log.Printf("[ERROR] TCP -> WebSocket transfer error: %v", copyErr)
		}
		closeBoth()
	}()

	wg.Wait()
	return nil
}

type wsBinaryStream struct {
	ws     *websocket.Conn
	reader io.Reader
}

func (s *wsBinaryStream) Read(p []byte) (int, error) {
	for {
		if s.reader == nil {
			msgType, r, err := s.ws.NextReader()
			if err != nil {
				return 0, err
			}
			if msgType != websocket.BinaryMessage {
				continue
			}
			s.reader = r
		}

		n, err := s.reader.Read(p)
		if err == io.EOF {
			s.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (s *wsBinaryStream) Write(p []byte) (int, error) {
	if err := s.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// parseHost decodes the VLESS address field starting at startIndex.
// It returns the host string, the index of the first byte past the address,
// and any error.
func parseHost(msg []byte, startIndex int) (string, int, error) {
	if startIndex >= len(msg) {
		return "", startIndex, fmt.Errorf("message too short to read address type")
	}

	atyp := msg[startIndex]

	switch atyp {
	case 1: // IPv4 – 4 bytes
		end := startIndex + 5
		if len(msg) < end {
			return "", startIndex, fmt.Errorf("message too short to read IPv4 address")
		}
		ip := net.IP(msg[startIndex+1 : end])
		return ip.String(), end, nil

	case 2: // Domain – 1-byte length prefix
		if startIndex+2 > len(msg) {
			return "", startIndex, fmt.Errorf("message too short to read domain length")
		}
		domainLen := int(msg[startIndex+1])
		end := startIndex + 2 + domainLen
		if len(msg) < end {
			return "", startIndex, fmt.Errorf("message too short to read domain")
		}
		return string(msg[startIndex+2 : end]), end, nil

	case 3: // IPv6 – 16 bytes
		end := startIndex + 17
		if len(msg) < end {
			return "", startIndex, fmt.Errorf("message too short to read IPv6 address")
		}
		ip := net.IP(msg[startIndex+1 : end])
		return ip.String(), end, nil

	default:
		return "", startIndex, fmt.Errorf("invalid address type: %d", atyp)
	}
}

// isExpectedProxyClose reports whether a copy error just means one side of the
// session went away, which is routine for a proxy and not worth logging.
func isExpectedProxyClose(err error) bool {
	var closeErr *websocket.CloseError
	return err == nil ||
		errors.As(err, &closeErr) || // any WebSocket close, incl. 1006 when the client just drops
		errors.Is(err, websocket.ErrCloseSent) ||
		errors.Is(err, net.ErrClosed) || // we closed it after the other direction ended
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
}
