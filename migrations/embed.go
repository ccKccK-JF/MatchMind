package migrations

import "embed"

// Files contains ordered PostgreSQL migrations used by cmd/migrate.
//
//go:embed *.sql
var Files embed.FS
