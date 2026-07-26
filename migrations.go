// Package bountyboard embeds SQL migrations so they ship inside the binary.
package bountyboard

import "embed"

// MigrationFS holds every file under migrations/.
//
//go:embed migrations/*.sql
var MigrationFS embed.FS
