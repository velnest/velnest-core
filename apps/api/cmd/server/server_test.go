package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/velnest/velnest-core/apps/api/internal/pkg/config"
)

const allowedOrigin = "http://localhost:4321"

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func testServer(t *testing.T, pingErr error) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Env:    "development",
		Server: config.ServerConfig{Port: 7979},
		CORS: config.CORSConfig{
			AllowedOrigins:   []string{allowedOrigin},
			AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
			AllowedHeaders:   []string{"Content-Type", "X-Request-ID"},
			AllowCredentials: true,
		},
	}
	return New(Dependencies{Config: cfg, DB: fakePinger{err: pingErr}}).engine()
}

func do(h http.Handler, method, path string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		path       string
		wantStatus int
	}{
		{name: "ping", path: "/v1/ping", wantStatus: http.StatusOK},
		{name: "health ok", path: "/v1/health", wantStatus: http.StatusOK},
		{name: "health db down", pingErr: errors.New("boom"), path: "/v1/health", wantStatus: http.StatusServiceUnavailable},
		{name: "unversioned path is not served", path: "/ping", wantStatus: http.StatusNotFound},
		{name: "unknown path", path: "/v1/nope", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := do(testServer(t, tt.pingErr), http.MethodGet, tt.path, nil)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestRequestIDHeader(t *testing.T) {
	h := testServer(t, nil)

	t.Run("generated when absent", func(t *testing.T) {
		w := do(h, http.MethodGet, "/v1/ping", nil)
		assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
	})
	t.Run("echoed when supplied", func(t *testing.T) {
		w := do(h, http.MethodGet, "/v1/ping", map[string]string{"X-Request-ID": "req-123"})
		assert.Equal(t, "req-123", w.Header().Get("X-Request-ID"))
	})
}

func TestCORS(t *testing.T) {
	h := testServer(t, nil)

	t.Run("allowed origin on a normal request", func(t *testing.T) {
		w := do(h, http.MethodGet, "/v1/ping", map[string]string{"Origin": allowedOrigin})
		assert.Equal(t, allowedOrigin, w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	})
	t.Run("allowed origin preflight", func(t *testing.T) {
		w := do(h, http.MethodOptions, "/v1/ping", map[string]string{
			"Origin":                        allowedOrigin,
			"Access-Control-Request-Method": "GET",
		})
		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, allowedOrigin, w.Header().Get("Access-Control-Allow-Origin"))
	})
	t.Run("other origin gets no CORS grant", func(t *testing.T) {
		w := do(h, http.MethodGet, "/v1/ping", map[string]string{"Origin": "https://evil.example"})
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})
}

func TestListenAddr(t *testing.T) {
	tests := []struct {
		port int
		want string
	}{
		{port: 0, want: ":7979"},
		{port: 8080, want: ":8080"},
	}
	for _, tt := range tests {
		cfg := &config.Config{Server: config.ServerConfig{Port: tt.port}}
		require.Equal(t, tt.want, listenAddr(cfg))
	}
}

func TestOtelEndpoint(t *testing.T) {
	tests := map[string]string{
		"collector:4317":          "collector:4317",
		"http://collector:4317":   "collector:4317",
		"https://collector:4317/": "collector:4317",
	}
	for in, want := range tests {
		assert.Equal(t, want, otelEndpoint(in), in)
	}
}
