package migrations

import "embed"

// FS contains the versioned schema migrations shipped with Project Hub.
//
//go:embed *.sql
var FS embed.FS
