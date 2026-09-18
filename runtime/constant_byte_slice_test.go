package runtime

import (
	"encoding/json"
	"testing"
)

func TestDecodeByteSliceConstantPreservesBinaryData(t *testing.T) {
	module := &moduleInstance{}
	value, err := decodeConstantAs(module, runtimeTypeFromText("Slice<Uint8>"), json.RawMessage(`"ALj/Cg=="`))
	if err != nil {
		t.Fatal(err)
	}
	slice, ok := value.Data.(*vmSlice)
	if !ok || slice == nil || !slice.ByteBacked {
		t.Fatalf("decoded constant = %#v", value)
	}
	if got := slice.ByteBacking[slice.Start : slice.Start+slice.Len]; string(got) != string([]byte{0, 0xb8, 0xff, '\n'}) {
		t.Fatalf("decoded bytes = %v", got)
	}
}
