package traefik_enforce_header_case_plugin

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
)

// Config holds the plugin configuration.
type Config struct {
	Headers []string `json:"headers,omitempty"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		Headers: []string{},
	}
}

// ForceCasePlugin holds the necessary components of a Traefik plugin
type ForceCasePlugin struct {
	next    http.Handler
	name    string
	headers []string
}

// New instantiates and returns the required components used to handle a HTTP request
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	return &ForceCasePlugin{
		next:    next,
		name:    name,
		headers: config.Headers,
	}, nil
}

func (p *ForceCasePlugin) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// 1. FIX INCOMING REQUESTS (Client -> Backend)
	for _, expectedHeader := range p.headers {
		canonical := http.CanonicalHeaderKey(expectedHeader)
		if values, exists := req.Header[canonical]; exists {
			req.Header[expectedHeader] = values
			if expectedHeader != canonical {
				delete(req.Header, canonical)
			}
		}
	}

	// 2. INTERCEPT OUTGOING RESPONSES (Backend -> Client)
	interceptor := &responseInterceptor{
		ResponseWriter: rw,
		headers:        p.headers,
	}

	// Pass the request forward using our interceptor
	p.next.ServeHTTP(interceptor, req)
}

// responseInterceptor wraps http.ResponseWriter to fix casing before it goes to the client
type responseInterceptor struct {
	http.ResponseWriter
	headers []string
}

// WriteHeader catches the response right before it is sent to the client
func (r *responseInterceptor) WriteHeader(statusCode int) {
	for _, expectedHeader := range r.headers {
		canonical := http.CanonicalHeaderKey(expectedHeader)
		if values, exists := r.Header()[canonical]; exists {
			r.Header()[expectedHeader] = values
			if expectedHeader != canonical {
				delete(r.Header(), canonical)
			}
		}
	}
	r.ResponseWriter.WriteHeader(statusCode)
}

// Hijack is CRITICAL! Without this, WebSockets will fail instantly in Go.
func (r *responseInterceptor) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}
	return hijacker.Hijack()
}

// Flush ensures streaming responses (like SSE) keep working
func (r *responseInterceptor) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
