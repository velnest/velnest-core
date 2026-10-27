package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/velnest/velnest-core/apps/api/internal/pkg/config"
)

func clearEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
}

func TestLoadDevelopment(t *testing.T) {
	clearEnv(t)

	cfg, err := config.Load("development")
	require.NoError(t, err)

	assert.Equal(t, "development", cfg.Env)
	assert.Equal(t, 7979, cfg.Server.Port)
	assert.Contains(t, cfg.CORS.AllowedOrigins, "http://localhost:4321")
	assert.Contains(t, cfg.Database.URL, "localhost:54322")
	assert.False(t, cfg.Database.PreferSimpleProtocol)
	assert.Equal(t, 30*time.Minute, cfg.Database.ConnMaxLifetime)
	assert.Empty(t, cfg.Otel.Endpoint)
}

func TestLoadEnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://u:p@db:6543/x")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4317")

	cfg, err := config.Load("development")
	require.NoError(t, err)

	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, "postgres://u:p@db:6543/x", cfg.Database.URL)
	assert.Equal(t, "collector:4317", cfg.Otel.Endpoint)
}

func TestLoadUnknownEnv(t *testing.T) {
	clearEnv(t)

	cfg, err := config.Load("qa")
	require.Error(t, err)
	assert.Nil(t, cfg)
}

func TestLoadRemoteEnvsRequireDatabaseURL(t *testing.T) {
	for _, env := range []string{"staging", "production"} {
		t.Run(env, func(t *testing.T) {
			clearEnv(t)

			cfg, err := config.Load(env)
			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), "DATABASE_URL")

			t.Setenv("DATABASE_URL", "postgres://u:p@pooler:6543/postgres")
			cfg, err = config.Load(env)
			require.NoError(t, err)
			assert.True(t, cfg.Database.PreferSimpleProtocol)
			assert.Equal(t, 5, cfg.Database.MaxOpenConns)
			assert.Equal(t, 2, cfg.Database.MaxIdleConns)
		})
	}
}

func TestResolveEnv(t *testing.T) {
	tests := []struct {
		name    string
		vars    map[string]string
		want    string
		wantErr bool
	}{
		{name: "explicit env wins", vars: map[string]string{"ENV": "staging"}, want: "staging"},
		{name: "explicit env wins on vercel", vars: map[string]string{"ENV": "production", "VERCEL": "1"}, want: "production"},
		{name: "unset locally defaults to development", vars: map[string]string{}, want: "development"},
		// A forgotten ENV on Vercel must not silently run a deployment with
		// development CORS, debug mode and the wrong DB protocol.
		{name: "unset on vercel fails closed", vars: map[string]string{"VERCEL": "1"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.ResolveEnv(func(k string) string { return tt.vars[k] })
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "ENV")
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
