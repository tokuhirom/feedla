package store

import "embed"

// migrationsFS embeds the *.sql migration files applied at startup.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS
