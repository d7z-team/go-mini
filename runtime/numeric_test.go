package runtime

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNegativeShiftIsGuestPanic(t *testing.T) {
	_, err := (&moduleInstance{}).evalBinary(operatorShiftLeft, newVMValue("Int", int64(1)), newVMValue("Int", int64(-1)))
	var panicValue *guestPanic
	if !errors.As(err, &panicValue) {
		t.Fatalf("negative shift error = %T %v, want guest panic", err, err)
	}
}

func TestParseConstantComplex128RequiresCanonicalObject(t *testing.T) {
	value, err := parseConstantComplex128(json.RawMessage(`{"real":1.5,"imag":-2}`))
	if err != nil || value != complex(1.5, -2) {
		t.Fatalf("parse canonical complex = %v, %v", value, err)
	}
	for _, raw := range []string{
		`"1+2i"`,
		`{}`,
		`{"real":1}`,
		`{"real":1,"imag":2,"extra":3}`,
		`{"real":"1","imag":2}`,
		`{"real":null,"imag":2}`,
	} {
		if _, err := parseConstantComplex128(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted malformed complex constant %s", raw)
		}
	}
}
