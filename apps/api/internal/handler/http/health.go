package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/davidsugianto/go-pkgs/logs"
	"github.com/davidsugianto/go-pkgs/response"
	"github.com/gin-gonic/gin"

	"github.com/velnest/velnest-core/apps/api/internal/handler/middleware"
)

const healthTimeout = 2 * time.Second

// errDatabaseUnavailable is the only failure detail sent to callers. The health
// endpoint is unauthenticated, so driver errors (DB user, host, port) stay in
// the server log.
var errDatabaseUnavailable = errors.New("database unavailable")

// Health is a readiness check that pings the database.
func (h *Handler) Health(c *gin.Context) {
	if h.db == nil {
		logs.Errorf("health: database not configured")
		response.GinError(c, http.StatusServiceUnavailable, errDatabaseUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), healthTimeout)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		reqID, _ := c.Request.Context().Value(middleware.RequestIDKey).(string)
		logs.Errorf("health: ping database (request_id=%s): %v", reqID, err)
		response.GinError(c, http.StatusServiceUnavailable, errDatabaseUnavailable)
		return
	}
	response.GinSuccess(c, gin.H{"status": "ok", "db": "up"})
}
