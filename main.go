package traefik_enforce_header_case_plugin

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
)

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

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	return &ForceCasePlugin{
		next:    next,
		name:    name,
		headers: config.Headers,
	}, nil
}

func (p *ForceCasePlugin) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// WE DO NOT TOUCH THE REQUEST. Let Traefik handle the backend upgrade normally.

	// Wrap the response to catch the handshake reply
	interceptor := &responseInterceptor{
		ResponseWriter: rw,
		headers:        p.headers,
	}

	p.next.ServeHTTP(interceptor, req)
}

type responseInterceptor struct {
	http.ResponseWriter
	headers []string
}

// WriteHeader catches the HTTP 101 Handshake right before it goes to the client
func (r *responseInterceptor) WriteHeader(statusCode int) {
	// Only trigger our casing logic during the WebSocket handshake
	if statusCode == http.StatusSwitchingProtocols {
		for _, expectedHeader := range r.headers {
			canonical := http.CanonicalHeaderKey(expectedHeader)

			// If Traefik/Backend generated the canonical header...
			if values, exists := r.Header()[canonical]; exists {
				// Inject the strict casing directly into the underlying map
				r.Header()[expectedHeader] = values

				// Delete the canonical version so only the strict one is sent
				if expectedHeader != canonical {
					delete(r.Header(), canonical)
				}
			}
		}
	}
	r.ResponseWriter.WriteHeader(statusCode)
}

// Hijack is required so Traefik can take over the TCP stream AFTER the 101 handshake
func (r *responseInterceptor) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}
	return hijacker.Hijack()
}

func (r *responseInterceptor) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
