package migrations

import "embed"

// FS содержит все SQL-миграции, вшитые в бинарник при сборке.
//
//go:embed *.sql
var FS embed.FS
