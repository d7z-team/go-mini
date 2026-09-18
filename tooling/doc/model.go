// Package doc renders compiler-owned source documentation as Markdown.
package doc

import documentation "github.com/d7z-team/mini-go/compiler/doc"

type (
	Catalog       = documentation.Catalog
	Package       = documentation.Package
	Symbol        = documentation.Symbol
	TypeParameter = documentation.TypeParameter
	Member        = documentation.Member
	Comment       = documentation.Comment
	Note          = documentation.Note
	BuildRequest  = documentation.BuildRequest
)

type File struct {
	Path string
	Text []byte
}
type Output struct{ Files []File }

func Build(request BuildRequest) (Catalog, error) { return documentation.Build(request) }
