package stdlib_test

import (
	"testing"

	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func FuzzTemplateParseAndExecute(f *testing.F) {
	for _, seed := range []string{
		"plain text",
		"{{.Name}}:{{range .Values}}{{.}},{{end}}",
		"{{if .Name}}{{printf \"%q\" .Name}}{{else}}empty{{end}}",
		"{{define \"part\"}}part={{.}}{{end}}{{template \"part\" .Name}}",
		"{{",
	} {
		f.Add(seed)
	}

	const root = "minigo.test/template-fuzz"
	program := prepareStdlibRunProgram(f, root, `package main

import (
	"bytes"
	"text/template"
)

func Run(source string) string {
	tmpl, err := template.New("fuzz").Parse(source)
	if err != nil {
		return "parse:" + err.Error()
	}
	var output bytes.Buffer
	err = tmpl.Execute(&output, map[string]any{"Name": "mini", "Values": []int{1, 2, 3}})
	if err != nil {
		return "execute:" + err.Error()
	}
	return "ok:" + output.String()
}
`)

	f.Fuzz(func(t *testing.T, templateSource string) {
		if len(templateSource) > 1024 {
			t.Skip()
		}
		firstValue, firstError := runFuzzString(program, minigoruntime.HostString(templateSource))
		secondValue, secondError := runFuzzString(program, minigoruntime.HostString(templateSource))
		if firstValue != secondValue || firstError != secondError {
			t.Fatalf("non-deterministic template result: (%q, %q) != (%q, %q)", firstValue, firstError, secondValue, secondError)
		}
	})
}
