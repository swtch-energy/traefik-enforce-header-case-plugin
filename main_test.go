package traefik_enforce_header_case_plugin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swtch-energy/traefik-enforce-header-case-plugin"
)

func preparePlugin(t *testing.T) (context.Context, *traefik_enforce_header_case_plugin.Config, http.Handler) {
	t.Helper()

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {})

	cfg := traefik_enforce_header_case_plugin.CreateConfig()
	cfg.Headers = append(cfg.Headers, "x-tEsT-hEAder")

	handler, err := traefik_enforce_header_case_plugin.New(ctx, next, cfg, "traefik-enforce-header-case-plugin")
	if err != nil {
		t.Fatal(err)
	}

	return ctx, cfg, handler
}

func prepareResponsePlugin(t *testing.T) (context.Context, *traefik_enforce_header_case_plugin.Config, http.Handler) {
	t.Helper()

	ctx := context.Background()
	cfg := traefik_enforce_header_case_plugin.CreateConfig()
	cfg.Headers = append(cfg.Headers, "x-tEsT-hEAder")
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("x-test-header", "456")
		rw.WriteHeader(http.StatusCreated)
		_, _ = rw.Write([]byte("ok"))
	})
	handler, err := traefik_enforce_header_case_plugin.New(ctx, next, cfg, "traefik-enforce-header-case-plugin")
	if err != nil {
		t.Fatal(err)
	}

	return ctx, cfg, handler
}

func prepareRequest(ctx context.Context, t *testing.T) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()

	recorder := httptest.NewRecorder()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost", nil)
	if err != nil {
		t.Fatal(err)
	}

	return recorder, req
}

func TestEnforceHeaderCaseWhenHeaderExists(t *testing.T) {
	ctx, cfg, handler := preparePlugin(t)
	recorder, req := prepareRequest(ctx, t)

	req.Header.Set("x-test-header", "123")
	handler.ServeHTTP(recorder, req)

	canonicalHeaderValue := req.Header.Get(cfg.Headers[0])
	if canonicalHeaderValue != "" {
		t.Errorf("unexpected value for canonicalised header: %s", canonicalHeaderValue)
	}

	caseEnforcedHeaderValue, ok := req.Header[cfg.Headers[0]]
	if !ok || len(caseEnforcedHeaderValue) != 1 || caseEnforcedHeaderValue[0] != "123" {
		t.Errorf("unexpected value for case-enforced header: %q", caseEnforcedHeaderValue)
	}
}

func TestEnforceHeaderCaseWhenHeaderDoesNotExist(t *testing.T) {
	ctx, cfg, handler := preparePlugin(t)
	recorder, req := prepareRequest(ctx, t)

	handler.ServeHTTP(recorder, req)

	canonicalHeaderValue := req.Header.Get(cfg.Headers[0])
	if canonicalHeaderValue != "" {
		t.Errorf("unexpected value for canonicalised header: %s", canonicalHeaderValue)
	}

	caseEnforcedHeaderValue, ok := req.Header[cfg.Headers[0]]
	if ok || len(caseEnforcedHeaderValue) != 0 {
		t.Errorf("unexpected value for case-enforced header: %q", caseEnforcedHeaderValue)
	}
}

func TestSecWebSocketKey_RFC_Casing(t *testing.T) {
	// Browsers and RFC 6455 use "Sec-WebSocket-Key" (mid-word capital S in WebSocket). Go
	// stores the HTTP/1.1 name as "Sec-Websocket-Key" in http.Header. We must re-key
	// using delete(found), not h.Del (which only removes the canonical name).
	t.Helper()
	ctx := context.Background()
	cfg := traefik_enforce_header_case_plugin.CreateConfig()
	const wantKey = "Sec-WebSocket-Key"
	cfg.Headers = []string{wantKey}

	checked := false
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		checked = true
		if req.Header == nil {
			t.Fatal("nil header")
		}
		if _, hasGo := req.Header["Sec-Websocket-Key"]; hasGo {
			t.Error("expected Go's Sec-Websocket-Key to be re-keyed to the configured spelling")
		}
		if v, has := req.Header[wantKey]; !has || len(v) != 1 || v[0] == "" {
			t.Errorf("expected %q with a value, got has=%v, val=%#v", wantKey, has, v)
		}
	})

	handler, err := traefik_enforce_header_case_plugin.New(ctx, next, cfg, "p")
	if err != nil {
		t.Fatal(err)
	}

	rec, req := prepareRequest(ctx, t)
	// Triggers the standard Go key used for this header in net/http.
	req.Header.Set("sec-websocket-key", "d3d3LmV4YW1wbGUuY29t")
	handler.ServeHTTP(rec, req)
	if !checked {
		t.Fatal("next handler not called")
	}
}

func TestEnforceHeaderCaseWhenUnconfiguredHeaderExists(t *testing.T) {
	ctx, _, handler := preparePlugin(t)
	recorder, req := prepareRequest(ctx, t)

	req.Header.Set("x-test-header-copy", "123")
	handler.ServeHTTP(recorder, req)

	canonicalHeaderValue := req.Header.Get("x-test-header-copy")
	if canonicalHeaderValue != "123" {
		t.Errorf("unexpected value for unconfigured header: %s", canonicalHeaderValue)
	}
}

func TestEnforceResponseHeaderCaseWhenHeaderExists(t *testing.T) {
	ctx, cfg, handler := prepareResponsePlugin(t)
	recorder, req := prepareRequest(ctx, t)

	handler.ServeHTTP(recorder, req)
	res := recorder.Result()
	defer res.Body.Close()

	_, hasCanon := res.Header[http.CanonicalHeaderKey(cfg.Headers[0])]
	if hasCanon {
		t.Errorf("did not want canonical key for response header %q in map: %#v", cfg.Headers[0], res.Header)
	}

	vs, ok := res.Header[cfg.Headers[0]]
	if !ok || len(vs) != 1 || vs[0] != "456" {
		t.Errorf("unexpected response header %q: %#v", cfg.Headers[0], res.Header[cfg.Headers[0]])
	}
}

func TestEnforceResponseHeaderCaseWhenHeaderDoesNotExist(t *testing.T) {
	ctx, cfg, _ := preparePlugin(t)
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusNoContent)
	})
	inner, err := traefik_enforce_header_case_plugin.New(ctx, next, cfg, "traefik-enforce-header-case-plugin")
	if err != nil {
		t.Fatal(err)
	}
	recorder, req := prepareRequest(ctx, t)
	inner.ServeHTTP(recorder, req)
	res := recorder.Result()
	defer res.Body.Close()
	if values, present := res.Header[cfg.Headers[0]]; present {
		t.Errorf("unexpected response header %q: %q", cfg.Headers[0], values)
	}
}
