// Package configs embeds the per-environment YAML files so the binary (and the
// Vercel Go build) ships them without reading from disk.
package configs

import "embed"

// FS holds config.<env>.yaml for every environment.
//
//go:embed config.*.yaml
var FS embed.FS
