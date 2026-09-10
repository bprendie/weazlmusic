package web

import "embed"

//go:embed *.html *.css *.js *.png
var Files embed.FS
