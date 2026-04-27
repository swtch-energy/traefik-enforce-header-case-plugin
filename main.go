package traefik_enforce_header_case_plugin

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
)

// Config holds the configuration from your routes.yml
type Config struct {
	Headers []string `json:"headers,omitempty"`
}

func CreateConfig() *Config {
	return &Config{}
}

type ForceCasePlugin struct {
	next         http.Handler
	name         string
	searches     [][]byte
	replacements [][]byte
}

// New runs once when Traefik starts. It pre-computes the search/replace lists.
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	var searches, replacements [][]byte

	// Dynamically build the byte replacement list based on your config
	for _, h := range config.Headers {
		canonical := http.CanonicalHeaderKey(h)
		// Only track it if Go's default casing differs from your strict casing
		if h != canonical {
			searches = append(searches, []byte(canonical))
			replacements = append(replacements, []byte(h))
		}
	}

	return &ForceCasePlugin{
		next:         next,
		name:         name,
		searches:     searches,
		replacements: replacements,
	}, nil
}

func (p *ForceCasePlugin) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	interceptor := &hijackInterceptor{
		ResponseWriter: rw,
		searches:       p.searches,
		replacements:   p.replacements,
	}
	p.next.ServeHTTP(interceptor, req)
}

type hijackInterceptor struct {
	http.ResponseWriter
	searches     [][]byte
	replacements [][]byte
}

// Hijack intercepts the TCP stream when Traefik upgrades to WebSockets
func (r *hijackInterceptor) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}

	conn, brw, err := hijacker.Hijack()
	if err != nil {
		return conn, brw, err
	}

	// Wrap the raw connection with our dynamic replacer
	wrappedConn := &tcpByteReplacer{
		Conn:         conn,
		searches:     r.searches,
		replacements: r.replacements,
	}

	newWriter := bufio.NewWriter(wrappedConn)
	newBrw := bufio.NewReadWriter(brw.Reader, newWriter)

	return wrappedConn, newBrw, nil
}

func (r *hijackInterceptor) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// tcpByteReplacer inspects raw TCP bytes and replaces all configured headers
type tcpByteReplacer struct {
	net.Conn
	searches     [][]byte
	replacements [][]byte
	headersDone  bool
}

// Write intercepts bytes right before they hit the network card
func (c *tcpByteReplacer) Write(p []byte) (int, error) {
	// Only scan the stream during the HTTP handshake phase
	if !c.headersDone {
		// Loop through every header in your config
		for i := range c.searches {
			if bytes.Contains(p, c.searches[i]) {
				// Replace the canonical Go header with your strict custom header
				p = bytes.Replace(p, c.searches[i], c.replacements[i], 1)
			}
		}

		// Stop scanning the stream once the HTTP headers finish ( \r\n\r\n )
		if bytes.Contains(p, []byte("\r\n\r\n")) {
			c.headersDone = true
		}
	}

	// Pass the modified bytes to the real network connection
	return c.Conn.Write(p)
}
