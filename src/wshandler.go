package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	connectionTimeout = 30 * time.Second // initial-message deadline
	tcpDialTimeout    = 5 * time.Second
	keepAliveInterval = 10 * time.Second
	maxRetries        = 2
	retryDelay        = 1 * time.Second
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Accept connections from any origin (same behaviour as the JS ws library).
	CheckOrigin: func(r *http.Request) bool { return true },
}

var successResponse = []byte{0x00, 0x00}

// setupWebSocketHandler returns an http.HandlerFunc that upgrades HTTP
// connections to WebSocket and handles the VLESS protocol.
// uuidHex is the server UUID with hyphens removed (32 hex chars).
func setupWebSocketHandler(uuidHex string) http.HandlerFunc {
	uuidBytes, err := hex.DecodeString(uuidHex)
	if err != nil {
		log.Fatalf("[FATAL] Invalid UUID hex: %v", err)
	}

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

	// Enforce a deadline for the first (header) message.
	_ = ws.SetReadDeadline(time.Now().Add(connectionTimeout))

	msgType, msg, err := ws.ReadMessage()
	if err != nil {
		log.Printf("[WARN] Connection timeout or read error: %v", err)
		return
	}

	// Disable the deadline for subsequent data messages.
	_ = ws.SetReadDeadline(time.Time{})

	if msgType != websocket.BinaryMessage {
		log.Printf("[WARN] Expected binary message, got type: %d", msgType)
		return
	}

	// Minimum header length check: 1(ver)+16(uuid)+1(addonLen)+1(cmd)+2(port)+1(atyp)+4(ipv4) = 26
	if len(msg) < 26 {
		log.Printf("[WARN] Message too short: %d bytes", len(msg))
		return
	}

	// ── VLESS header layout ──────────────────────────────────────
	// [0]        version  (ignored, must be 0)
	// [1..16]    UUID     (16 bytes)
	// [17]       M        additional-data length
	// [18..17+M] additional data (ignored)
	// [18+M]     command  (1=TCP — only TCP is handled here)
	// [19+M..20+M] port   (uint16, big-endian)
	// [21+M]     addrType + address bytes
	// ─────────────────────────────────────────────────────────────

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

	// Send VLESS success response before proxying.
	if writeErr := ws.WriteMessage(websocket.BinaryMessage, successResponse); writeErr != nil {
		log.Printf("[ERROR] Failed to send success response: %v", writeErr)
		return
	}

	target := fmt.Sprintf("%s:%d", host, port)
	connectWithRetry(ws, target, payload)
}

// connectWithRetry dials the TCP target (retrying up to maxRetries times on
// failure) and then bidirectionally proxies data between the WebSocket and the
// TCP connection.
func connectWithRetry(ws *websocket.Conn, target string, payload []byte) {
	var tcpConn net.Conn
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[INFO] Retrying connection (%d/%d)", attempt, maxRetries)
			time.Sleep(retryDelay)
		}
		tcpConn, err = net.DialTimeout("tcp", target, tcpDialTimeout)
		if err == nil {
			break
		}
		log.Printf("[ERROR] TCP connection failed (attempt %d): %v", attempt+1, err)
	}

	if err != nil {
		log.Printf("[ERROR] Max retries reached, unable to connect: %s", target)
		return
	}
	defer tcpConn.Close()

	// Apply TCP tuning options.
	if tc, ok := tcpConn.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(keepAliveInterval)
		_ = tc.SetNoDelay(true)
	}

	// Forward the initial payload that arrived with the VLESS header.
	if len(payload) > 0 {
		if _, wErr := tcpConn.Write(payload); wErr != nil {
			log.Printf("[ERROR] Failed to send initial payload: %v", wErr)
			return
		}
	}

	wsStream := &wsBinaryStream{ws: ws}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = wsStream.Close()
			_ = tcpConn.Close()
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if _, copyErr := io.Copy(tcpConn, wsStream); copyErr != nil && !isNormalWSClose(copyErr) && !isExpectedProxyClose(copyErr) {
			log.Printf("[ERROR] WebSocket -> TCP transfer error: %v", copyErr)
		}
		cancel()
		closeBoth()
	}()

	go func() {
		defer wg.Done()
		if _, copyErr := io.Copy(wsStream, tcpConn); copyErr != nil && !isExpectedProxyClose(copyErr) {
			log.Printf("[ERROR] TCP -> WebSocket transfer error: %v", copyErr)
		}
		cancel()
		closeBoth()
	}()

	<-ctx.Done()
	closeBoth()
	wg.Wait()
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
	w, err := s.ws.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return 0, err
	}
	n, writeErr := w.Write(p)
	closeErr := w.Close()
	if writeErr != nil {
		return n, writeErr
	}
	if closeErr != nil {
		return n, closeErr
	}
	return n, nil
}

func (s *wsBinaryStream) Close() error {
	return s.ws.Close()
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

func isNormalWSClose(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
	) || strings.Contains(err.Error(), "use of closed network connection")
}

func isExpectedProxyClose(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	if closeErr := new(websocket.CloseError); errors.As(err, &closeErr) {
		return closeErr.Code == websocket.CloseNormalClosure || closeErr.Code == websocket.CloseGoingAway
	}
	return strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), "forcibly closed by the remote host") ||
		bytes.Contains([]byte(err.Error()), []byte("broken pipe"))
}
