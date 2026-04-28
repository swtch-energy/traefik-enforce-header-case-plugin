package traefik_enforce_header_case_plugin

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"net/textproto"
	"strings"
)

// Config holds the configuration from your routes.yml
type Config struct {
	Headers []string `json:"headers,omitempty"`
}

func CreateConfig() *Config {
	return &Config{
		Headers: []string{},
	}
}

type ForceCasePlugin struct {
	next    http.Handler
	name    string
	headers []string
}

// New runs once when Traefik starts.
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	_ = ctx
	headers := []string(nil)
	if config != nil {
		headers = append(headers, config.Headers...)
	}

	return &ForceCasePlugin{
		next:    next,
		name:    name,
		headers: headers,
	}, nil
}

func (p *ForceCasePlugin) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// For requests, keep Go's canonical header keys so internal code that uses
	// Header.Get (including WebSocket handshake validation) still works.
	enforceHeaderCase(req.Header, p.headers, true)
	out := &responseCaseWriter{ResponseWriter: rw, headers: p.headers}
	p.next.ServeHTTP(out, req)
	// If downstream only set headers and returned, ensure those are re-keyed too.
	if !out.wroteHeader {
		enforceHeaderCase(out.ResponseWriter.Header(), p.headers, false)
	}
}

func enforceHeaderCase(hdr http.Header, keys []string, keepCanonical bool) {
	for _, want := range keys {
		if want == "" {
			continue
		}
		wantCanon := textproto.CanonicalMIMEHeaderKey(want)
		var found string
		for k := range hdr {
			if textproto.CanonicalMIMEHeaderKey(k) == wantCanon {
				found = k
				break
			}
		}
		if found == "" || found == want {
			continue
		}
		vals := hdr[found]
		// For responses, we want only the configured spelling on the wire, so we
		// delete the found key and replace it. For requests, keep canonical keys
		// so Header.Get continues to work for downstream code.
		if !keepCanonical {
			delete(hdr, found)
		}
		// Direct map assignment avoids Header.Set canonicalization.
		hdr[want] = append([]string(nil), vals...)
	}
}

type responseCaseWriter struct {
	http.ResponseWriter
	headers     []string
	wroteHeader bool
}

func (w *responseCaseWriter) WriteHeader(statusCode int) {
	if !w.wroteHeader {
		enforceHeaderCase(w.ResponseWriter.Header(), w.headers, false)
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseCaseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		// Avoid forcing a 200 here if nothing is written; still safe for normal writes.
		if len(p) != 0 {
			w.WriteHeader(http.StatusOK)
		}
	}
	return w.ResponseWriter.Write(p)
}

// Flush must not call WriteHeader(200). Traefik can Flush before the upstream
// writes status 101 during WebSocket upgrade; forcing 200 breaks upgrades.
func (w *responseCaseWriter) Flush() {
	if !w.wroteHeader {
		enforceHeaderCase(w.ResponseWriter.Header(), w.headers, false)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack must be delegated unchanged for WebSocket stability.
func (w *responseCaseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	if !w.wroteHeader {
		enforceHeaderCase(w.ResponseWriter.Header(), w.headers, false)
	}
	conn, brw, err := h.Hijack()
	if err != nil {
		return conn, brw, err
	}
	// Some Traefik/upgrade paths write the 101 response directly to the hijacked
	// net.Conn. If so, ResponseWriter.Header() re-keying above is bypassed. Wrap
	// only the handshake header block (up to \r\n\r\n) and then pass frames through.
	wrapped := newHandshakeHeaderConn(conn, w.headers)
	// Return a ReadWriter that writes to wrapped conn, but keep the original reader.
	// Use a 1-byte buffer to avoid introducing buffering/flush-related stalls that
	// can look like "idle" traffic and trigger 60s proxy/LB timeouts.
	newBrw := bufio.NewReadWriter(brw.Reader, bufio.NewWriterSize(wrapped, 1))
	return wrapped, newBrw, nil
}

type handshakeHeaderConn struct {
	net.Conn
	keys        []headerRewrite
	buf         []byte
	headersDone bool
}

type headerRewrite struct {
	canon string
	want  string
}

func newHandshakeHeaderConn(under net.Conn, wants []string) *handshakeHeaderConn {
	rules := make([]headerRewrite, 0, len(wants))
	for _, w := range wants {
		if w == "" {
			continue
		}
		c := http.CanonicalHeaderKey(w)
		if c == w {
			continue
		}
		rules = append(rules, headerRewrite{canon: c, want: w})
	}
	return &handshakeHeaderConn{Conn: under, keys: rules, buf: make([]byte, 0, 2048)}
}

func (c *handshakeHeaderConn) Write(p []byte) (int, error) {
	if c.headersDone || len(c.keys) == 0 {
		return c.Conn.Write(p)
	}

	// Buffer until we have the full HTTP header block.
	c.buf = append(c.buf, p...)
	idx := bytes.Index(c.buf, []byte("\r\n\r\n"))
	if idx == -1 {
		// Report success to the caller so it can continue writing; we will flush later.
		return len(p), nil
	}
	return c.flushHandshake(idx, len(p))
}

func (c *handshakeHeaderConn) flushHandshake(idx int, reportedN int) (int, error) {
	// Split: header part includes the terminator, remainder is passthrough.
	end := idx + 4
	head := c.buf[:end]
	tail := c.buf[end:]

	rewritten := c.rewriteHandshakeHeaderBlock(head)

	c.headersDone = true
	c.buf = c.buf[:0]

	// Write the rewritten header block, then any remainder.
	if _, err := c.Conn.Write(rewritten); err != nil {
		return 0, err
	}
	if len(tail) > 0 {
		if _, err := c.Conn.Write(tail); err != nil {
			return 0, err
		}
	}
	return reportedN, nil
}

func (c *handshakeHeaderConn) rewriteHandshakeHeaderBlock(head []byte) []byte {
	// Rewrite only header field names, not arbitrary substrings:
	// match start-of-block and CRLF + key + colon.
	h := string(head)
	for _, r := range c.keys {
		h = strings.ReplaceAll(h, "\r\n"+r.canon+":", "\r\n"+r.want+":")
		if strings.HasPrefix(h, r.canon+":") {
			h = r.want + ":" + h[len(r.canon)+1:]
		}
	}
	return []byte(h)
}
