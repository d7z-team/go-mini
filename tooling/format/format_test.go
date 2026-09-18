package format_test

import (
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/tooling/format"
)

func TestSourceFormatsMRPC(t *testing.T) {
	input := `syntax="mrpc/v2"; namespace sample; service api{Get(id int64=1) returns(value string=1,err error=2);}`
	result := format.Source("example/sample", "api.mrpc", input)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("format diagnostics: %#v", result.Diagnostics)
	}
	if !strings.Contains(result.Text, "service api {") || !strings.Contains(result.Text, "\tGet(") {
		t.Fatalf("formatted MRPC:\n%s", result.Text)
	}
}
