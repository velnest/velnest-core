// Command server runs the Velnest HTTP API.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/davidsugianto/go-pkgs/logger"
	"github.com/davidsugianto/go-pkgs/logs"
	"github.com/davidsugianto/go-pkgs/otel"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/velnest/velnest-core/apps/api/internal/pkg/config"
	"github.com/velnest/velnest-core/apps/api/internal/pkg/database"
)

func main() {
	os.Exit(run())
}

func run() int {
	env, err := config.ResolveEnv(os.Getenv)
	if err != nil {
		// Logging is not configured yet, so report straight to stderr.
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	setupLogging(env)

	cfg, err := config.Load(env)
	if err != nil {
		logs.Errorf("load config: %v", err)
		return 1
	}
	if cfg.Env != "development" {
		gin.SetMode(gin.ReleaseMode)
	}
	logs.Infof("starting velnest-api in %s mode", cfg.Env)

	ctx := context.Background()

	if cfg.Otel.Endpoint != "" {
		endpoint := otelEndpoint(cfg.Otel.Endpoint)
		provider, err := otel.NewProvider(ctx, otel.NewConfig("velnest-api").
			WithEnvironment(cfg.Env).
			WithTraceEndpoint(endpoint).
			WithMetricEndpoint(endpoint))
		if err != nil {
			logs.Errorf("init otel: %v", err)
			return 1
		}
		defer func() {
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := provider.Shutdown(flushCtx); err != nil {
				logs.Errorf("flush otel: %v", err)
			}
		}()
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := database.New(connectCtx, cfg.Database)
	cancel()
	if err != nil {
		logs.Errorf("connect database: %v", err)
		return 1
	}
	defer func() {
		if err := db.Close(); err != nil {
			logs.Errorf("close database: %v", err)
		}
	}()

	server := New(Dependencies{Config: cfg, DB: db})

	addr := listenAddr(cfg)
	logs.Infof("listening on %s", addr)
	if err := server.Run(addr); err != nil {
		logs.Errorf("server: %v", err)
		return 1
	}
	logs.Info("server stopped")
	return 0
}

// setupLogging sends JSON logs to STDOUT (no files: Vercel and containers have
// ephemeral disks). logs.NewLogger cannot target stdout, so the underlying
// logger is configured directly.
func setupLogging(env string) {
	lgr := logger.NewWithConfig(logger.Config{
		Output:      os.Stdout,
		Level:       zerolog.InfoLevel,
		Format:      logger.FormatJSON,
		ServiceName: "velnest-api",
		Environment: env,
	})
	for _, level := range []logs.Level{logs.DebugLevel, logs.InfoLevel, logs.WarnLevel, logs.ErrorLevel, logs.FatalLevel} {
		if err := logs.SetLogger(level, lgr); err != nil {
			panic(err)
		}
	}
}

// otelEndpoint turns an OTLP URL into the host:port the gRPC exporter expects.
func otelEndpoint(v string) string {
	v = strings.TrimPrefix(v, "http://")
	v = strings.TrimPrefix(v, "https://")
	return strings.TrimSuffix(v, "/")
}
