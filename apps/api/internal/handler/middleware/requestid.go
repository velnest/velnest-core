// Package middleware holds Gin middleware.
package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type contextKey string

// RequestIDKey is the context key under which the request ID is stored.
const RequestIDKey contextKey = "request_id"

// RequestID reads X-Request-ID (or generates one), stores it in the request
// context and echoes it on the response.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()
		}

		ctx := context.WithValue(c.Request.Context(), RequestIDKey, reqID)
		c.Request = c.Request.WithContext(ctx)
		c.Writer.Header().Set("X-Request-ID", reqID)

		c.Next()
	}
}
