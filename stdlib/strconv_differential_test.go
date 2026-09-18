package stdlib_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func TestFloatFormattingMatchesGo1266(t *testing.T) {
	// Golden values were produced by the unmodified internal/strconv algorithms
	// from the go1.26.6 source tag, not by the host toolchain's strconv package.
	data, err := os.ReadFile("testdata/strconv_go1.26.6.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Input     uint64
		Format    string
		Precision int
		Bits      int
		Text      string
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	program := prepareStdlibRunProgram(t, "minigo.test/strconv-oracle", `package main
import "math"
import "strconv"
func Run(input uint64, format string, precision int, bits int) string {
 return strconv.FormatFloat(math.Float64frombits(input), format[0], precision, bits)
}
`)
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, vector := range vectors {
		result, err := instance.Call(context.Background(), "run", minigoruntime.HostUint("Uint64", vector.Input), minigoruntime.HostString(vector.Format), minigoruntime.HostInt("Int", int64(vector.Precision)), minigoruntime.HostInt("Int", int64(vector.Bits)))
		if err != nil {
			t.Fatalf("%#v: %v", vector, err)
		}
		got, ok := result.Values[0].StringValue()
		if !ok || got != vector.Text {
			t.Fatalf("bits %x format %s precision %d size %d: got %q want %q", vector.Input, vector.Format, vector.Precision, vector.Bits, got, vector.Text)
		}
	}
}
