// Package config loads the embedded per-environment YAML and applies
// environment-variable overrides.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/velnest/velnest-core/apps/api/configs"
)

const defaultPort = 7979

// Config is the application configuration.
type Config struct {
	Env      string         `yaml:"-"`
	Server   ServerConfig   `yaml:"server"`
	CORS     CORSConfig     `yaml:"cors"`
	Database DatabaseConfig `yaml:"database"`
	Otel     OtelConfig     `yaml:"otel"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int `yaml:"port"`
}

// CORSConfig holds CORS settings.
type CORSConfig struct {
	AllowedOrigins   []string `yaml:"allowed_origins"`
	AllowedMethods   []string `yaml:"allowed_methods"`
	AllowedHeaders   []string `yaml:"allowed_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
}

// DatabaseConfig holds Postgres connection settings.
type DatabaseConfig struct {
	URL                  string        `yaml:"url"`
	PreferSimpleProtocol bool          `yaml:"prefer_simple_protocol"`
	MaxOpenConns         int           `yaml:"max_open_conns"`
	MaxIdleConns         int           `yaml:"max_idle_conns"`
	ConnMaxLifetime      time.Duration `yaml:"conn_max_lifetime"`
	ConnMaxIdleTime      time.Duration `yaml:"conn_max_idle_time"`
}

// OtelConfig holds OpenTelemetry settings. Tracing is off when Endpoint is empty.
type OtelConfig struct {
	Endpoint string `yaml:"endpoint"`
}

// ResolveEnv returns the environment name from ENV. When ENV is unset it falls
// back to "development" for local runs, but fails on Vercel (VERCEL is set
// there): a deployment that forgot ENV must not start with development CORS,
// debug mode and the wrong database protocol.
func ResolveEnv(getenv func(string) string) (string, error) {
	if env := getenv("ENV"); env != "" {
		return env, nil
	}
	if getenv("VERCEL") != "" {
		return "", errors.New("ENV is not set: set ENV to staging or production in the Vercel project")
	}
	return "development", nil
}

// Load reads configs/config.<env>.yaml from the embedded FS, applies the PORT,
// DATABASE_URL and OTEL_EXPORTER_OTLP_ENDPOINT overrides, and validates it.
func Load(env string) (*Config, error) {
	switch env {
	case "development", "staging", "production":
	default:
		return nil, fmt.Errorf("unknown environment %q (want development, staging or production)", env)
	}

	raw, err := configs.FS.ReadFile("config." + env + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("read config for %s: %w", env, err)
	}

	cfg := &Config{Server: ServerConfig{Port: defaultPort}}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config for %s: %w", env, err)
	}
	cfg.Env = env

	if err := cfg.applyEnvOverrides(); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyEnvOverrides() error {
	if v := os.Getenv("PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("invalid PORT %q", v)
		}
		c.Server.Port = port
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		c.Database.URL = v
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); v != "" {
		c.Otel.Endpoint = v
	}
	return nil
}

func (c *Config) validate() error {
	if c.Database.URL == "" {
		return errors.New("database url is empty: set DATABASE_URL")
	}
	if c.Server.Port == 0 {
		c.Server.Port = defaultPort
	}
	return nil
}
