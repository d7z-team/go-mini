package stdlib_test

import (
	stdregexp "regexp"
	"strconv"
	"testing"

	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func FuzzRegexpCompileAndMatch(f *testing.F) {
	for _, seed := range [][2]string{
		{`^[[:alpha:]][[:alnum:]_]*$`, "mini_42"},
		{`(?P<word>\p{Greek}+)`, "x αβ y"},
		{`a*`, "baaac"},
		{`([`, "invalid"},
	} {
		f.Add(seed[0], seed[1])
	}
	program := prepareStdlibRunProgram(f, "minigo.test/regexp-fuzz", `package main

import (
	"regexp"
	"strconv"
)

func Run(pattern, input string) string {
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return "error"
	}
	location := expression.FindStringIndex(input)
	if location == nil {
		return "none"
	}
	return strconv.Itoa(location[0]) + ":" + strconv.Itoa(location[1]) + ":" + expression.ReplaceAllString(input, "<$0>")
}
`)
	f.Fuzz(func(t *testing.T, pattern, input string) {
		if len(pattern) > 256 || len(input) > 512 {
			t.Skip()
		}
		got, runError := runFuzzString(program, minigoruntime.HostString(pattern), minigoruntime.HostString(input))
		if runError != "" {
			t.Fatalf("MiniGo regexp failed: %s", runError)
		}
		expression, err := stdregexp.Compile(pattern)
		want := "error"
		if err == nil {
			location := expression.FindStringIndex(input)
			if location == nil {
				want = "none"
			} else {
				want = strconv.Itoa(location[0]) + ":" + strconv.Itoa(location[1]) + ":" + expression.ReplaceAllString(input, "<$0>")
			}
		}
		if got != want {
			t.Fatalf("regexp result = %q, want %q", got, want)
		}
	})
}
