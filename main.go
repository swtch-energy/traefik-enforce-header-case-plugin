// Package traefik_enforce_header_case_plugin is a Traefik middleware plugin that rewrites
// the spelling of specific header names in request and response header maps
// (e.g. Sec-Websocket-Key → Sec-WebSocket-Key) for the next step inside Traefik.
//
// A Go *http.Request on the backend re-canonicalizes header names on parse, so
// the map key there is still Go’s form (e.g. Sec-Websocket-Key). See the README
// section “Why the backend can still look wrong”.
package traefik_enforce_header_case_plugin

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/textproto"
)

// Config plugin configuration.
type Config struct {
	// Headers lists the exact header name spelling you want (key casing only).
	// Only headers that are already present are rewritten. Example: browsers send
	// sec-websocket-key, while Go storage uses Sec-Websocket-Key; to get RFC
	// spelling "Sec-WebSocket-Key" on the wire, include that exact string here.
	Headers []string `json:"headers,omitempty"`
}

// CreateConfig creates an empty plugin configuration.
func CreateConfig() *Config {
	return &Config{
		Headers: []string{},
	}
}

// TraefikEnforceHeaderCaseMiddleware plugin.
type TraefikEnforceHeaderCaseMiddleware struct {
	name    string
	next    http.Handler
	headers []string
}

// New creates a new TraefikEnforceHeaderCaseMiddleware plugin.
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	return &TraefikEnforceHeaderCaseMiddleware{
		name:    name,
		next:    next,
		headers: config.Headers,
	}, nil
}

// enforceHeaderCase rewrites the keys of the given map to match each entry in keys
// exactly. Matching is by Go's case-insensitive header rules (textproto
// canonicalization), not by string equality, so the stored key Sec-Websocket-Key
// and the wanted key Sec-WebSocket-Key (RFC 6455) are still matched.
// http.Header.Del is not used to remove the old name: it always deletes the
// canonical form only, and would not remove a previously set custom-cased key.
func enforceHeaderCase(h http.Header, keys []string) {
	for _, want := range keys {
		if want == "" {
			continue
		}
		wantCanon := textproto.CanonicalMIMEHeaderKey(want)
		var found string
		for k := range h {
			if textproto.CanonicalMIMEHeaderKey(k) == wantCanon {
				found = k
				break
			}
		}
		if found == "" || found == want {
			continue
		}
		vals := h[found]
		delete(h, found)
		h[want] = append([]string(nil), vals...)
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
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

// Flush is implemented so that optional http.Flusher is preserved on the
// underlying ResponseWriter, and so headers are rewritten if Flush runs first.
func (w *responseCaseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

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

func (w *responseCaseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (ehc *TraefikEnforceHeaderCaseMiddleware) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	enforceHeaderCase(req.Header, ehc.headers)
	out := &responseCaseWriter{ResponseWriter: rw, headers: ehc.headers}
	ehc.next.ServeHTTP(out, req)
	// If the downstream never called Write, WriteHeader, or Flush, headers were never enforced.
	// (That can happen, for example, with a handler that only sets response headers and returns.)
	if !out.wroteHeader {
		enforceHeaderCase(out.ResponseWriter.Header(), ehc.headers)
	}
}
