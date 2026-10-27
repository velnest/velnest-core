package main

import (
	"fmt"
	"net/http"

	"github.com/davidsugianto/go-pkgs/grace"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	httpHandler "github.com/velnest/velnest-core/apps/api/internal/handler/http"
	"github.com/velnest/velnest-core/apps/api/internal/handler/middleware"
	"github.com/velnest/velnest-core/apps/api/internal/pkg/config"
)

// Server wires the Gin engine to the handlers.
type Server struct {
	*http.Server
	handler *httpHandler.Handler
	config  *config.Config
}

// Dependencies are the Server's collaborators.
type Dependencies struct {
	Config *config.Config
	DB     httpHandler.Pinger
}

// New builds a Server.
func New(deps Dependencies) *Server {
	return &Server{
		Server:  &http.Server{},
		handler: httpHandler.New(httpHandler.Dependencies{DB: deps.DB}),
		config:  deps.Config,
	}
}

func (s *Server) routes(r *gin.Engine) {
	v1 := r.Group("/v1")
	v1.GET("/ping", s.handler.Ping)
	v1.GET("/health", s.handler.Health)
}

func (s *Server) engine() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestID(), cors.New(cors.Config{
		AllowOrigins:     s.config.CORS.AllowedOrigins,
		AllowMethods:     s.config.CORS.AllowedMethods,
		AllowHeaders:     s.config.CORS.AllowedHeaders,
		AllowCredentials: s.config.CORS.AllowCredentials,
	}))
	s.routes(r)
	return r
}

// Run serves HTTP on addr and blocks until SIGINT/SIGTERM, then shuts down gracefully.
func (s *Server) Run(addr string) error {
	s.Addr = addr
	s.Handler = s.engine()
	return grace.ServeHTTP(s.Addr, s.Handler)
}

func listenAddr(cfg *config.Config) string {
	port := cfg.Server.Port
	if port == 0 {
		port = 7979
	}
	return fmt.Sprintf(":%d", port)
}
