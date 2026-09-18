package runtime

import (
	"errors"
	"testing"
)

func TestDynamicNilComparabilityPreservesStaticAndDynamicTypes(t *testing.T) {
	module := &moduleInstance{}
	for _, typ := range []string{"Slice<Int>", "Map<Int, Int>", "function() Void"} {
		t.Run(typ, func(t *testing.T) {
			zero := module.zeroValue(typ)
			boxed, err := module.coerceAssignableValue(zero, "Any")
			if err != nil {
				t.Fatal(err)
			}
			_, err = module.equalValues(boxed, boxed)
			var guest *guestPanic
			if !errors.As(err, &guest) {
				t.Fatalf("dynamic equality = %v", err)
			}
			if equal, err := module.equalValues(boxed, module.zeroValue("Any")); err != nil || equal {
				t.Fatalf("typed nil equals nil interface: %v %v", equal, err)
			}
			if _, err = module.mapKey(boxed); !errors.As(err, &guest) {
				t.Fatalf("dynamic map key = %v", err)
			}
		})
	}
	for _, typ := range []string{"Ptr<Int>", "Any"} {
		boxed, err := module.coerceAssignableValue(module.zeroValue(typ), "Any")
		if err != nil {
			t.Fatal(err)
		}
		if equal, err := module.equalValues(boxed, boxed); err != nil || !equal {
			t.Fatalf("comparable nil %s: %v %v", typ, equal, err)
		}
		if _, err = module.mapKey(boxed); err != nil {
			t.Fatalf("comparable nil key %s: %v", typ, err)
		}
	}
}
