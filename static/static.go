// Package static embeds the files served under /static.
package static

import "embed"

//go:embed style.css
var FS embed.FS
