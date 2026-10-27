package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func adminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("VELNEST_TEST_DATABASE_URL")
	if u == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("VELNEST_TEST_DATABASE_URL must be set in CI")
		}
		t.Skip("VELNEST_TEST_DATABASE_URL not set; skipping integration test")
	}
	return u
}

// newTestDB creates a throwaway database, returns its URL and drops it on cleanup.
func newTestDB(t *testing.T) string {
	t.Helper()
	admin := adminURL(t)
	suffix := make([]byte, 6)
	_, err := rand.Read(suffix)
	require.NoError(t, err)
	name := "velnest_test_" + hex.EncodeToString(suffix)

	adb, err := sql.Open("pgx", admin)
	require.NoError(t, err)
	t.Cleanup(func() { _ = adb.Close() })
	_, err = adb.ExecContext(context.Background(), "CREATE DATABASE "+name)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := adb.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		assert.NoError(t, err, "dropping throwaway database %s", name)
	})

	u, err := url.Parse(admin)
	require.NoError(t, err)
	u.Path = "/" + name
	return u.String()
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func runCmd(t *testing.T, dbURL string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), args, envOf(map[string]string{"DATABASE_MIGRATE_URL": dbURL}), &out)
	return out.String(), err
}

func TestUpDownVersionCycle(t *testing.T) {
	dbURL := newTestDB(t)

	out, err := runCmd(t, dbURL, "version")
	require.NoError(t, err)
	assert.Contains(t, out, "no migration")

	out, err = runCmd(t, dbURL, "up")
	require.NoError(t, err)
	assert.Contains(t, out, "migrations applied")

	out, err = runCmd(t, dbURL, "up")
	require.NoError(t, err, "second up must not fail")
	assert.Contains(t, out, "no change")

	out, err = runCmd(t, dbURL, "version")
	require.NoError(t, err)
	assert.Contains(t, out, "version: 1")
	assert.Contains(t, out, "dirty: false")

	out, err = runCmd(t, dbURL, "down")
	require.NoError(t, err)
	assert.Contains(t, out, "rolled back 1")

	out, err = runCmd(t, dbURL, "version")
	require.NoError(t, err)
	assert.Contains(t, out, "no migration")

	_, err = runCmd(t, dbURL, "up")
	require.NoError(t, err, "up again after down")

	out, err = runCmd(t, dbURL, "down", "1")
	require.NoError(t, err)
	assert.Contains(t, out, "rolled back 1")
}

func TestDownTooManySteps(t *testing.T) {
	dbURL := newTestDB(t)
	_, err := runCmd(t, dbURL, "up")
	require.NoError(t, err)
	_, err = runCmd(t, dbURL, "down", "5")
	require.Error(t, err, "rolling back more migrations than exist must fail, not silently succeed")
}

func TestPostgresqlSchemeAccepted(t *testing.T) {
	dbURL := newTestDB(t)
	u, err := url.Parse(dbURL)
	require.NoError(t, err)
	u.Scheme = "postgresql"
	_, err = runCmd(t, u.String(), "up")
	require.NoError(t, err)
}

func TestBadArgs(t *testing.T) {
	// These must fail before any connection is attempted, so no database needed.
	const dbURL = "postgres://u:secretpw@localhost:1/none?sslmode=disable"
	cases := map[string][]string{
		"no subcommand":      {},
		"unknown subcommand": {"sideways"},
		"down zero":          {"down", "0"},
		"down negative":      {"down", "-1"},
		"down non-numeric":   {"down", "abc"},
		"down float":         {"down", "1.5"},
		"down too many args": {"down", "1", "2"},
		"up with extra arg":  {"up", "1"},
		"version extra arg":  {"version", "x"},
		"down overflow":      {"down", "99999999999999999999"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			err := run(context.Background(), args, envOf(map[string]string{"DATABASE_MIGRATE_URL": dbURL}), &out)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secretpw")
		})
	}
}

func TestMissingURL(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"up"}, envOf(nil), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_MIGRATE_URL")

	err = run(context.Background(), []string{"up"}, envOf(map[string]string{"DATABASE_URL": "postgres://x"}), &out)
	require.Error(t, err, "only DATABASE_MIGRATE_URL is read")
}

func TestUnsupportedSchemeAndNoCredentialLeak(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"up"},
		envOf(map[string]string{"DATABASE_MIGRATE_URL": "mysql://user:secretpw@host/db"}), &out)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secretpw")
	assert.NotContains(t, out.String(), "secretpw")
}

func TestConnectionFailureDoesNotLeakCredentials(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"up"},
		envOf(map[string]string{"DATABASE_MIGRATE_URL": "postgres://user:secretpw@127.0.0.1:1/db?sslmode=disable"}), &out)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secretpw")
	assert.NotContains(t, out.String(), "secretpw")
}

func TestToMigrateURL(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"postgres://u:p@h:5432/db?sslmode=disable", "pgx5://u:p@h:5432/db?sslmode=disable", false},
		{"postgresql://u:p@h/db", "pgx5://u:p@h/db", false},
		{"pgx5://u:p@h/db", "", true},
		{"http://x", "", true},
		{"not a url", "", true},
		{"postgres://u:p@h:6543/db", "", true}, // transaction pooler
	}
	for _, c := range cases {
		got, err := toMigrateURL(c.in)
		if c.wantErr {
			require.Error(t, err, c.in)
			assert.NotContains(t, err.Error(), ":p@")
			continue
		}
		require.NoError(t, err, c.in)
		assert.Equal(t, c.want, got)
	}
}

func TestUsageMentionsPoolerWarning(t *testing.T) {
	assert.True(t, strings.Contains(usage, "6543"), "usage must warn about the transaction pooler")
	assert.Contains(t, usage, "DATABASE_MIGRATE_URL")
}
