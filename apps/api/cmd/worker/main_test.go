package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/velnest/velnest-core/apps/api/internal/pkg/config"
)

func TestRunReturnsNilWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, &config.Config{Env: "development"}) }()

	select {
	case err := <-done:
		t.Fatalf("run returned before cancel: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after context cancel")
	}
}

func TestRunReturnsNilWhenAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NoError(t, run(ctx, &config.Config{Env: "development"}))
}
