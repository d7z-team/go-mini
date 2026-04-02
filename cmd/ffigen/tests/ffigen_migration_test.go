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
			path: "../../../core/ffilib/iolib/io_ffigen.go",
			patterns: []string{
				"registrar, ok := executor.(interface {",
				"registrar.RegisterFFISchema(",
				"registrar.RegisterStructSchema(",
			},
		},
		{
			name: "ServiceSchemaOnlyRegistration",
			path: "../../../core/e2e/canonicaltest/canonical_type_ffigen.go",
			patterns: []string{
				"registrar.RegisterFFISchema(",
				"panic(\"ffigen: executor does not support schema FFI registration\")",
			},
		},
		{
			name: "StructDirectSchemaRegistration",
			path: "../../../core/e2e/structtest/ffigen.go",
			patterns: []string{
				"RegisterStructSchema(",
				"registrar.RegisterStructSchema(",
				"Ptr<calc.Calculator>",
			},
		},
		{
			name: "BusinessServiceSchemaGeneration",
			path: "../../../cmd/ffigen/tests/ordertest/order_ffigen.go",
			patterns: []string{
				"var OrderService_FFI_Schemas = []struct {",
				"Ptr<order.Order>",
				"registrar.RegisterFFISchema(\"__method_order.Order_AddItem\"",
				"registrar.RegisterFFISchema(",
			},
		},
		{
			name: "ReverseProxyCompat",
			path: "../../../cmd/ffigen/tests/reverse_ffigen_test.go",
			patterns: []string{
				"type ScriptCalculator_ReverseProxy struct {",
				"InvokeCallable(",
				"if err != nil {",
				"return 0, err",
			},
		},
		{
			name: "CrossPackageImportGeneration",
			path: "../../../cmd/ffigen/tests/importtest/ffigen.go",
			patterns: []string{
				"\"time\"",
				"Sleep(ctx context.Context, d time.Duration) error",
				"runtime.MustParseRuntimeFuncSig(ast.GoMiniType(\"function(Int64) Error\"))",
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
			disallowed := []string{"legacyRegistrar", "RegisterStructSpec(", "RegisterFFI("}
			for _, pattern := range disallowed {
				if strings.Contains(code, pattern) {
					t.Fatalf("expected %s to drop legacy marker %q", tt.path, pattern)
				}
			}
		})
	}
}

func TestFFIGenPtrIsOpaqueHandleContract(t *testing.T) {
	content, err := os.ReadFile("../../../core/e2e/structtest/ffigen.go")
	if err != nil {
		t.Fatalf("read generated struct sample: %v", err)
	}
	code := string(content)

	required := []string{
		"Ptr<T> crosses the FFI boundary as an opaque handle ID.",
		"Ptr<T> is restored from the opaque handle ID written on the FFI wire.",
		"registry.Register(",
		"ReadUvarint()",
	}
	for _, pattern := range required {
		if !strings.Contains(code, pattern) {
			t.Fatalf("generated struct sample missing pointer/handle contract marker %q", pattern)
		}
	}
}
