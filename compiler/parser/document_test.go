package parser

import (
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/ast"
)

func TestDocumentSharesLosslessScanAndPartialAST(t *testing.T) {
	source := "package main\n// keep\nvar bad = \x00\nfunc Good() {}\n"
	document := ParseDocument("example/main", "main.mgo", source)
	var rebuilt strings.Builder
	for _, element := range document.Elements {
		rebuilt.WriteString(element.Lexeme)
	}
	if rebuilt.String() != source || len(document.Diagnostics) == 0 {
		t.Fatalf("unexpected document: %#v", document)
	}
	found := false
	for _, decl := range document.Program.Files[0].Decls {
		found = found || decl.Kind == ast.DeclFunc && decl.Func.Name == "Good"
	}
	if !found {
		t.Fatalf("partial AST lost later declaration: %#v", document.Program.Files[0].Decls)
	}
}

func TestParseGoEmbedDirectives(t *testing.T) {
	result := ParseSource("example", "embed.mgo", "package example\nimport _ \"embed\"\n//go:embed text/*.txt `file with space.txt`\nvar content []byte\n")
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	decl := result.Program.Files[0].Decls[1]
	if got := decl.Var.EmbedPatterns; len(got) != 2 || got[0] != "text/*.txt" || got[1] != "file with space.txt" {
		t.Fatalf("embed patterns = %#v", got)
	}
}

func TestParseGoEmbedDirectiveBinding(t *testing.T) {
	result := ParseSource("example", "embed.mgo", `package example
import _ "embed"

//go:embed first.txt

// an ordinary comment may separate the directive and declaration
var first string

var (
	//go:embed second.txt
	second string
	third string
)
`)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	var patterns [][]string
	for _, decl := range result.Program.Files[0].Decls {
		if decl.Kind == ast.DeclVar {
			patterns = append(patterns, decl.Var.EmbedPatterns)
		}
	}
	if len(patterns) != 3 || len(patterns[0]) != 1 || patterns[0][0] != "first.txt" || len(patterns[1]) != 1 || patterns[1][0] != "second.txt" || len(patterns[2]) != 0 {
		t.Fatalf("embed binding = %#v", patterns)
	}
}

func TestParseGoEmbedRejectsNonVariableTargets(t *testing.T) {
	for _, sourceText := range []string{
		"package example\n//go:embed value.txt\nconst value = 1\n",
		"package example\n//go:embed value.txt\nfunc value() {}\n",
		"package example\n//go:embed value.txt\nvar (\n\tvalue string\n)\n",
		"package example\nfunc value() {\n//go:embed value.txt\nvar local string\n}\n",
	} {
		result := ParseSource("example", "embed.mgo", sourceText)
		found := false
		for _, diagnostic := range result.Diagnostics {
			found = found || diagnostic.Code == "parser.embed.declaration"
		}
		if !found {
			t.Fatalf("missing declaration diagnostic for %q: %#v", sourceText, result.Diagnostics)
		}
	}
}
