// Package http holds the Gin HTTP handlers.
package http

import "context"

// Pinger reports whether a dependency (the database) is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler serves the HTTP endpoints.
type Handler struct {
	db Pinger
}

// Dependencies are the Handler's collaborators.
type Dependencies struct {
	DB Pinger
}

// New builds a Handler.
func New(deps Dependencies) *Handler {
	return &Handler{db: deps.DB}
}
