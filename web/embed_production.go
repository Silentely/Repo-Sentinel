//go:build production

package web

import "embed"

const embeddedRoot = "dist"

//go:embed dist
var embeddedFiles embed.FS
