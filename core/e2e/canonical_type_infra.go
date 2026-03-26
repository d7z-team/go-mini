//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg e2e -path gopkg.d7z.net/go-mini/core/e2e -out canonical_type_ffigen.go canonical_type_infra.go
package e2e

import (
	"context"

	a_other "gopkg.d7z.net/go-mini/core/e2e/internal/a/other"
	b_other "gopkg.d7z.net/go-mini/core/e2e/internal/b/other"
)

// ffigen:module test_canonical
type TestCanonicalService interface {
	NewA(ctx context.Context, name string) *a_other.Type
	NewB(ctx context.Context, id int) *b_other.Type
}

// ffigen:methods a_other.Type
type ATypeService interface {
	Hello(t *a_other.Type) string
}

// ffigen:methods b_other.Type
type BTypeService interface {
	Hello(t *b_other.Type) string
}
