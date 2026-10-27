package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/velnest/velnest-core/apps/api/internal/handler/middleware"
)

func TestRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		incoming string
		wantSame bool
	}{
		{name: "echoes provided id", incoming: "abc-123", wantSame: true},
		{name: "generates id when missing", incoming: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var inContext any
			r := gin.New()
			r.Use(middleware.RequestID())
			r.GET("/", func(c *gin.Context) {
				inContext = c.Request.Context().Value(middleware.RequestIDKey)
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.incoming != "" {
				req.Header.Set("X-Request-ID", tt.incoming)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			got := w.Header().Get("X-Request-ID")
			require.NotEmpty(t, got)
			assert.Equal(t, got, inContext)
			if tt.wantSame {
				assert.Equal(t, tt.incoming, got)
			}
		})
	}
}
