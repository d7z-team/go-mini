package surface

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestMergeReturnsSchemaConflictInBundle(t *testing.T) {
	left := runtime.NewFFISurfaceSchema()
	left.AddConst("demo", "Value", runtime.ConstInt64(1))
	right := runtime.NewFFISurfaceSchema()
	right.AddConst("demo", "Value", runtime.ConstInt64(2))

	bundle := Merge(New(left, nil), New(right, nil))
	if bundle == nil || bundle.Err == nil {
		t.Fatal("expected schema conflict to be stored on merged bundle")
	}
}
