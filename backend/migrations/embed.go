package migrations

import "embed"

// Files contains the versioned SQL migrations shipped with the backend.
//
//go:embed *.sql
var Files embed.FS
