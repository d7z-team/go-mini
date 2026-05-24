package tests

import (
	"os"
	"strings"
	"testing"
)

func TestFFIGenMigrationSamples(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
	}{
		{
			name: "StdlibSchemaOnlyRegistration",
			path: "../../../../ffilib/iolib/io_ffigen.go",
			patterns: []string{
				"func SurfaceIO(",
				"var ioRoutes = []runtime.FFIRouteDecl{",
				"schema.AddRouteDecls(ioRoutes)",
				"bound.BindSchemaRoutes(schema, bridge)",
			},
		},
		{
			name: "ServiceSchemaOnlyRegistration",
			path: "../../../e2e/canonicaltest/canonical_type_ffigen.go",
			patterns: []string{
				"func SurfaceTestCanonicalService(",
				"var testCanonicalServiceRoutes = []runtime.FFIRouteDecl{",
				"schema.AddRouteDecls(testCanonicalServiceRoutes)",
				"runtime.NewBoundFFISurfaceFromSchema(schema)",
			},
		},
		{
			name: "StructDirectSchemaRegistration",
			path: "../../../e2e/structtest/ffigen.go",
			patterns: []string{
				"schema.AddStruct(",
				"TypeName: \"calc.Calculator\"",
				"bound.BindSchemaRoutes(schema, bridge)",
				"HostRef<calc.Calculator>",
			},
		},
		{
			name: "BusinessServiceSchemaGeneration",
			path: "ordertest/order_ffigen.go",
			patterns: []string{
				"var orderServiceRoutes = []runtime.FFIRouteDecl{",
				"HostRef<order.Order>",
				"TypeName: \"order.Order\", MethodName: \"AddItem\"",
				"schema.AddRouteDecls(orderServiceRoutes)",
			},
		},
		{
			name: "CrossPackageImportGeneration",
			path: "importtest/ffigen.go",
			patterns: []string{
				"\"time\"",
				"var d time.Duration",
				"err := impl.Sleep(ctx, d)",
				"runtime.MustParseRuntimeFuncSigWithModes(\"function(Int64) Error\", runtime.FFIParamIn)",
			},
		},
		{
			name: "GlobalHostRefPackageValues",
			path: "../../../../ffilib/encoding/base64lib/base64_ffigen.go",
			patterns: []string{
				"func SurfaceGlobals() *surface.Bundle",
				"schema.AddValue(\"encoding/base64\", \"StdEncoding\"",
				"runtime.StaticHostRefProvider{ElementType: runtime.TypeSpec(\"encoding/base64.Encoding\")",
				"Value: StdEncoding",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			code := string(content)
			for _, pattern := range tt.patterns {
				if !strings.Contains(code, pattern) {
					t.Fatalf("expected %s to contain %q", tt.path, pattern)
				}
			}
			disallowed := []string{
				"legacyRegistrar",
				"func Register",
				"RegisterFFISchema(",
				"RegisterStructSpec(",
				"RegisterFFI(",
				"registrar.RegisterFFISchema(",
				"_Bridge",
				"_FFI_Schemas",
				"MethodID_",
				"Value: &Encoding{Enc: base64.StdEncoding}",
			}
			for _, pattern := range disallowed {
				if strings.Contains(code, pattern) {
					t.Fatalf("expected %s to drop legacy marker %q", tt.path, pattern)
				}
			}
		})
	}
}

func TestFFIGenHostRefIsOpaqueHandleContract(t *testing.T) {
	content, err := os.ReadFile("../../../e2e/structtest/ffigen.go")
	if err != nil {
		t.Fatalf("read generated struct sample: %v", err)
	}
	code := string(content)

	required := []string{
		"HostRef<T> crosses the FFI boundary as an opaque handle ID.",
		"HostRef<T> is restored from the opaque handle ID written on the FFI wire.",
		"registry.RegisterTyped(",
		"ReadUvarint()",
	}
	for _, pattern := range required {
		if !strings.Contains(code, pattern) {
			t.Fatalf("generated struct sample missing pointer/handle contract marker %q", pattern)
		}
	}
}

func TestFFIGenStructMethodsVariadicHostRefReturn(t *testing.T) {
	content, err := os.ReadFile("dummy_ffigen_test.go")
	if err != nil {
		t.Fatalf("read generated dummy sample: %v", err)
	}
	code := string(content)

	required := []string{
		"case methodIDPageGetByPlaceholder:",
		"r0 := __recv.GetByPlaceholder(text, exact...)",
		"if r0 == nil {",
		"resBuf.WriteUvarint(0)",
		"resBuf.WriteUvarint(uint64(registry.RegisterTyped(r0, \"Selector\")))",
		"return resBuf.Bytes(), nil",
	}
	for _, pattern := range required {
		if !strings.Contains(code, pattern) {
			t.Fatalf("generated struct-method sample missing %q", pattern)
		}
	}
	if count := strings.Count(code, "var Selector_FFI_StructSchema = "); count != 1 {
		t.Fatalf("expected Selector_FFI_StructSchema to be emitted once, got %d", count)
	}
}

func TestFFIGenDoesNotLeakSymbolsFromOtherGeneratedFiles(t *testing.T) {
	content, err := os.ReadFile("unnamed_params_ffigen.go")
	if err != nil {
		t.Fatalf("read generated unnamed params sample: %v", err)
	}
	code := string(content)

	disallowed := []string{
		"methodIDAdvancedFFI",
		"methodIDContextMock",
		"methodIDMapTest",
		"methodIDMockOS",
		"methodIDPageGetByPlaceholder",
		"methodIDVariadicPointerMethods",
	}
	for _, pattern := range disallowed {
		if strings.Contains(code, pattern) {
			t.Fatalf("generated file leaked symbols from sibling ffigen output: %q", pattern)
		}
	}
}

func TestFFIGenProxyIsOptIn(t *testing.T) {
	tests := []struct {
		path      string
		wantProxy bool
	}{
		{path: "array_ref_ffigen_test.go", wantProxy: true},
		{path: "dummy_ffigen_test.go"},
		{path: "ffi_struct_ffigen_test.go"},
		{path: "ordertest/order_ffigen.go"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			content, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			hasProxy := strings.Contains(string(content), "Proxy struct {")
			if hasProxy != tt.wantProxy {
				t.Fatalf("proxy generation mismatch for %s: got %v want %v", tt.path, hasProxy, tt.wantProxy)
			}
		})
	}
}
