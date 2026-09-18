package stdlib_test

import (
	"bytes"
	stdjson "encoding/json"
	stdxml "encoding/xml"
	stdhtml "html"
	stdtemplate "html/template"
	"io"
	"strings"
	"testing"

	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func FuzzStructuredCodecs(f *testing.F) {
	for _, seed := range []struct {
		mode  int64
		input string
	}{
		{0, `{"name":"mini","values":[1,2,3]}`},
		{0, `{"broken":`},
		{1, `<root xmlns="urn:mini"><child id="1">text</child></root>`},
		{1, `<root><child></root>`},
		{2, `<a href='x'>&"`},
		{3, `</script><b>mini</b>`},
	} {
		f.Add(seed.mode, seed.input)
	}

	const root = "minigo.test/structured-stdlib-fuzz"
	program := prepareStdlibRunProgram(f, root, `package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"html"
	"html/template"
	"io"
	"strings"
)

func Run(mode int64, input string) string {
	switch mode & 3 {
	case 0:
		var output bytes.Buffer
		if err := json.Compact(&output, []byte(input)); err != nil {
			return "error"
		}
		return "ok:" + output.String()
	case 1:
		decoder := xml.NewDecoder(strings.NewReader(input))
		result := ""
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				return "ok:" + result
			}
			if err != nil {
				return "error"
			}
			switch value := token.(type) {
			case xml.StartElement:
				result += "<" + value.Name.Space + ":" + value.Name.Local + ">"
			case xml.EndElement:
				result += "</" + value.Name.Space + ":" + value.Name.Local + ">"
			case xml.CharData:
				result += string(value)
			}
		}
	case 2:
		return html.EscapeString(input) + "|" + html.UnescapeString(input)
	default:
		tmpl, err := template.New("fuzz").Parse("<script>const value={{.}};</script>")
		if err != nil {
			return "parse-error"
		}
		var output bytes.Buffer
		if err := tmpl.Execute(&output, input); err != nil {
			return "execute-error"
		}
		return output.String()
	}
}
`)

	f.Fuzz(func(t *testing.T, mode int64, input string) {
		if len(input) > 2048 {
			t.Skip()
		}
		got, runError := runFuzzString(program, minigoruntime.HostInt("Int64", mode), minigoruntime.HostString(input))
		if runError != "" {
			t.Fatalf("MiniGo structured stdlib failed: %s", runError)
		}
		want := structuredCodecOracle(mode, input)
		if got != want {
			t.Fatalf("mode %d result = %q, want %q", mode&3, got, want)
		}
	})
}

func structuredCodecOracle(mode int64, input string) string {
	switch mode & 3 {
	case 0:
		var output bytes.Buffer
		if err := stdjson.Compact(&output, []byte(input)); err != nil {
			return "error"
		}
		return "ok:" + output.String()
	case 1:
		decoder := stdxml.NewDecoder(strings.NewReader(input))
		result := ""
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				return "ok:" + result
			}
			if err != nil {
				return "error"
			}
			switch value := token.(type) {
			case stdxml.StartElement:
				result += "<" + value.Name.Space + ":" + value.Name.Local + ">"
			case stdxml.EndElement:
				result += "</" + value.Name.Space + ":" + value.Name.Local + ">"
			case stdxml.CharData:
				result += string(value)
			}
		}
	case 2:
		return stdhtml.EscapeString(input) + "|" + stdhtml.UnescapeString(input)
	default:
		tmpl := stdtemplate.Must(stdtemplate.New("fuzz").Parse(`<script>const value={{.}};</script>`))
		var output bytes.Buffer
		if err := tmpl.Execute(&output, input); err != nil {
			return "execute-error"
		}
		return output.String()
	}
}
