package regexplib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestMatchString(t *testing.T) {
	testutil.Run(t, `
package main
import "regexp"

func main() {
	ok, err := regexp.MatchString("a.c", "abc")
	if err != nil || !ok {
		panic("regexp.MatchString failed")
	}
}
`)
}

func TestRegexpStringHelpers(t *testing.T) {
	testutil.Run(t, `
package main
import "regexp"

func main() {
	matches, err := regexp.FindAllString("[a-z]+", "a1 bb22 ccc", -1)
	if err != nil || len(matches) != 3 || matches[0] != "a" || matches[2] != "ccc" {
		panic("regexp.FindAllString failed")
	}

	idx, err := regexp.FindStringIndex("[0-9]+", "abc123")
	if err != nil || len(idx) != 2 || idx[0] != 3 || idx[1] != 6 {
		panic("regexp.FindStringIndex failed")
	}

	subIdx, err := regexp.FindStringSubmatchIndex("([a-z]+)([0-9]+)", "abc123")
	if err != nil || len(subIdx) != 6 || subIdx[2] != 0 || subIdx[5] != 6 {
		panic("regexp.FindStringSubmatchIndex failed")
	}

	groups, err := regexp.FindAllStringSubmatch("([a-z]+)([0-9]+)", "a1 bb22", -1)
	if err != nil || len(groups) != 2 || len(groups[1]) != 3 || groups[1][1] != "bb" || groups[1][2] != "22" {
		panic("regexp.FindAllStringSubmatch failed")
	}

	replaced, err := regexp.ReplaceAllString("[0-9]+", "a1b22", "#")
	if err != nil || replaced != "a#b#" {
		panic("regexp.ReplaceAllString failed")
	}

	literal, err := regexp.ReplaceAllLiteralString("[0-9]+", "a1", "$x")
	if err != nil || literal != "a$x" {
		panic("regexp.ReplaceAllLiteralString failed")
	}

	parts, err := regexp.Split("[,;]", "a,b;c", -1)
	if err != nil || len(parts) != 3 || parts[1] != "b" {
		panic("regexp.Split failed")
	}
}
`)
}

func TestRegexpInvalidPatternReturnsError(t *testing.T) {
	testutil.Run(t, `
package main
import "regexp"

func main() {
	_, err := regexp.FindAllString("[", "abc", -1)
	if err == nil {
		panic("expected regexp error")
	}
}
`)
}
