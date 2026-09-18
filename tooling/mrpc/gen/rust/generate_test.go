package rustgen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/tooling/mrpc"
)

func catalog(t *testing.T, declarations string) mrpc.Catalog {
	t.Helper()
	file, diagnostics := mrpc.Parse(mrpc.Source{Path: "sample.mrpc", Text: "syntax = \"mrpc/v2\"; namespace sample.v1; option rust_module = \"crate::sample\";\n" + declarations})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	result, err := mrpc.NewCatalog([]mrpc.File{file})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSourceOptionsPreserveWireIdentityAndDeterminism(t *testing.T) {
	c := catalog(t, "// Node owns its child.\nmessage Node { value int64 = 1; next optional[Node] = 2; }\nservice Tree { Echo(node Node = 1) returns (node Node = 1); }")
	first, err := Generate(c, Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(c, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("non-deterministic output")
	}
	renamed, err := Generate(c, Options{Module: "crate::other", Runtime: "renamed_runtime", Prefix: "Remote"})
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range [][]byte{first, renamed} {
		if !bytes.Contains(output, []byte(c.ContractID)) {
			t.Fatal("source options changed contract identity")
		}
	}
	for _, expected := range []string{"use mini_go::rpc", "/// Node owns its child.", "Option<Box<Node>>", "TreeHandler"} {
		if !bytes.Contains(first, []byte(expected)) {
			t.Fatalf("missing %q", expected)
		}
	}
	if !bytes.Contains(renamed, []byte("use renamed_runtime::rpc")) {
		t.Fatal("runtime path not applied")
	}
	c.ContractID = "mutated"
	if _, err := Generate(c, Options{}); err == nil {
		t.Fatal("stale identity accepted")
	}
}

func TestRustNamesRejectInvalidPathsAndConversions(t *testing.T) {
	c := catalog(t, `message Item { value int64 = 1; }`)
	for _, path := range []string{"crate::mod", "crate::r#self", "crate::", "crate::2bad", "_", "crate::super", "foo::super"} {
		if _, err := Generate(c, Options{Module: path}); err == nil {
			t.Errorf("invalid module accepted: %q", path)
		}
	}
	if _, err := Generate(c, Options{Prefix: "bad-"}); err == nil {
		t.Fatal("invalid prefix accepted")
	}
	for _, declarations := range []string{`message FooBar { value int64 = 1; } message foo_bar { value int64 = 1; }`, `service Client { Bind() returns (); }`, `message Item { HTTPCode int64 = 1; http_code int64 = 2; }`} {
		if _, err := Generate(catalog(t, declarations), Options{}); err == nil || !strings.Contains(err.Error(), "collision") {
			t.Fatalf("collision diagnostic: %v", err)
		}
	}
	keyword := catalog(t, `message Item { type string = 1; }`)
	output, err := Generate(keyword, Options{Module: "crate::r#mod"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output, []byte("r#type")) {
		t.Fatal("keyword field not escaped")
	}
}
