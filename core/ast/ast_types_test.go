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
		{"Empty", "", true, false, false, false, false, false, false},
		{"Void", "Void", false, true, false, false, false, false, false},
		{"Basic Int", "Int", false, false, false, false, false, false, false},
		{"Basic String", "String", false, false, false, false, false, false, false},
		{"Pointer", "Ptr<Int>", false, false, true, false, false, false, false},
		{"Array", "Array<Int>", false, false, false, true, false, false, false},
		{"Map", "Map<String, Int>", false, false, false, false, true, false, false},
		{"Tuple", "tuple(Int, String)", false, false, false, false, false, true, false},
		{"Function", "function(Int a, String b) Int", false, false, false, false, false, false, true},
		{"Nested Array", "Array<Array<Int>>", false, false, false, true, false, false, false},
		{"Map with Array Value", "Map<String, Array<Int>>", false, false, false, false, true, false, false},
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
			if _, got := tt.typeStr.ReadFunc(); got != tt.isFunc {
				t.Errorf("ReadFunc() returned ok = %v, want %v", got, tt.isFunc)
			}
		})
	}
}

func TestGoMiniType_ArrayOperations(t *testing.T) {
	tests := []struct {
		name         string
		typeStr      GoMiniType
		isArray      bool
		expectedElem GoMiniType
		shouldFail   bool
	}{
		{"Simple Array", "Array<Int>", true, "Int", false},
		{"Nested Array", "Array<Array<String>>", true, "Array<String>", false},
		{"Array with Function", "Array<function() Int>", true, "function() Int", false},
		{"Not Array", "Int", false, "", true},
		{"Bad Array Format", "Array<Int", false, "", true},
		{"Empty Array", "Array<>", true, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.typeStr.IsArray(); got != tt.isArray {
				t.Errorf("IsArray() = %v, want %v", got, tt.isArray)
			}

			elem, ok := tt.typeStr.ReadArrayItemType()
			if ok && tt.shouldFail {
				t.Errorf("ReadArrayItemType() should have failed but succeeded")
			}
			if !ok && !tt.shouldFail && tt.isArray {
				t.Errorf("ReadArrayItemType() should have succeeded but failed")
			}
			if ok && elem != tt.expectedElem {
				t.Errorf("ReadArrayItemType() = %v, want %v", elem, tt.expectedElem)
			}
		})
	}
}

func TestCreateArrayType(t *testing.T) {
	tests := []struct {
		name     string
		elemType GoMiniType
		expected GoMiniType
	}{
		{"Int Array", "Int", "Array<Int>"},
		{"String Array", "String", "Array<String>"},
		{"Pointer Array", "Ptr<Int>", "Array<Ptr<Int>>"},
		{"Function Array", "function() Int", "Array<function() Int>"},
		{"Tuple Array", "tuple(Int, String)", "Array<tuple(Int, String)>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateArrayType(tt.elemType)
			if got != tt.expected {
				t.Errorf("CreateArrayType() = %v, want %v", got, tt.expected)
			}
			if !got.IsArray() {
				t.Errorf("Created type should be an array")
			}
		})
	}
}

func TestGoMiniType_PointerOperations(t *testing.T) {
	tests := []struct {
		name         string
		typeStr      GoMiniType
		isPtr        bool
		expectedElem GoMiniType
		shouldFail   bool
	}{
		{"Simple Pointer", "Ptr<Int>", true, "Int", false},
		{"Pointer to Array", "Ptr<Array<Int>>", true, "Array<Int>", false},
		{"Pointer to Map", "Ptr<Map<String, Int>>", true, "Map<String, Int>", false},
		{"Not Pointer", "Int", false, "", true},
		{"Bad Pointer Format", "Ptr<Int", false, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.typeStr.IsPtr(); got != tt.isPtr {
				t.Errorf("IsPtr() = %v, want %v", got, tt.isPtr)
			}

			elem, ok := tt.typeStr.GetPtrElementType()
			if ok && tt.shouldFail {
				t.Errorf("GetPtrElementType() should have failed but succeeded")
			}
			if !ok && !tt.shouldFail && tt.isPtr {
				t.Errorf("GetPtrElementType() should have succeeded but failed")
			}
			if ok && elem != tt.expectedElem {
				t.Errorf("GetPtrElementType() = %v, want %v", elem, tt.expectedElem)
			}
		})
	}
}

func TestCreatePtrType(t *testing.T) {
	tests := []struct {
		name     string
		elemType GoMiniType
		expected GoMiniType
	}{
		{"Int Pointer", "Int", "Ptr<Int>"},
		{"Array Pointer", "Array<Int>", "Ptr<Array<Int>>"},
		{"Function Pointer", "function() Int", "Ptr<function() Int>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.elemType.ToPtr()
			if got != tt.expected {
				t.Errorf("CreatePtrType() = %v, want %v", got, tt.expected)
			}
			if !got.IsPtr() {
				t.Errorf("Created type should be a pointer")
			}
		})
	}
}

func TestGoMiniType_MapOperations(t *testing.T) {
	tests := []struct {
		name          string
		typeStr       GoMiniType
		isMap         bool
		expectedKey   GoMiniType
		expectedValue GoMiniType
		shouldFail    bool
	}{
		{"Simple Map", "Map<String, Int>", true, "String", "Int", false},
		{"Map with Complex Key", "Map<Ptr<Int>, String>", true, "Ptr<Int>", "String", false},
		{"Map with Array Value", "Map<Int, Array<String>>", true, "Int", "Array<String>", false},
		{"Map with Function Value", "Map<String, function() Int>", true, "String", "function() Int", false},
		{"Not Map", "Int", false, "", "", true},
		{"Bad Map Format 1", "Map<Int", false, "", "", true},
		{"Bad Map Format 2", "Map<Int>", true, "Int", "", true}, // Missing comma
		{"Map with Extra Spaces", "Map< Int , String >", true, "Int", "String", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.typeStr.IsMap(); got != tt.isMap {
				t.Errorf("IsMap() = %v, want %v", got, tt.isMap)
			}

			key, value, ok := tt.typeStr.GetMapKeyValueTypes()
			if ok && tt.shouldFail {
				t.Errorf("GetMapKeyValueTypes() should have failed but succeeded")
			}
			if !ok && !tt.shouldFail && tt.isMap {
				t.Errorf("GetMapKeyValueTypes() should have succeeded but failed")
			}
			if ok && key != tt.expectedKey {
				t.Errorf("GetMapKeyValueTypes() key = %v, want %v", key, tt.expectedKey)
			}
			if ok && value != tt.expectedValue {
				t.Errorf("GetMapKeyValueTypes() value = %v, want %v", value, tt.expectedValue)
			}
		})
	}
}

func TestCreateMapType(t *testing.T) {
	tests := []struct {
		name      string
		keyType   GoMiniType
		valueType GoMiniType
		expected  GoMiniType
	}{
		{"Simple Map", "String", "Int", "Map<String, Int>"},
		{"Map with Pointer Value", "Int", "Ptr<String>", "Map<Int, Ptr<String>>"},
		{"Map with Array Key", "Array<Int>", "String", "Map<Array<Int>, String>"},
		{"Map with Function", "String", "function() Int", "Map<String, function() Int>"},
		{"Nested Map", "String", "Map<Int, Bool>", "Map<String, Map<Int, Bool>>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateMapType(tt.keyType, tt.valueType)
			if got != tt.expected {
				t.Errorf("CreateMapType() = %v, want %v", got, tt.expected)
			}
			if !got.IsMap() {
				t.Errorf("Created type should be a map")
			}

			// Verify the created map can be parsed back
			if got.IsMap() {
				key, value, ok := got.GetMapKeyValueTypes()
				if !ok {
					t.Errorf("Failed to parse created map type")
				}
				if key != tt.keyType {
					t.Errorf("Parsed key type = %v, want %v", key, tt.keyType)
				}
				if value != tt.valueType {
					t.Errorf("Parsed value type = %v, want %v", value, tt.valueType)
				}
			}
		})
	}
}

func TestGoMiniType_FunctionParsing(t *testing.T) {
	tests := []struct {
		name           string
		typeStr        GoMiniType
		isFunc         bool
		expectedParams []FunctionParam
		expectedReturn GoMiniType
	}{
		{
			"No params, no return",
			"function()",
			true,
			nil,
			"Void",
		},
		{
			"No params with return",
			"function() Int",
			true,
			nil,
			"Int",
		},
		{
			"Single param",
			"function(Int a) String",
			true,
			[]FunctionParam{{Name: "a", Type: "Int"}},
			"String",
		},
		{
			"Multiple params",
			"function(Int a, String b, Bool c) Int",
			true,
			[]FunctionParam{
				{Name: "a", Type: "Int"},
				{Name: "b", Type: "String"},
				{Name: "c", Type: "Bool"},
			},
			"Int",
		},
		{
			"Anonymous params",
			"function(Int, String) Bool",
			true,
			[]FunctionParam{
				{Name: "", Type: "Int"},
				{Name: "", Type: "String"},
			},
			"Bool",
		},
		{
			"Mixed params",
			"function(Int a, String, Bool c) Void",
			true,
			[]FunctionParam{
				{Name: "a", Type: "Int"},
				{Name: "", Type: "String"},
				{Name: "c", Type: "Bool"},
			},
			"Void",
		},
		{
			"Complex param types",
			"function(Array<Int> a, Ptr<String> b) Map<Int, String>",
			true,
			[]FunctionParam{
				{Name: "a", Type: "Array<Int>"},
				{Name: "b", Type: "Ptr<String>"},
			},
			"Map<Int, String>",
		},
		{
			"Nested function param",
			"function(function(Int) String callback) Int",
			true,
			[]FunctionParam{
				{Name: "callback", Type: "function(Int) String"},
			},
			"Int",
		},
		{
			"Tuple return",
			"function(Int a) tuple(Int, String)",
			true,
			[]FunctionParam{{Name: "a", Type: "Int"}},
			"tuple(Int, String)",
		},
		{
			"Multiple returns in parens",
			"function() (Int, String)",
			true,
			nil,
			"tuple(Int, String)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn, ok := tt.typeStr.ReadFunc()
			if ok != tt.isFunc {
				t.Errorf("ReadFunc() ok = %v, want %v", ok, tt.isFunc)
				return
			}

			if !ok {
				return
			}

			// Check return type
			if fn.Return != tt.expectedReturn {
				t.Errorf("Return type = %v, want %v", fn.Return, tt.expectedReturn)
			}

			// Check parameters
			if len(fn.Params) != len(tt.expectedParams) {
				t.Errorf("Param count = %v, want %v", len(fn.Params), len(tt.expectedParams))
				return
			}

			for i, param := range fn.Params {
				expected := tt.expectedParams[i]
				if param.Name != expected.Name {
					t.Errorf("Param %d name = %v, want %v", i, param.Name, expected.Name)
				}
				if param.Type != expected.Type {
					t.Errorf("Param %d type = %v, want %v", i, param.Type, expected.Type)
				}
			}
		})
	}
}

func TestGoMiniType_TupleParsing(t *testing.T) {
	tests := []struct {
		name          string
		typeStr       GoMiniType
		isTuple       bool
		expectedTypes []GoMiniType
	}{
		{
			"Simple tuple",
			"tuple(Int, String, Bool)",
			true,
			[]GoMiniType{"Int", "String", "Bool"},
		},
		{
			"Single element tuple",
			"tuple(Int)",
			true,
			[]GoMiniType{"Int"},
		},
		{
			"Empty tuple",
			"tuple()",
			true,
			nil,
		},
		{
			"Nested tuple",
			"tuple(Int, tuple(String, Bool), Array<Int>)",
			true,
			[]GoMiniType{"Int", "tuple(String, Bool)", "Array<Int>"},
		},
		{
			"Tuple with complex types",
			"tuple(Array<Int>, Map<String, Int>, function() Int)",
			true,
			[]GoMiniType{"Array<Int>", "Map<String, Int>", "function() Int"},
		},
		{
			"Not a tuple",
			"Int",
			false,
			[]GoMiniType{},
		},
		{
			"Array acts like single-element tuple",
			"Array<Int>",
			false,
			[]GoMiniType{"Int"}, // Special case for arrays
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.typeStr.IsTuple(); got != tt.isTuple {
				t.Errorf("IsTuple() = %v, want %v", got, tt.isTuple)
			}

			types, isTuple := tt.typeStr.ReadTuple()

			// For arrays, ReadTuple returns false for the tuple flag
			if tt.typeStr.IsArray() {
				if isTuple {
					t.Errorf("ReadTuple() should return false for isTuple with arrays")
				}
			} else if isTuple != tt.isTuple {
				t.Errorf("ReadTuple() isTuple = %v, want %v", isTuple, tt.isTuple)
			}

			// Check types
			if len(types) != len(tt.expectedTypes) {
				t.Errorf("Type count = %v, want %v", len(types), len(tt.expectedTypes))
				return
			}

			for i, typ := range types {
				if typ != tt.expectedTypes[i] {
					t.Errorf("Type %d = %v, want %v", i, typ, tt.expectedTypes[i])
				}
			}
		})
	}
}

func TestCreateTupleType(t *testing.T) {
	tests := []struct {
		name     string
		types    []GoMiniType
		expected GoMiniType
	}{
		{"Empty", []GoMiniType{}, "Void"},
		{"Single", []GoMiniType{"Int"}, "Int"},
		{"Two types", []GoMiniType{"Int", "String"}, "tuple(Int, String)"},
		{"Three types", []GoMiniType{"Int", "String", "Bool"}, "tuple(Int, String, Bool)"},
		{"With complex types", []GoMiniType{"Array<Int>", "Map<String, Int>"}, "tuple(Array<Int>, Map<String, Int>)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateTupleType(tt.types...)
			if got != tt.expected {
				t.Errorf("CreateTupleType() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGoMiniType_Equals(t *testing.T) {
	tests := []struct {
		name     string
		type1    GoMiniType
		type2    GoMiniType
		expected bool
	}{
		// Basic types
		{"Same basic", "Int", "Int", true},
		{"Different basic", "Int", "String", false},

		// Pointer types
		{"Same pointer", "Ptr<Int>", "Ptr<Int>", true},
		{"Different pointer content", "Ptr<Int>", "Ptr<String>", false},
		{"Pointer vs non-pointer", "Ptr<Int>", "Int", false},

		// Array types
		{"Same array", "Array<Int>", "Array<Int>", true},
		{"Different array content", "Array<Int>", "Array<String>", false},

		// Map types
		{"Same map", "Map<Int, String>", "Map<Int, String>", true},
		{"Different map key", "Map<Int, String>", "Map<String, String>", false},
		{"Different map value", "Map<Int, String>", "Map<Int, Bool>", false},

		// Function types
		{"Same function", "function(Int) String", "function(Int) String", true},
		{"Different function params", "function(Int) String", "function(String) String", false},
		{"Different function return", "function(Int) String", "function(Int) Bool", false},
		{"Named vs unnamed params", "function(Int a) String", "function(Int) String", true}, // Should be equal

		// Tuple types
		{"Same tuple", "tuple(Int, String)", "tuple(Int, String)", true},
		{"Different tuple order", "tuple(Int, String)", "tuple(String, Int)", false},
		{"Single tuple vs non-tuple", "tuple(Int)", "Int", false},

		// Complex nested types
		{"Nested array", "Array<Array<Int>>", "Array<Array<Int>>", true},
		{"Map in function", "function(Map<Int, String>) Bool", "function(Map<Int, String>) Bool", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.type1.Equals(tt.type2)
			if result != tt.expected {
				t.Errorf("Equals() = %v, want %v for %v == %v", result, tt.expected, tt.type1, tt.type2)
			}

			// Test symmetry
			reverseResult := tt.type2.Equals(tt.type1)
			if reverseResult != result {
				t.Errorf("Equals() is not symmetric: %v == %v gives %v, but %v == %v gives %v",
					tt.type1, tt.type2, result, tt.type2, tt.type1, reverseResult)
			}
		})
	}
}
