package frontend

import (
	"context"

	"gopkg.d7z.net/go-mini/core/ast"
)

type SourceFile struct {
	URI      string
	Filename string
	Language string
	Code     string
}

type SourceBundle struct {
	Language string
	Files    []SourceFile
	Sources  map[string]string
}

type Mode struct {
	Tolerant bool
}

type Frontend interface {
	Language() string
	Parse(ctx context.Context, files []SourceFile, mode Mode) (*ast.ProgramStmt, *SourceBundle, []error, error)
}

func NewSourceBundle(language string, files []SourceFile) *SourceBundle {
	copied := append([]SourceFile(nil), files...)
	sources := make(map[string]string, len(copied))
	for _, file := range copied {
		if file.URI != "" {
			sources[file.URI] = file.Code
		}
		if file.Filename != "" {
			sources[file.Filename] = file.Code
		}
	}
	return &SourceBundle{
		Language: language,
		Files:    copied,
		Sources:  sources,
	}
}
