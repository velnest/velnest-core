// Package migrations embeds the SQL migrations applied by cmd/migrate.
package migrations

import "embed"

// FS holds every *.sql migration file (golang-migrate naming:
// NNNNNN_name.up.sql / NNNNNN_name.down.sql).
//
//go:embed *.sql
var FS embed.FS
