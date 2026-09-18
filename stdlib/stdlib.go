// Package stdlib embeds the Mini-Go standard-library source tree and declares
// the host capabilities used by that source.
package stdlib

import (
	"embed"
	"io/fs"
)

//go:embed src
var embedded embed.FS

// Open returns the immutable standard-library source tree.
func Open() fs.FS {
	sources, err := fs.Sub(embedded, "src")
	if err != nil {
		panic(err)
	}
	return sources
}
