package traefik_enforce_header_case_plugin

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

type writeCaptureConn struct {
	net.Conn
	w bytes.Buffer
}

func (c *writeCaptureConn) Write(p []byte) (int, error) { return c.w.Write(p) }
func (c *writeCaptureConn) Read(p []byte) (int, error)  { return 0, io.EOF }
func (c *writeCaptureConn) Close() error                { return nil }
func (c *writeCaptureConn) LocalAddr() net.Addr         { return nil }
func (c *writeCaptureConn) RemoteAddr() net.Addr        { return nil }
func (c *writeCaptureConn) SetDeadline(time.Time) error      { return nil }
func (c *writeCaptureConn) SetReadDeadline(time.Time) error  { return nil }
func (c *writeCaptureConn) SetWriteDeadline(time.Time) error { return nil }

func TestHandshakeHeaderConn_RewritesOnlyHandshakeHeaders(t *testing.T) {
	under := &writeCaptureConn{}
	c := newHandshakeHeaderConn(under, []string{"Sec-WebSocket-Protocol"})

	// Write handshake headers split across writes.
	part1 := []byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nSec-Websocket-Protocol:")
	part2 := []byte(" ocpp1.6\r\nConnection: Upgrade\r\n\r\n")
	if n, err := c.Write(part1); err != nil || n != len(part1) {
		t.Fatalf("write1 n=%d err=%v", n, err)
	}
	if n, err := c.Write(part2); err != nil || n != len(part2) {
		t.Fatalf("write2 n=%d err=%v", n, err)
	}

	out := under.w.String()
	if !bytes.Contains([]byte(out), []byte("\r\nSec-WebSocket-Protocol:")) {
		t.Fatalf("expected rewritten header name, got: %q", out)
	}
	if bytes.Contains([]byte(out), []byte("Sec-Websocket-Protocol:")) {
		t.Fatalf("did not want canonical header name, got: %q", out)
	}

	// After handshake, do not mutate payload bytes even if they contain the canonical header name.
	payload := []byte("payload Sec-Websocket-Protocol: should not be touched")
	_, _ = c.Write(payload)
	if !bytes.Contains(under.w.Bytes(), payload) {
		t.Fatalf("expected payload to pass through unchanged")
	}
}

