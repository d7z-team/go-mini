package surface

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestMergeReturnsSchemaConflictInBundle(t *testing.T) {
	left := runtime.NewFFISurfaceSchema()
	if err := left.AddConst("demo", "Value", runtime.ConstInt64(1)); err != nil {
		t.Fatal(err)
	}
	right := runtime.NewFFISurfaceSchema()
	if err := right.AddConst("demo", "Value", runtime.ConstInt64(2)); err != nil {
		t.Fatal(err)
	}

	bundle := Merge(New(left, nil), New(right, nil))
	if bundle == nil || bundle.Err == nil {
		t.Fatal("expected schema conflict to be stored on merged bundle")
	}
}
