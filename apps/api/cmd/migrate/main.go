// Command migrate applies the embedded SQL migrations to the database named by
// DATABASE_MIGRATE_URL.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// scheme
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/velnest/velnest-core/apps/api/migrations"
)

const envURL = "DATABASE_MIGRATE_URL"

const usage = `usage: migrate <command>

commands:
  up          apply all pending migrations
  down [n]    roll back n migrations (default 1; n must be a positive integer)
  version     print the current migration version and dirty flag

environment:
  DATABASE_MIGRATE_URL  postgres:// or postgresql:// URL of the database to migrate.
                        Use a Supabase direct connection or session-pooler URL
                        (port 5432). Never use the transaction pooler (port 6543):
                        migrations need session state such as advisory locks.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

// run executes one migrate command. It never writes the database URL or
// credentials to stdout or into returned errors.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	// Validate arguments before touching the environment or the network.
	if len(args) == 0 {
		return fmt.Errorf("missing command\n\n%s", usage)
	}
	cmd := args[0]
	steps := 0
	switch cmd {
	case "up", "version":
		if len(args) != 1 {
			return fmt.Errorf("%s takes no arguments\n\n%s", cmd, usage)
		}
	case "down":
		steps = 1
		switch len(args) {
		case 1:
		case 2:
			n, err := strconv.Atoi(args[1])
			if err != nil || n <= 0 {
				return fmt.Errorf("invalid step count %q: must be a positive integer\n\n%s", args[1], usage)
			}
			steps = n
		default:
			return fmt.Errorf("down takes at most one argument\n\n%s", usage)
		}
	default:
		return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}

	raw := getenv(envURL)
	if raw == "" {
		return fmt.Errorf("%s is not set\n\n%s", envURL, usage)
	}
	dsn, err := toMigrateURL(raw)
	if err != nil {
		return err
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	// Let an in-flight migration finish its current statement on cancellation.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			m.GracefulStop <- true
		case <-done:
		}
	}()

	switch cmd {
	case "up":
		return report(stdout, m.Up(), "migrations applied")
	case "down":
		return report(stdout, m.Steps(-steps), fmt.Sprintf("rolled back %d migration(s)", steps))
	default: // version
		v, dirty, err := m.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			_, _ = fmt.Fprintln(stdout, "version: no migration applied")
			return nil
		}
		if err != nil {
			return fmt.Errorf("read version: %w", err)
		}
		_, _ = fmt.Fprintf(stdout, "version: %d\ndirty: %t\n", v, dirty)
		return nil
	}
}

// report prints msg on success, "no change" for migrate.ErrNoChange, and
// returns any other error.
func report(w io.Writer, err error, msg string) error {
	switch {
	case err == nil:
		_, _ = fmt.Fprintln(w, msg)
	case errors.Is(err, migrate.ErrNoChange):
		_, _ = fmt.Fprintln(w, "no change")
	default:
		return err
	}
	return nil
}

// toMigrateURL converts a postgres:// or postgresql:// URL to the pgx5://
// scheme golang-migrate's pgx/v5 driver registers. Errors never include the
// URL, since it carries credentials.
func toMigrateURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s is not a valid URL", envURL)
	}
	switch u.Scheme {
	case "postgres", "postgresql":
	default:
		return "", fmt.Errorf("%s must use the postgres:// or postgresql:// scheme", envURL)
	}
	if u.Port() == "6543" {
		return "", fmt.Errorf("%s points at port 6543 (Supabase transaction pooler); use the direct or session-pooler connection (port 5432)", envURL)
	}
	u.Scheme = "pgx5"
	return u.String(), nil
}
