package migrations_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers pgx5://
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/velnest/velnest-core/apps/api/migrations"
)

// tenantExempt lists the public tables that legitimately have no household_id.
// Everything else must carry household_id (directly, as the build-plan exit
// criterion for Foundation requires) so tenancy checks stay mechanical.
var tenantExempt = map[string]string{
	"users":             "global identity; a user can belong to many households",
	"households":        "the tenant itself (its primary key is the household id)",
	"schema_migrations": "golang-migrate bookkeeping, not tenant data",
}

// rlsExempt lists public tables allowed to have row level security disabled.
// Empty on purpose: schema_migrations has RLS enabled by 000001_init.
var rlsExempt = map[string]string{}

// adminURL returns the admin database URL or skips (fails under CI).
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

// newTestDB creates a throwaway database, returns its URL (postgres:// scheme)
// and drops it on cleanup.
func newTestDB(t *testing.T) string {
	t.Helper()
	admin := adminURL(t)
	suffix := make([]byte, 6)
	_, err := rand.Read(suffix)
	require.NoError(t, err)
	name := "velnest_test_" + hex.EncodeToString(suffix)

	ctx := context.Background()
	adb, err := sql.Open("pgx", admin)
	require.NoError(t, err)
	t.Cleanup(func() { _ = adb.Close() })
	_, err = adb.ExecContext(ctx, "CREATE DATABASE "+name)
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

func newMigrator(t *testing.T, dbURL string) *migrate.Migrate {
	t.Helper()
	src, err := iofs.New(migrations.FS, ".")
	require.NoError(t, err)
	u, err := url.Parse(dbURL)
	require.NoError(t, err)
	u.Scheme = "pgx5"
	m, err := migrate.NewWithSourceInstance("iofs", src, u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	return m
}

func openDB(t *testing.T, dbURL string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// migratedDB returns a connection to a fresh database with all migrations applied.
func migratedDB(t *testing.T) *sql.DB {
	t.Helper()
	dbURL := newTestDB(t)
	require.NoError(t, newMigrator(t, dbURL).Up())
	return openDB(t, dbURL)
}

func TestUpDownUp(t *testing.T) {
	dbURL := newTestDB(t)
	m := newMigrator(t, dbURL)

	require.NoError(t, m.Up())
	v, dirty, err := m.Version()
	require.NoError(t, err)
	assert.EqualValues(t, 1, v)
	assert.False(t, dirty)

	assert.ErrorIs(t, m.Up(), migrate.ErrNoChange, "second up must be a no-op")

	require.NoError(t, m.Steps(-1), "down")
	_, _, err = m.Version()
	assert.ErrorIs(t, err, migrate.ErrNilVersion)

	db := openDB(t, dbURL)
	assert.Empty(t, publicTables(t, db, "schema_migrations"), "down must drop every table it created")

	require.NoError(t, m.Up(), "up again after down")
	v, dirty, err = m.Version()
	require.NoError(t, err)
	assert.EqualValues(t, 1, v)
	assert.False(t, dirty)
}

// publicTables lists base tables in schema public, excluding the given names.
func publicTables(t *testing.T, db *sql.DB, exclude ...string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
		ORDER BY c.relname`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	skip := map[string]bool{}
	for _, e := range exclude {
		skip[e] = true
	}
	var out []string
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		if !skip[n] {
			out = append(out, n)
		}
	}
	require.NoError(t, rows.Err())
	return out
}

// tablesMissingHouseholdID returns public tables that lack a household_id
// column and are not in the allowlist.
func tablesMissingHouseholdID(ctx context.Context, db *sql.DB, exempt map[string]string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_attribute a
		    WHERE a.attrelid = c.oid AND a.attname = 'household_id'
		      AND a.attnum > 0 AND NOT a.attisdropped)
		ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if _, ok := exempt[n]; !ok {
			out = append(out, n)
		}
	}
	return out, rows.Err()
}

// tablesWithoutRLS returns public tables with row level security disabled
// that are not in the allowlist.
func tablesWithoutRLS(ctx context.Context, db *sql.DB, exempt map[string]string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND NOT c.relrowsecurity
		ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if _, ok := exempt[n]; !ok {
			out = append(out, n)
		}
	}
	return out, rows.Err()
}

func TestEveryTableHasHouseholdID(t *testing.T) {
	db := migratedDB(t)
	bad, err := tablesMissingHouseholdID(context.Background(), db, tenantExempt)
	require.NoError(t, err)
	for _, tbl := range bad {
		t.Errorf("table public.%s has no household_id column: add `household_id uuid not null references households(id)` "+
			"(tenancy invariant), or add it to tenantExempt in migrations_test.go with a justification", tbl)
	}
}

func TestHouseholdIDGuardDetectsViolations(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `CREATE TABLE scratch_no_tenant (id uuid primary key)`)
	require.NoError(t, err)
	bad, err := tablesMissingHouseholdID(ctx, db, tenantExempt)
	require.NoError(t, err)
	assert.Equal(t, []string{"scratch_no_tenant"}, bad)

	_, err = db.ExecContext(ctx, `ALTER TABLE scratch_no_tenant ADD COLUMN household_id uuid`)
	require.NoError(t, err)
	bad, err = tablesMissingHouseholdID(ctx, db, tenantExempt)
	require.NoError(t, err)
	assert.Empty(t, bad, "adding household_id must satisfy the guard")
}

func TestEveryTableHasRLS(t *testing.T) {
	db := migratedDB(t)
	bad, err := tablesWithoutRLS(context.Background(), db, rlsExempt)
	require.NoError(t, err)
	for _, tbl := range bad {
		t.Errorf("table public.%s has row level security disabled: add `ALTER TABLE %s ENABLE ROW LEVEL SECURITY;` "+
			"to its migration (Supabase exposes public via the Data API)", tbl, tbl)
	}
	// The three init tables and the migrate bookkeeping table are all covered.
	all := publicTables(t, db)
	assert.Equal(t, []string{"household_members", "households", "schema_migrations", "users"}, all)
}

func TestRLSGuardDetectsViolations(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE scratch_no_rls (id uuid primary key, household_id uuid)`)
	require.NoError(t, err)
	bad, err := tablesWithoutRLS(ctx, db, rlsExempt)
	require.NoError(t, err)
	assert.Equal(t, []string{"scratch_no_rls"}, bad)
}

func TestRLSEnabledButNotForced(t *testing.T) {
	db := migratedDB(t)
	rows, err := db.QueryContext(context.Background(), `
		SELECT c.relname, c.relforcerowsecurity
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var forced bool
		require.NoError(t, rows.Scan(&name, &forced))
		assert.False(t, forced, "%s must not FORCE RLS (the owner role runs the Go server)", name)
	}
	require.NoError(t, rows.Err())

	var policies int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM pg_policies WHERE schemaname = 'public'`).Scan(&policies))
	assert.Zero(t, policies, "init migration defines no policies (deny-by-default for Data API roles)")
}

// requireSQLState asserts err is a Postgres error with the given SQLSTATE
// (and, if non-empty, constraint name).
func requireSQLState(t *testing.T, err error, code, constraint string) {
	t.Helper()
	require.Error(t, err)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr, "expected a postgres error, got %v", err)
	assert.Equal(t, code, pgErr.Code, pgErr.Message)
	if constraint != "" {
		assert.Equal(t, constraint, pgErr.ConstraintName)
	}
}

const (
	sqlCheckViolation  = "23514"
	sqlUniqueViolation = "23505"
)

func mustUser(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var id string
	require.NoError(t, db.QueryRowContext(context.Background(),
		`INSERT INTO users (name) VALUES ($1) RETURNING id`, name).Scan(&id))
	return id
}

func mustHousehold(t *testing.T, db *sql.DB, name, createdBy string) string {
	t.Helper()
	var id string
	require.NoError(t, db.QueryRowContext(context.Background(),
		`INSERT INTO households (name, created_by) VALUES ($1, $2) RETURNING id`, name, createdBy).Scan(&id))
	return id
}

func TestDefaults(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	uid := mustUser(t, db, "Budi")
	hid := mustHousehold(t, db, "Rumah Budi", uid)

	var locale string
	var created, updated time.Time
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT locale, created_at, updated_at FROM users WHERE id = $1`, uid).Scan(&locale, &created, &updated))
	assert.Equal(t, "id", locale)
	assert.False(t, created.IsZero())
	assert.False(t, updated.IsZero())

	var tz, plan string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT timezone, plan FROM households WHERE id = $1`, hid).Scan(&tz, &plan))
	assert.Equal(t, "Asia/Jakarta", tz)
	assert.Equal(t, "free", plan)

	_, err := db.ExecContext(ctx,
		`INSERT INTO household_members (household_id, user_id, role) VALUES ($1, $2, 'owner')`, hid, uid)
	require.NoError(t, err)
	var status string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT status FROM household_members WHERE household_id = $1 AND user_id = $2`, hid, uid).Scan(&status))
	assert.Equal(t, "active", status)
}

func TestConstraints(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	uid := mustUser(t, db, "Budi")
	hid := mustHousehold(t, db, "Rumah Budi", uid)
	other := mustUser(t, db, "Sari")

	t.Run("bad locale", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `INSERT INTO users (name, locale) VALUES ('x', 'fr')`)
		requireSQLState(t, err, sqlCheckViolation, "")
	})
	t.Run("bad timezone", func(t *testing.T) {
		_, err := db.ExecContext(ctx,
			`INSERT INTO households (name, timezone, created_by) VALUES ('x', 'Europe/Paris', $1)`, uid)
		requireSQLState(t, err, sqlCheckViolation, "")
	})
	t.Run("bad plan", func(t *testing.T) {
		_, err := db.ExecContext(ctx,
			`INSERT INTO households (name, plan, created_by) VALUES ('x', 'enterprise', $1)`, uid)
		requireSQLState(t, err, sqlCheckViolation, "")
	})
	t.Run("bad role", func(t *testing.T) {
		_, err := db.ExecContext(ctx,
			`INSERT INTO household_members (household_id, user_id, role) VALUES ($1, $2, 'superuser')`, hid, other)
		requireSQLState(t, err, sqlCheckViolation, "")
	})
	t.Run("bad status", func(t *testing.T) {
		_, err := db.ExecContext(ctx,
			`INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'member', 'zombie')`, hid, other)
		requireSQLState(t, err, sqlCheckViolation, "")
	})
	t.Run("duplicate member", func(t *testing.T) {
		_, err := db.ExecContext(ctx,
			`INSERT INTO household_members (household_id, user_id, role) VALUES ($1, $2, 'owner')`, hid, uid)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx,
			`INSERT INTO household_members (household_id, user_id, role) VALUES ($1, $2, 'member')`, hid, uid)
		requireSQLState(t, err, sqlUniqueViolation, "")
	})
	t.Run("household needs existing creator", func(t *testing.T) {
		_, err := db.ExecContext(ctx,
			`INSERT INTO households (name, created_by) VALUES ('x', gen_random_uuid())`)
		requireSQLState(t, err, "23503", "")
	})
	t.Run("duplicate email ignoring case", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `INSERT INTO users (name, email) VALUES ('a', 'Budi@Example.com')`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO users (name, email) VALUES ('b', 'budi@example.COM')`)
		requireSQLState(t, err, sqlUniqueViolation, "")
	})
	t.Run("null emails do not collide", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `INSERT INTO users (name) VALUES ('n1'), ('n2')`)
		require.NoError(t, err)
	})
	t.Run("duplicate phone", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `INSERT INTO users (name, phone) VALUES ('a', '+6281234567890')`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO users (name, phone) VALUES ('b', '+6281234567890')`)
		requireSQLState(t, err, sqlUniqueViolation, "")
	})
	t.Run("empty email and phone rejected", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `INSERT INTO users (name, email) VALUES ('a', '')`)
		requireSQLState(t, err, sqlCheckViolation, "")
		_, err = db.ExecContext(ctx, `INSERT INTO users (name, phone) VALUES ('a', '')`)
		requireSQLState(t, err, sqlCheckViolation, "")
	})
	t.Run("all valid enum values accepted", func(t *testing.T) {
		for _, loc := range []string{"id", "en"} {
			_, err := db.ExecContext(ctx, `INSERT INTO users (name, locale) VALUES ('l', $1)`, loc)
			require.NoError(t, err, loc)
		}
		for _, tz := range []string{"Asia/Jakarta", "Asia/Makassar", "Asia/Jayapura"} {
			_, err := db.ExecContext(ctx, `INSERT INTO households (name, timezone, created_by) VALUES ('t', $1, $2)`, tz, uid)
			require.NoError(t, err, tz)
		}
		for _, plan := range []string{"free", "plus"} {
			_, err := db.ExecContext(ctx, `INSERT INTO households (name, plan, created_by) VALUES ('p', $1, $2)`, plan, uid)
			require.NoError(t, err, plan)
		}
		roles := []string{"owner", "admin", "member", "viewer", "helper"}
		statuses := []string{"active", "invited", "removed"}
		for i, role := range roles {
			h := mustHousehold(t, db, "r"+role, uid)
			u := mustUser(t, db, "u"+role)
			_, err := db.ExecContext(ctx,
				`INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, $3, $4)`,
				h, u, role, statuses[i%len(statuses)])
			require.NoError(t, err, role)
		}
	})
}

func TestMembersCascadeOnHouseholdDelete(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	uid := mustUser(t, db, "Budi")
	member := mustUser(t, db, "Sari")
	h1 := mustHousehold(t, db, "H1", uid)
	h2 := mustHousehold(t, db, "H2", uid)
	for _, h := range []string{h1, h2} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO household_members (household_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`,
			h, uid, member)
		require.NoError(t, err)
	}

	_, err := db.ExecContext(ctx, `DELETE FROM households WHERE id = $1`, h1)
	require.NoError(t, err)

	count := func(h string) int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT count(*) FROM household_members WHERE household_id = $1`, h).Scan(&n))
		return n
	}
	assert.Zero(t, count(h1), "members of the deleted household must be removed")
	assert.Equal(t, 2, count(h2), "other households are untouched")

	var users int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&users))
	assert.Equal(t, 2, users, "deleting a household must not delete users")
}

func TestMembersCascadeOnUserDelete(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	owner := mustUser(t, db, "Budi")
	member := mustUser(t, db, "Sari")
	h := mustHousehold(t, db, "H", owner)
	_, err := db.ExecContext(ctx,
		`INSERT INTO household_members (household_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`,
		h, owner, member)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, member)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM household_members WHERE household_id = $1`, h).Scan(&n))
	assert.Equal(t, 1, n)

	// The creator cannot be deleted while a household references it.
	_, err = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, owner)
	requireSQLState(t, err, "23503", "")
}

func TestIndexes(t *testing.T) {
	db := migratedDB(t)
	rows, err := db.QueryContext(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' ORDER BY indexname`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var defs []string
	for rows.Next() {
		var d string
		require.NoError(t, rows.Scan(&d))
		defs = append(defs, d)
	}
	require.NoError(t, rows.Err())
	sort.Strings(defs)
	joined := strings.Join(defs, "\n")
	assert.Contains(t, joined, "lower(email)")
	assert.Contains(t, joined, "(phone)")
	assert.Contains(t, joined, "ON public.households USING btree (created_by)")
	assert.Contains(t, joined, "ON public.household_members USING btree (user_id)")
}
