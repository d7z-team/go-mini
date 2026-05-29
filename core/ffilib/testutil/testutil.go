package testutil

import (
	"context"
	"errors"
	"fmt"
	"sort"
	gostrconv "strconv"
	"strings"
	"sync"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

type OutputKind string

const (
	OutputString OutputKind = "string"
	OutputBool   OutputKind = "bool"
	OutputInt    OutputKind = "int"
	OutputFloat  OutputKind = "float"
	OutputBytes  OutputKind = "bytes"
)

type Harness struct {
	Executor *engine.MiniExecutor
	output   *recorder
}

type Option func(*config)

type config struct {
	surface *surface.Bundle
}

const caseTimeout = 30 * time.Second

func WithSurface(bundle *surface.Bundle) Option {
	return func(cfg *config) {
		cfg.surface = bundle
	}
}

type ExprCase struct {
	Name           string
	Imports        []string
	Expr           string
	Output         OutputKind
	Want           string
	WantCompileErr string
	WantRunErr     string
	Covers         []string
}

type BlockCase struct {
	Name           string
	Imports        []string
	Decls          string
	Body           string
	Want           string
	WantCompileErr string
	WantRunErr     string
	Covers         []string
}

type Case struct {
	Name           string
	Imports        []string
	Decls          string
	Body           string
	Expr           string
	Output         OutputKind
	Want           string
	WantCompileErr string
	WantRunErr     string
	Covers         []string
}

type MethodSchema struct {
	Name    string
	Methods []string
}

func NewExecutor(tb testing.TB, opts ...Option) *engine.MiniExecutor {
	tb.Helper()
	return NewHarness(tb, opts...).Executor
}

func NewHarness(tb testing.TB, opts ...Option) *Harness {
	tb.Helper()
	cfg := config{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	executor := engine.MustNewMiniExecutor()
	if cfg.surface != nil {
		if err := executor.UseSurface(cfg.surface); err != nil {
			tb.Fatal(err)
		}
	}
	out := &recorder{}
	if err := executor.UseSurface(testSurface(out)); err != nil {
		tb.Fatal(err)
	}
	return &Harness{Executor: executor, output: out}
}

func ExpectExpr(tb testing.TB, tc ExprCase, opts ...Option) {
	tb.Helper()
	runCase(tb, Case{
		Name:           tc.Name,
		Imports:        tc.Imports,
		Expr:           tc.Expr,
		Output:         tc.Output,
		Want:           tc.Want,
		WantCompileErr: tc.WantCompileErr,
		WantRunErr:     tc.WantRunErr,
		Covers:         tc.Covers,
	}, opts...)
}

func ExpectBlock(tb testing.TB, tc BlockCase, opts ...Option) {
	tb.Helper()
	runCase(tb, Case{
		Name:           tc.Name,
		Imports:        tc.Imports,
		Decls:          tc.Decls,
		Body:           tc.Body,
		Want:           tc.Want,
		WantCompileErr: tc.WantCompileErr,
		WantRunErr:     tc.WantRunErr,
		Covers:         tc.Covers,
	}, opts...)
}

func ExpectCompileError(tb testing.TB, tc BlockCase) {
	tb.Helper()
	tc.WantCompileErr = strings.TrimSpace(tc.WantCompileErr)
	if tc.WantCompileErr == "" {
		tc.WantCompileErr = "<any>"
	}
	ExpectBlock(tb, tc)
}

func RunCases(t *testing.T, schemas []MethodSchema, cases []Case, opts ...Option) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		name := tc.Name
		if name == "" {
			name = "case"
		}
		t.Run(name, func(t *testing.T) {
			runCase(t, tc, opts...)
		})
	}
	verifyCoverage(t, schemas, cases)
}

func Schema(name string, methods ...string) MethodSchema {
	res := MethodSchema{Name: name, Methods: append([]string(nil), methods...)}
	sort.Strings(res.Methods)
	return res
}

func SurfaceFFISchema(name string, bundle *surface.Bundle) MethodSchema {
	if bundle == nil || bundle.Schema == nil {
		return Schema(name)
	}
	methods := make([]string, 0)
	if pkg := bundle.Schema.Packages[name]; pkg != nil {
		for memberName, member := range pkg.Members {
			if member != nil && member.Kind == runtime.FFIMemberFunc {
				methods = append(methods, memberName)
			}
		}
	}
	if typ := bundle.Schema.Types[name]; typ != nil {
		for methodName := range typ.Methods {
			methods = append(methods, methodName)
		}
	}
	return Schema(name, methods...)
}

func runCase(tb testing.TB, tc Case, opts ...Option) {
	tb.Helper()
	h := NewHarness(tb, opts...)
	code := buildProgram(tc)
	prog, err := h.Executor.NewRuntimeByGoCode(code)
	if tc.WantCompileErr != "" {
		if err == nil {
			tb.Fatalf("expected compile error containing %q, got nil", tc.WantCompileErr)
		}
		if tc.WantCompileErr != "<any>" && !strings.Contains(err.Error(), tc.WantCompileErr) {
			tb.Fatalf("compile error %q does not contain %q", err.Error(), tc.WantCompileErr)
		}
		return
	}
	if err != nil {
		tb.Fatalf("compile failed: %v\n%s", err, code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), caseTimeout)
	defer cancel()
	execErr := prog.Execute(ctx)
	if tc.WantRunErr != "" {
		if execErr == nil {
			tb.Fatalf("expected run error containing %q, got nil", tc.WantRunErr)
		}
		if tc.WantRunErr != "<any>" && !strings.Contains(execErr.Error(), tc.WantRunErr) {
			tb.Fatalf("run error %q does not contain %q", execErr.Error(), tc.WantRunErr)
		}
		return
	}
	if execErr != nil {
		tb.Fatalf("execute failed: %v\n%s", execErr, code)
	}
	if !h.output.Done() {
		tb.Fatalf("test script finished without calling test.Done()\n%s", code)
	}
	if got := h.output.String(); got != tc.Want {
		tb.Fatalf("unexpected output %q, want %q\n%s", got, tc.Want, code)
	}
}

func buildProgram(tc Case) string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"test\"\n")
	for _, imp := range tc.Imports {
		b.WriteString("\t")
		b.WriteString(formatImport(imp))
		b.WriteByte('\n')
	}
	b.WriteString(")\n\n")
	if strings.TrimSpace(tc.Decls) != "" {
		b.WriteString(tc.Decls)
		if !strings.HasSuffix(tc.Decls, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString("func main() {\n")
	if strings.TrimSpace(tc.Expr) != "" {
		fmt.Fprintf(&b, "\t%s(%s)\n", outputFunc(tc.Output), tc.Expr)
	} else {
		b.WriteString(tc.Body)
		if !strings.HasSuffix(tc.Body, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\ttest.Done()\n")
	b.WriteString("}\n")
	return b.String()
}

func formatImport(imp string) string {
	if strings.Contains(imp, "\"") {
		return imp
	}
	return gostrconv.Quote(imp)
}

func outputFunc(kind OutputKind) string {
	switch kind {
	case "", OutputString:
		return "test.Out"
	case OutputBool:
		return "test.OutBool"
	case OutputInt:
		return "test.OutInt"
	case OutputFloat:
		return "test.OutFloat"
	case OutputBytes:
		return "test.OutBytes"
	default:
		panic(fmt.Sprintf("unknown output kind %q", kind))
	}
}

func verifyCoverage(tb testing.TB, schemas []MethodSchema, cases []Case) {
	tb.Helper()
	expected := make(map[string]string)
	for _, schema := range schemas {
		for _, method := range schema.Methods {
			expected[method] = schema.Name
		}
	}
	if len(expected) == 0 {
		return
	}
	covered := make(map[string]bool)
	for _, tc := range cases {
		for _, method := range tc.Covers {
			if _, ok := expected[method]; !ok {
				tb.Fatalf("case %s covers unknown method %s", tc.Name, method)
			}
			covered[method] = true
		}
	}
	var missing []string
	for method, schemaName := range expected {
		if !covered[method] {
			missing = append(missing, schemaName+"."+method)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		tb.Fatalf("missing FFI test coverage: %s", strings.Join(missing, ", "))
	}
}

type recorder struct {
	mu   sync.Mutex
	sb   strings.Builder
	done bool
}

func (r *recorder) Write(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sb.WriteString(s)
}

func (r *recorder) Done() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

func (r *recorder) MarkDone() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = true
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sb.String()
}

type testBridge struct {
	output *recorder
}

const (
	methodOut uint32 = iota + 1
	methodOutLine
	methodOutBool
	methodOutInt
	methodOutFloat
	methodOutBytes
	methodDone
)

func testSurface(output *recorder) *surface.Bundle {
	routes := []struct {
		member   string
		methodID uint32
		sig      *runtime.RuntimeFuncSig
	}{
		{"Out", methodOut, runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecString)},
		{"OutLine", methodOutLine, runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecString)},
		{"OutBool", methodOutBool, runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecBool)},
		{"OutInt", methodOutInt, runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecInt64)},
		{"OutFloat", methodOutFloat, runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecFloat64)},
		{"OutBytes", methodOutBytes, runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecBytes)},
		{"Done", methodDone, runtime.MustRuntimeFuncSig(runtime.SpecVoid, false)},
	}
	schema := runtime.NewFFISurfaceSchema()
	for _, route := range routes {
		if err := schema.AddFunc("test", route.member, "test."+route.member, route.methodID, route.sig, ""); err != nil {
			return &surface.Bundle{Err: err}
		}
	}
	return surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		bridge := &testBridge{output: output}
		bound := runtime.NewBoundFFISurface(schema)
		for _, route := range routes {
			bound.AddRoute("test", route.member, runtime.FFIRoute{
				Name:     "test." + route.member,
				Bridge:   bridge,
				MethodID: route.methodID,
				FuncSig:  route.sig,
			})
		}
		return bound, nil
	})
}

func (b *testBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req == nil {
		return nil, errors.New("test bridge: missing request")
	}
	reader := ffigo.NewReader(req.Args)
	switch req.MethodID {
	case methodOut:
		v, err := reader.ReadString()
		if err != nil {
			return nil, err
		}
		b.output.Write(v)
	case methodOutLine:
		v, err := reader.ReadString()
		if err != nil {
			return nil, err
		}
		b.output.Write(v)
		b.output.Write("\n")
	case methodOutBool:
		v, err := reader.ReadBool()
		if err != nil {
			return nil, err
		}
		b.output.Write(gostrconv.FormatBool(v))
	case methodOutInt:
		v, err := reader.ReadVarint()
		if err != nil {
			return nil, err
		}
		b.output.Write(gostrconv.FormatInt(v, 10))
	case methodOutFloat:
		v, err := reader.ReadFloat64()
		if err != nil {
			return nil, err
		}
		b.output.Write(gostrconv.FormatFloat(v, 'f', -1, 64))
	case methodOutBytes:
		v, err := reader.ReadBytes()
		if err != nil {
			return nil, err
		}
		b.output.Write(string(v))
	case methodDone:
		b.output.MarkDone()
	default:
		return nil, fmt.Errorf("test bridge: unknown method ID %d", req.MethodID)
	}
	return nil, nil
}

func (b *testBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return b.Call(ctx, req)
}

func (b *testBridge) DestroyHandle(uint32) error {
	return nil
}
