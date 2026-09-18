//go:build !minigo

package compilerentry

import "context"

func toolRequestContext(_, _ string) (context.Context, func(), error) {
	return context.Background(), func() {}, nil
}
