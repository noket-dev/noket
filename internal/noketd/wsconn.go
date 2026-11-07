package noketd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsReadDeadline  = 90 * time.Second
	wsWriteDeadline = 30 * time.Second
	wsReadLimit     = 33 << 20
)

// ErrUnauthorized means the tunnel rejected the login token during the
// WebSocket handshake. Noketd must not retry with the same credentials.
var ErrUnauthorized = errors.New("unauthorized")

type wsConn struct {
	*websocket.Conn

	buffer  []byte
	writeMu sync.Mutex
}

func (c *wsConn) Read(b []byte) (int, error) {
	if len(c.buffer) == 0 {
		_ = c.SetReadDeadline(time.Now().Add(wsReadDeadline))
		_, data, err := c.ReadMessage()
		if err != nil {
			return 0, err
		}
		c.buffer = data
	}
	n := copy(b, c.buffer)
	c.buffer = c.buffer[n:]
	return n, nil
}

func (c *wsConn) Write(b []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
	if err := c.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *wsConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

// dialWebSocket opens the tunnel connection with the login JWT in the
// Authorization header. Query-string tokens are deliberately not supported.
func dialWebSocket(ctx context.Context, rawURL, token string) (net.Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("tunnel.url is invalid: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, errors.New("tunnel.url must use ws:// or wss://")
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		// Compression is negotiated by the tunnel payloads themselves; the
		// inner HTTP stream must stay byte-for-byte.
		EnableCompression: false,
	}
	conn, resp, err := dialer.DialContext(ctx, rawURL, header)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				return nil, fmt.Errorf("%w: tunnel returned HTTP %d", ErrUnauthorized, resp.StatusCode)
			}
			return nil, fmt.Errorf("websocket handshake failed: HTTP %d", resp.StatusCode)
		}
		return nil, err
	}
	conn.SetReadLimit(wsReadLimit)

	return &wsConn{Conn: conn}, nil
}

func isUnauthorizedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnauthorized) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "unauthenticated") ||
		strings.Contains(text, "invalid jwt") ||
		strings.Contains(text, "login")
}
