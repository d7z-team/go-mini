package ast

import (
	"testing"
)

func TestGoMiniType_BasicTypes(t *testing.T) {
	tests := []struct {
		name    string
		typeStr GoMiniType
		isEmpty bool
		isVoid  bool
		isPtr   bool
		isArray bool
		isMap   bool
		isTuple bool
		isFunc  bool
	}{
		{"Empty", "", true, true, false, false, false, false, false},
		{"Void", "Void", false, true, false, false, false, false, false},
		{"Basic Int", "Int64", false, false, false, false, false, false, false},
		{"Basic String", "String", false, false, false, false, false, false, false},
		{"Pointer", "Ptr<Int64>", false, false, true, false, false, false, false},
		{"Array", "Array<Int64>", false, false, false, true, false, false, false},
		{"Map", "Map<String, Int64>", false, false, false, false, true, false, false},
		{"Tuple", "tuple(Int64, String)", false, false, false, false, false, true, false},
		{"Function", "function(Int64 a, String b) Int64", false, false, false, false, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.typeStr.IsEmpty(); got != tt.isEmpty {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.isEmpty)
			}
			if got := tt.typeStr.IsVoid(); got != tt.isVoid {
				t.Errorf("IsVoid() = %v, want %v", got, tt.isVoid)
			}
			if got := tt.typeStr.IsPtr(); got != tt.isPtr {
				t.Errorf("IsPtr() = %v, want %v", got, tt.isPtr)
			}
			if got := tt.typeStr.IsArray(); got != tt.isArray {
				t.Errorf("IsArray() = %v, want %v", got, tt.isArray)
			}
			if got := tt.typeStr.IsMap(); got != tt.isMap {
				t.Errorf("IsMap() = %v, want %v", got, tt.isMap)
			}
			if got := tt.typeStr.IsTuple(); got != tt.isTuple {
				t.Errorf("IsTuple() = %v, want %v", got, tt.isTuple)
			}
			_, isFunc := tt.typeStr.ReadFunc()
			if isFunc != tt.isFunc {
				t.Errorf("ReadFunc() ok = %v, want %v", isFunc, tt.isFunc)
			}
		})
	}
}

func TestGoMiniType_ArrayOperations(t *testing.T) {
	t.Run("Simple Array", func(t *testing.T) {
		arr := GoMiniType("Array<Int64>")
		elem, ok := arr.ReadArrayItemType()
		if !ok || elem != "Int64" {
			t.Errorf("ReadArrayItemType() = %v, %v, want Int64, true", elem, ok)
		}
	})
}

func TestGoMiniType_FunctionParsing(t *testing.T) {
	t.Run("Multiple params", func(t *testing.T) {
		fnType := GoMiniType("function(Int64 a, String b) Int64")
		fn, ok := fnType.ReadFunc()
		if !ok {
			t.Fatal("ReadFunc failed")
		}
		if len(fn.Params) != 2 || fn.Return != "Int64" {
			t.Errorf("Parsed incorrectly: %+v", fn)
		}
	})
}

func TestGoMiniType_Equals(t *testing.T) {
	tests := []struct {
		name  string
		t1    GoMiniType
		t2    GoMiniType
		equal bool
	}{
		{"Same basic", "Int64", "Int64", true},
		{"Different basic", "Int64", "String", false},
		{"Any matches all", "Any", "Int64", true},
		{"Any matches all rev", "Int64", "Any", true},
		{"Same array", "Array<Int64>", "Array<Int64>", true},
		{"Different array", "Array<Int64>", "Array<String>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.t1.Equals(tt.t2); got != tt.equal {
				t.Errorf("Equals() = %v, want %v", got, tt.equal)
			}
		})
	}
}
