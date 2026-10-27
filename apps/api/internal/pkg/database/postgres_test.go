package database_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/velnest/velnest-core/apps/api/internal/pkg/config"
	"github.com/velnest/velnest-core/apps/api/internal/pkg/database"
)

// testURL returns VELNEST_TEST_DATABASE_URL, skipping locally when unset but
// failing in CI so the integration test cannot be silently dropped.
func testURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("VELNEST_TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("VELNEST_TEST_DATABASE_URL must be set in CI")
		}
		t.Skip("VELNEST_TEST_DATABASE_URL not set")
	}
	return url
}

func newSimpleProtocolClient(t *testing.T) *database.Client {
	t.Helper()
	client, err := database.New(context.Background(), config.DatabaseConfig{
		URL:                  testURL(t),
		PreferSimpleProtocol: true,
		MaxOpenConns:         1,
		MaxIdleConns:         1,
		ConnMaxLifetime:      time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestPing(t *testing.T) {
	client := newSimpleProtocolClient(t)
	require.NoError(t, client.Ping(context.Background()))
}

func TestSimpleProtocolLeavesNoPreparedStatements(t *testing.T) {
	client := newSimpleProtocolClient(t)

	for range 2 {
		var got int
		require.NoError(t, client.Raw("SELECT ?::int", 42).Scan(&got).Error)
		assert.Equal(t, 42, got)
	}

	// MaxOpenConns is 1, so this runs on the very connection used above.
	var prepared int64
	require.NoError(t, client.Raw("SELECT count(*) FROM pg_prepared_statements").Scan(&prepared).Error)
	assert.Zero(t, prepared)
}

func TestNewFailsWhenUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := database.New(ctx, config.DatabaseConfig{
		URL: "postgres://velnest:velnest@localhost:1/velnest?sslmode=disable",
	})
	require.Error(t, err)
	assert.Nil(t, client)
}
