package traefik_enforce_header_case_plugin

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/textproto"
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
	enforceHeaderCase(req.Header, p.headers)
	out := &responseCaseWriter{ResponseWriter: rw, headers: p.headers}
	p.next.ServeHTTP(out, req)
	// If downstream only set headers and returned, ensure those are re-keyed too.
	if !out.wroteHeader {
		enforceHeaderCase(out.ResponseWriter.Header(), p.headers)
	}
}

func enforceHeaderCase(hdr http.Header, keys []string) {
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
		delete(hdr, found)
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
		enforceHeaderCase(w.ResponseWriter.Header(), w.headers)
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
		enforceHeaderCase(w.ResponseWriter.Header(), w.headers)
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
		enforceHeaderCase(w.ResponseWriter.Header(), w.headers)
	}
	return h.Hijack()
}
