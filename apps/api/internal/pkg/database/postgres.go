// Package database is a local Supabase-safe Postgres adapter.
//
// It exists because go-pkgs/db cannot yet take a URL DSN or enable the pgx
// simple protocol, which Supabase's transaction pooler (port 6543) requires.
// Drop this package once go-pkgs/db gains both.
package database

import (
	"context"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"

	"github.com/velnest/velnest-core/apps/api/internal/pkg/config"
)

// Client wraps *gorm.DB with health and shutdown helpers.
type Client struct {
	*gorm.DB
}

// New opens a GORM connection and verifies it with a ping.
func New(ctx context.Context, cfg config.DatabaseConfig) (*Client, error) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  cfg.URL,
		PreferSimpleProtocol: cfg.PreferSimpleProtocol,
	}), &gorm.Config{
		// Prepared statements are incompatible with the transaction pooler.
		PrepareStmt:            false,
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}

	if err := db.Use(tracing.NewPlugin(tracing.WithoutMetrics())); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("enable tracing: %w", err)
	}

	c := &Client{DB: db}
	if err := c.Ping(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return c, nil
}

// Ping checks that the database is reachable.
func (c *Client) Ping(ctx context.Context) error {
	sqlDB, err := c.DB.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// Close releases the connection pool.
func (c *Client) Close() error {
	sqlDB, err := c.DB.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}
	return sqlDB.Close()
}
