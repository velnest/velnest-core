// Command worker is the background-job process. It is a skeleton until River
// arrives in W3: it connects nothing and just waits for a shutdown signal.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/davidsugianto/go-pkgs/logger"
	"github.com/davidsugianto/go-pkgs/logs"
	"github.com/davidsugianto/go-pkgs/otel"
	"github.com/rs/zerolog"

	"github.com/velnest/velnest-core/apps/api/internal/pkg/config"
)

func main() {
	os.Exit(realMain())
}

func realMain() int {
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
	logs.Infof("starting velnest-worker in %s mode", cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.Otel.Endpoint != "" {
		endpoint := otelEndpoint(cfg.Otel.Endpoint)
		provider, err := otel.NewProvider(ctx, otel.NewConfig("velnest-worker").
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

	if err := run(ctx, cfg); err != nil {
		logs.Errorf("worker: %v", err)
		return 1
	}
	logs.Info("worker stopped")
	return 0
}

// run blocks until ctx is cancelled. River workers will start here in W3.
func run(ctx context.Context, _ *config.Config) error {
	<-ctx.Done()
	return nil
}

// setupLogging sends JSON logs to STDOUT (no files: ephemeral disks).
func setupLogging(env string) {
	lgr := logger.NewWithConfig(logger.Config{
		Output:      os.Stdout,
		Level:       zerolog.InfoLevel,
		Format:      logger.FormatJSON,
		ServiceName: "velnest-worker",
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
