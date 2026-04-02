package urllib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestQueryEscape(t *testing.T) {
	testutil.Run(t, `
package main
import "net/url"

func main() {
	if url.QueryEscape("a b") != "a+b" {
		panic("QueryEscape failed")
	}
}
`)
}
