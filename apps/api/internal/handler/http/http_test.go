package http_test

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpHandler "github.com/velnest/velnest-core/apps/api/internal/handler/http"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

type envelope struct {
	Code  int            `json:"code"`
	Data  map[string]any `json:"data"`
	Error string         `json:"error"`
}

// The health endpoint is unauthenticated, so a failing ping must never leak the
// driver error (DB user, host, port) to the caller.
func TestHealthDoesNotLeakDatabaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	leaky := errors.New("ping database: failed to connect to `user=velnest database=velnest`: [::1]:5432 (db.internal): dial error")
	h := httpHandler.New(httpHandler.Dependencies{DB: fakePinger{err: leaky}})
	r := gin.New()
	r.GET("/health", h.Health)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(nethttp.MethodGet, "/health", nil))

	assert.Equal(t, 503, w.Code)
	body := w.Body.String()
	for _, secret := range []string{"velnest", "db.internal", "5432", "dial error", "failed to connect"} {
		assert.NotContains(t, body, secret)
	}
}

func TestHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		path       string
		pinger     httpHandler.Pinger
		wantStatus int
		wantData   map[string]any
		wantError  string
	}{
		{name: "ping ok", path: "/ping", pinger: fakePinger{}, wantStatus: 200, wantData: map[string]any{"status": "ok"}},
		{name: "ping ignores db", path: "/ping", pinger: fakePinger{err: errors.New("down")}, wantStatus: 200, wantData: map[string]any{"status": "ok"}},
		{name: "health ok", path: "/health", pinger: fakePinger{}, wantStatus: 200, wantData: map[string]any{"status": "ok", "db": "up"}},
		{name: "health db down", path: "/health", pinger: fakePinger{err: errors.New("connection refused")}, wantStatus: 503, wantError: "database unavailable"},
		{name: "health nil pinger", path: "/health", pinger: nil, wantStatus: 503, wantError: "database unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := httpHandler.New(httpHandler.Dependencies{DB: tt.pinger})
			r := gin.New()
			r.GET("/ping", h.Ping)
			r.GET("/health", h.Health)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(nethttp.MethodGet, tt.path, nil))

			assert.Equal(t, tt.wantStatus, w.Code)
			var got envelope
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			assert.Equal(t, tt.wantStatus, got.Code)
			assert.Equal(t, tt.wantData, got.Data)
			assert.Equal(t, tt.wantError, got.Error)
		})
	}
}
