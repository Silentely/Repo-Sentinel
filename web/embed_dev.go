//go:build !production

package web

import "embed"

const embeddedRoot = "fallback"

//go:embed fallback
var embeddedFiles embed.FS
