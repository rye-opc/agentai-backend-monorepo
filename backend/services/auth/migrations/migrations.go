package migrations

import "embed"

// FS contains embedded SQL migrations for the auth service.
//
//go:embed *.sql
var FS embed.FS
