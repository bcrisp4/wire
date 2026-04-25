// Package schema embeds the SQL migration files.
package schema

import "embed"

//go:embed *.sql
var FS embed.FS
