// Package migrations embeds the goose SQL migrations into the binary, so
// that deploying HDMS is running the binary — no separate migration
// artifact to ship or forget.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
