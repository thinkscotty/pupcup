// Package migrations holds the embedded SQL files used by internal/store.
// Files are applied in lexical order; create new ones as 000N_description.sql.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
