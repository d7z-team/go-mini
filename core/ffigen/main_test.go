package ffigen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRunDirConstantKindFromValue(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "consts.go", `package pkgmode

const (
	LocatorLayerKindLocator = "locator"
	LocatorLayerKindFrame   = "frame"
)
`)
	writeTestFile(t, workspace, "target.go", `package pkgmode

// ffigen:module demo
type DemoModule interface {
	Echo(s string) string
}
`)

	outputDir := filepath.Join(workspace, "gen")
	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)

	// Both constants should be ConstString, not ConstInt64
	for _, name := range []string{"LocatorLayerKindLocator", "LocatorLayerKindFrame"} {
		want := fmt.Sprintf(`schema.AddConst("demo", %q, runtime.ConstString`, name)
		if !strings.Contains(code, want) {
			t.Fatalf("expected ConstString for %s, got:\n%s", name, code)
		}
	}
}

func TestRunDirGeneratesPackageOutput(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "page.go", `package pkgmode

// ffigen:module browser
// ffigen:methods Page
type Page struct {
	TitleText string
}

func (p *Page) Title() string {
	return p.TitleText
}
`)
	writeTestFile(t, workspace, "browser.go", `package pkgmode

// ffigen:module browser
type BrowserModule interface {
	Open() *Page
}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)

	if strings.Count(code, "var browser_Page_FFI_StructSchema = ") != 1 {
		t.Fatalf("expected browser.Page schema to be emitted once, got %d", strings.Count(code, "var browser_Page_FFI_StructSchema = "))
	}
	if !strings.Contains(code, "func SurfaceBrowserModule(") {
		t.Fatalf("expected module target to be generated")
	}
	if !strings.Contains(code, "func SurfacePage(") {
		t.Fatalf("expected owned struct target to be generated")
	}
	if !strings.Contains(code, `TypePackagePath: "browser", TypeMemberName: "Page", MethodName: "Title", RouteName: "browser.Page.Title"`) {
		t.Fatalf("expected owned struct methods to use type method route descriptors")
	}
	if strings.Contains(code, "func RegisterBrowserModule(") || strings.Contains(code, "func RegisterPage(") {
		t.Fatalf("generated code should not expose legacy RegisterXxx helpers")
	}
}

func TestRunUsesOptions(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

const Small byte = 7
const Mark rune = 'A'
const Lower = "hello"
const Score = 42
const Rate = 3.14
const Ready = true
const NamedLower string = "world"

// ffigen:module demo
type DemoModule interface {
	Echo(s string) string
}
`)

	outputPath := filepath.Join(workspace, "demo_ffigen.go")
	err := Run(Options{
		PackageName: "pkgmode",
		Output:      outputPath,
		Args:        []string{filepath.Join(workspace, "api.go")},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	code := readGeneratedCode(t, outputPath)
	if !strings.Contains(code, "func SurfaceDemoModule(") {
		t.Fatalf("expected generated module surface, got:\n%s", code)
	}
	if !strings.Contains(code, `schema.AddConst("demo", "Small", runtime.ConstByte(byte(7)))`) {
		t.Fatalf("expected byte constant schema, got:\n%s", code)
	}
	if !strings.Contains(code, `schema.AddConst("demo", "Mark", runtime.ConstRune(int64('A')))`) {
		t.Fatalf("expected rune constant schema, got:\n%s", code)
	}
	if !strings.Contains(code, `schema.AddConst("demo", "Lower", runtime.ConstString(string("hello")))`) {
		t.Fatalf("expected string constant schema, got:\n%s", code)
	}
	if !strings.Contains(code, `schema.AddConst("demo", "Score", runtime.ConstInt64(int64(42)))`) {
		t.Fatalf("expected int64 constant schema, got:\n%s", code)
	}
	if !strings.Contains(code, `schema.AddConst("demo", "Rate", runtime.ConstFloat64(float64(3.14)))`) {
		t.Fatalf("expected float64 constant schema, got:\n%s", code)
	}
	if !strings.Contains(code, `schema.AddConst("demo", "Ready", runtime.ConstBool(bool(true)))`) {
		t.Fatalf("expected bool constant schema, got:\n%s", code)
	}
	if !strings.Contains(code, `schema.AddConst("demo", "NamedLower", runtime.ConstString(string("world")))`) {
		t.Fatalf("expected typed string constant schema, got:\n%s", code)
	}
}

func TestRunReturnsErrorForInvalidGlobalDirective(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "bad_global.go", `package pkgmode

// ffigen:global demo Singleton
var Singleton = 1
`)

	err := Run(Options{
		PackageName: "pkgmode",
		Output:      filepath.Join(workspace, "bad_global_ffigen.go"),
		Args:        []string{filepath.Join(workspace, "bad_global.go")},
	})
	if err == nil || !strings.Contains(err.Error(), "ffigen:global requires") {
		t.Fatalf("expected invalid global directive error, got %v", err)
	}
}

func TestRunConcurrentUsesIndependentGeneratorState(t *testing.T) {
	workspaceA := makeModuleTempDir(t)
	writeTestFile(t, workspaceA, "api.go", `package pkgmode

// ffigen:module alpha
type AlphaModule interface {
	Echo(s string) string
}
`)
	workspaceB := makeModuleTempDir(t)
	writeTestFile(t, workspaceB, "api.go", `package pkgmode

// ffigen:module beta
type BetaModule interface {
	Ping(n int64) int64
}
`)

	outputA := filepath.Join(workspaceA, "alpha_ffigen.go")
	outputB := filepath.Join(workspaceB, "beta_ffigen.go")
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	type runCase struct {
		workspace string
		output    string
	}
	for _, run := range []runCase{
		{workspace: workspaceA, output: outputA},
		{workspace: workspaceB, output: outputB},
	} {
		run := run
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- Run(Options{
				PackageName: "pkgmode",
				Output:      run.output,
				Args:        []string{filepath.Join(run.workspace, "api.go")},
			})
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent Run failed: %v", err)
		}
	}

	codeA := readGeneratedCode(t, outputA)
	codeB := readGeneratedCode(t, outputB)
	if !strings.Contains(codeA, "func SurfaceAlphaModule(") || strings.Contains(codeA, "SurfaceBetaModule") {
		t.Fatalf("alpha output was contaminated:\n%s", codeA)
	}
	if !strings.Contains(codeB, "func SurfaceBetaModule(") || strings.Contains(codeB, "SurfaceAlphaModule") {
		t.Fatalf("beta output was contaminated:\n%s", codeB)
	}
}

func TestRunDirKeepsVariadicArg(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "selector.go", `package pkgmode

// ffigen:module browser
// ffigen:methods Locator
type Locator struct{}

func (l *Locator) Locator() *Locator {
	return l
}

// ffigen:module browser
type BrowserModule interface {
	All(selectors ...string) *Locator
}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)
	if strings.Contains(code, "....") {
		t.Fatalf("generated code contains invalid variadic member access:\n%s", code)
	}
	if !strings.Contains(code, "r0 := impl.All(selectors...)") {
		t.Fatalf("expected variadic module call to target impl directly")
	}
}

func TestRunRejectsMethodsWithoutExplicitModule(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "page.go", `package pkgmode

// ffigen:methods Page
type Page struct{}

func (p *Page) Title() string {
	return ""
}
`)

	defer func() {
		got := recover()
		if got == nil || !strings.Contains(got.(string), "ffigen:methods Page requires ffigen:module") {
			t.Fatalf("expected explicit module panic for methods target, got %v", got)
		}
	}()

	if err := runDirectoryModeForTest("pkgmode", filepath.Join(workspace, "gen"), workspace); err != nil {
		t.Fatalf("expected methods target rejection to panic before returning error, got %v", err)
	}
}

func TestRunFileModeGeneratesNestedSliceCode(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

// ffigen:module regexp
type RegexpModule interface {
	FindAllStringSubmatch(pattern, s string, n int) ([][]string, error)
	EchoGroups(groups [][]string) ([][]string, error)
}
`)

	outputPath := filepath.Join(workspace, "regexp_ffigen.go")

	if err := runFileModeForTest("pkgmode", outputPath, []string{filepath.Join(workspace, "api.go")}); err != nil {
		t.Fatalf("runFileMode: %v", err)
	}
	code := readGeneratedCode(t, outputPath)
	if strings.Contains(code, "l_v_0[i_v_0]") || strings.Contains(code, "i_v_0[i_v_0]") {
		t.Fatalf("generated code still uses indexed expressions as identifiers:\n%s", code)
	}
	if !strings.Contains(code, "tuple(Array<Array<String>>, Error)") {
		t.Fatalf("expected nested array schema, got:\n%s", code)
	}
}

func TestRunDirInjectsStructReceiver(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "selector.go", `package pkgmode

// ffigen:module cdp
// ffigen:methods CdpSelector
type CdpSelector struct{}

func (o *CdpSelector) DragTo(target *CdpSelector) {}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)
	if !strings.Contains(code, "__recv.DragTo(target)") {
		t.Fatalf("expected generated struct method call to use injected receiver, got:\n%s", code)
	}
	if strings.Contains(code, "target.DragTo()") {
		t.Fatalf("generated code still misuses target as receiver:\n%s", code)
	}
}

func TestRunDirInjectsModuleStructReceiver(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "browser.go", `package pkgmode

// ffigen:module browser
// ffigen:methods Browser
type Browser struct{}

func (o *Browser) AutoPage(url string) *Browser {
	return o
}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)
	if !strings.Contains(code, "r0 := __recv.AutoPage(url)") {
		t.Fatalf("expected generated module-qualified struct method to use injected receiver, got:\n%s", code)
	}
	if strings.Contains(code, "r0 := impl.AutoPage(url)") {
		t.Fatalf("generated code still routes module-qualified struct method through impl:\n%s", code)
	}
}

func TestRunDirSkipsModuleOnlyReceiver(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "factory.go", `package pkgmode

// ffigen:module calc
type Factory struct{}

func (f *Factory) New(base int64) *Factory {
	return f
}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)
	if strings.Contains(code, "impl.New(__recv, base)") || strings.Contains(code, "var __recv *Factory") {
		t.Fatalf("module-only struct should not inject receiver, got:\n%s", code)
	}
	if !strings.Contains(code, "r0 := impl.New(base)") {
		t.Fatalf("expected module-only struct method to remain impl-based, got:\n%s", code)
	}
	if !strings.Contains(code, `PackagePath: "calc", MemberName: "New", RouteName: "calc.New"`) {
		t.Fatalf("expected module-only struct methods to generate package member route descriptors, got:\n%s", code)
	}
	if strings.Contains(code, "registerStructSchema(\"calc.Factory\"") {
		t.Fatalf("module-only struct should not self-register a struct schema, got:\n%s", code)
	}
}

func TestRunDirectoryModeGeneratesBytesRefCopyBackSupport(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "mutator.go", `package pkgmode

import "gopkg.d7z.net/go-mini/core/ffigo"

// ffigen:module demo
// ffigen:proxy
type Mutator interface {
	Mutate(buf *ffigo.BytesRef) []byte
}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)
	if !strings.Contains(code, `runtime.MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Array<Byte>", runtime.FFIParamInOutBytes)`) {
		t.Fatalf("expected BytesRef schema to emit inout bytes mode, got:\n%s", code)
	}
	if !strings.Contains(code, `resBuf.WriteUvarint(uint64(1))`) {
		t.Fatalf("expected host router to write copy-back envelope, got:\n%s", code)
	}
	if !strings.Contains(code, `buf.Value, _ = retBuf.ReadBytes()`) {
		t.Fatalf("expected proxy to read copy-back into BytesRef, got:\n%s", code)
	}
}

func TestRunDirectoryModeGeneratesArrayRefCopyBackSupport(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "mutator.go", `package pkgmode

import "gopkg.d7z.net/go-mini/core/ffigo"

// ffigen:module demo
// ffigen:proxy
type Mutator interface {
	Rewrite(nums *ffigo.ArrayRef[int64]) int64
}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)
	if !strings.Contains(code, `runtime.MustParseRuntimeFuncSigWithModes("function(Array<Int64>) Int64", runtime.FFIParamInOutArray)`) {
		t.Fatalf("expected ArrayRef schema to emit inout array mode, got:\n%s", code)
	}
	if !strings.Contains(code, "if nums == nil {\n\t\twireBuf.WriteUvarint(0)\n\t} else {") {
		t.Fatalf("expected ArrayRef proxy to guard nil before encoding, got:\n%s", code)
	}
	if !strings.Contains(code, `resBuf.WriteBytes(copyBackBuf_nums.Bytes())`) {
		t.Fatalf("expected host router to write array copy-back envelope, got:\n%s", code)
	}
	if !strings.Contains(code, `copyBackPayload_nums, _ := retBuf.ReadBytes()`) || !strings.Contains(code, `copyBackBuf_nums := ffigo.NewReader(copyBackPayload_nums)`) {
		t.Fatalf("expected proxy to read nested array copy-back payload, got:\n%s", code)
	}
	if !strings.Contains(code, `copyBack_nums[i_copyBack_nums] = int64(tmp)`) {
		t.Fatalf("expected ArrayRef primitive elements to decode via varint, got:\n%s", code)
	}
}

func TestRunFileModeDoesNotInjectReceiverForModuleOnlyStruct(t *testing.T) {
	workspace := makeModuleTempDir(t)
	sourcePath := filepath.Join(workspace, "factory.go")
	writeTestFile(t, workspace, "factory.go", `package pkgmode

// ffigen:module calc
type Factory struct{}

func (f *Factory) New(base int64) *Factory {
	return f
}
`)

	outputPath := filepath.Join(workspace, "ffigen_factory.go")

	if err := runFileModeForTest("pkgmode", outputPath, []string{sourcePath}); err != nil {
		t.Fatalf("runFileMode: %v", err)
	}

	code := readGeneratedCode(t, outputPath)
	if strings.Contains(code, "impl.New(__recv, base)") || strings.Contains(code, "var __recv *Factory") {
		t.Fatalf("file mode module-only struct should not inject receiver, got:\n%s", code)
	}
	if !strings.Contains(code, "r0 := impl.New(base)") {
		t.Fatalf("expected file mode module-only struct method to remain impl-based, got:\n%s", code)
	}
	if !strings.Contains(code, `PackagePath: "calc", MemberName: "New", RouteName: "calc.New"`) {
		t.Fatalf("expected file mode module-only struct methods to generate package member route descriptors, got:\n%s", code)
	}
	if strings.Contains(code, "registerStructSchema(\"calc.Factory\"") {
		t.Fatalf("file mode module-only struct should not self-register a struct schema, got:\n%s", code)
	}
}

func TestRunDirPreservesGroupedParams(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "table.go", `package pkgmode

// ffigen:module table
// ffigen:methods Table
type Table struct{}

func (t *Table) SetString(row, col int, val string) {}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)
	required := `SetString function(HostRef<table.Table>, Int64, Int64, String) Void;`
	if !strings.Contains(code, required) {
		t.Fatalf("expected grouped params to be preserved in struct schema, missing %q in:\n%s", required, code)
	}
}

func TestRunDirPreservesGroupedResults(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "pair.go", `package pkgmode

// ffigen:module pair
// ffigen:proxy
type PairModule interface {
	Pair() (left, right int64)
}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)
	if !strings.Contains(code, `runtime.MustParseRuntimeFuncSig("function() tuple(Int64, Int64)")`) {
		t.Fatalf("expected grouped results to be expanded in schema, got:\n%s", code)
	}
	if !strings.Contains(code, "func (__p *PairModuleProxy) Pair() (int64, int64)") {
		t.Fatalf("expected grouped results to be expanded in proxy signature, got:\n%s", code)
	}
	if !strings.Contains(code, "r0, r1 := impl.Pair()") {
		t.Fatalf("expected grouped results to be expanded in host call, got:\n%s", code)
	}
}

func TestRunDirectoryModeRejectsMultipleModules(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "a.go", `package pkgmode

// ffigen:module a
type A interface {
	Ping() int64
}
`)
	writeTestFile(t, workspace, "b.go", `package pkgmode

// ffigen:module b
type B interface {
	Pong() int64
}
`)

	err := runDirectoryModeForTest("pkgmode", filepath.Join(workspace, "gen"), workspace)
	if err == nil || !strings.Contains(err.Error(), "at most one ffigen:module") {
		t.Fatalf("expected multiple-module error, got %v", err)
	}
}

func TestRunFileModeRejectsReservedPackageOutputName(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

type Demo interface {
	Ping() int64
}
`)

	err := runFileModeForTest("pkgmode", filepath.Join(workspace, "ffigen_pkgmode.go"), []string{filepath.Join(workspace, "api.go")})
	if err == nil || !strings.Contains(err.Error(), "reserved package output name") {
		t.Fatalf("expected reserved-name rejection, got %v", err)
	}
}

func TestRunFileDedupesSharedStructs(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "order.go", `package pkgmode

// ffigen:module order
// ffigen:methods Order
type Order struct {
	ID string
}

func (o *Order) Close() {}
`)
	writeTestFile(t, workspace, "service.go", `package pkgmode

// ffigen:module order
type OrderService interface {
	New(id string) *Order
}
`)

	outputPath := filepath.Join(workspace, "ffigen_shared.go")

	err := runFileModeForTest("pkgmode", outputPath, []string{
		filepath.Join(workspace, "order.go"),
		filepath.Join(workspace, "service.go"),
	})
	if err != nil {
		t.Fatalf("runFileMode: %v", err)
	}

	code := readGeneratedCode(t, outputPath)
	if count := strings.Count(code, "var order_Order_FFI_StructSchema = "); count != 1 {
		t.Fatalf("expected one shared struct schema, got %d\n%s", count, code)
	}
	if count := strings.Count(code, `schema.AddStruct("order", "Order",`); count != 1 {
		t.Fatalf("expected one shared struct schema binding, got %d\n%s", count, code)
	}
	if strings.Contains(code, `bound.AddStruct("order", "Order",`) {
		t.Fatalf("shared struct binding should be derived from schema, got:\n%s", code)
	}
}

func TestRunFileUsesImportAliasFallbackForHostRef(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

import "time"

// ffigen:module demo
type Demo interface {
	Load() *time.Time
}
`)

	outputPath := filepath.Join(workspace, "ffigen_demo.go")

	if err := runFileModeForTest("pkgmode", outputPath, []string{filepath.Join(workspace, "api.go")}); err != nil {
		t.Fatalf("runFileMode: %v", err)
	}
	code := readGeneratedCode(t, outputPath)
	if !strings.Contains(code, `registry.RegisterTyped(r0, "time.Time")`) {
		t.Fatalf("expected unresolved import to fall back to alias form for host handles, got:\n%s", code)
	}
	if !strings.Contains(code, "HostRef<time.Time>") {
		t.Fatalf("expected imported pointer to use HostRef alias fallback, got:\n%s", code)
	}
}

func TestRunFileRejectsBareImportedValueType(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

import "time"

// ffigen:module demo
type Demo interface {
	Load() time.Time
}
`)

	err := Run(Options{
		PackageName: "pkgmode",
		Output:      filepath.Join(workspace, "ffigen_demo.go"),
		Args:        []string{filepath.Join(workspace, "api.go")},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported bare FFI type") {
		t.Fatalf("expected bare imported value type rejection, got %v", err)
	}
}

func TestRunDirectoryModeRejectsInterfaceFFIParam(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

type Reader interface {
	Read([]byte) (int64, error)
}

// ffigen:module demo
type Demo interface {
	Use(r Reader)
}
`)

	err := Run(Options{
		PackageName: "pkgmode",
		Output:      filepath.Join(workspace, "gen"),
		Args:        []string{workspace},
	})
	if err == nil || !strings.Contains(err.Error(), "interface parameter") {
		t.Fatalf("expected interface parameter rejection, got %v", err)
	}
}

func TestRunDirectoryModeGeneratesSignedWireForUnsignedGoTypes(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

// ffigen:module nums
// ffigen:proxy
type Numbers interface {
	Echo(v uint8) uint8
	Next() uint64
}
`)

	outputDir := filepath.Join(workspace, "gen")
	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}
	code := readGeneratedCode(t, filepath.Join(outputDir, "ffigen_pkgmode.go"))
	if !strings.Contains(code, "wireBuf.WriteVarint(int64(v))") {
		t.Fatalf("expected unsigned params to use signed varint wire, got:\n%s", code)
	}
	if !strings.Contains(code, "resBuf.WriteVarint(int64(r0))") {
		t.Fatalf("expected unsigned results to use signed varint wire, got:\n%s", code)
	}
	if !strings.Contains(code, "tmp, _ := reqBuf.ReadVarint()") || !strings.Contains(code, "tmp < 0 || tmp > 255") {
		t.Fatalf("expected host router to decode uint8 from signed varint with bounds, got:\n%s", code)
	}
	if strings.Contains(code, "wireBuf.WriteUvarint(uint64(v))") || strings.Contains(code, "resBuf.WriteUvarint(uint64(r0))") {
		t.Fatalf("generated unsigned value codec still uses unsigned varint:\n%s", code)
	}
}

func TestRunDirectoryModeGeneratesChannelSupport(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

// ffigen:module chanmod
// ffigen:proxy
type ChanModule interface {
	Source() <-chan int64
	Sink() chan<- int64
	Forward(in <-chan int64, out chan<- int64)
}
`)

	outputDir := filepath.Join(workspace, "gen")
	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}
	code := readGeneratedCode(t, filepath.Join(outputDir, "ffigen_pkgmode.go"))
	for _, required := range []string{
		`runtime.MustParseRuntimeFuncSig("function() RecvChan<Int64>")`,
		`runtime.MustParseRuntimeFuncSig("function() SendChan<Int64>")`,
		`runtime.MustParseRuntimeFuncSigWithModes("function(RecvChan<Int64>, SendChan<Int64>) Void", runtime.FFIParamIn, runtime.FFIParamIn)`,
		`ffigo.ChannelEndpointFuncs`,
		`ChannelRegistryFromContext(ctx)`,
		`__p.channelRegistry()`,
		`RegisterChannel(`,
		`UnregisterChannel(`,
	} {
		if !strings.Contains(code, required) {
			t.Fatalf("expected generated channel support to contain %q, got:\n%s", required, code)
		}
	}
}

func TestRunDirectoryModeGeneratesBidirectionalChannelReturn(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

// ffigen:module chanmod
type ChanModule interface {
	Open() chan int64
}
`)

	outputDir := filepath.Join(workspace, "gen")
	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}
	code := readGeneratedCode(t, filepath.Join(outputDir, "ffigen_pkgmode.go"))
	for _, required := range []string{
		`runtime.MustParseRuntimeFuncSig("function() Chan<Int64>")`,
		`ffigo.ChannelEndpointFuncs{Elem: "Int64", Dir: ffigo.ChannelBoth}`,
	} {
		if !strings.Contains(code, required) {
			t.Fatalf("expected generated bidirectional channel return to contain %q, got:\n%s", required, code)
		}
	}
}

func TestRunRejectsBidirectionalChannelParameter(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

// ffigen:module chanmod
type ChanModule interface {
	Use(ch chan int64)
}
`)

	err := Run(Options{
		PackageName: "pkgmode",
		Output:      filepath.Join(workspace, "gen"),
		Args:        []string{workspace},
	})
	if err == nil || !strings.Contains(err.Error(), "bidirectional channel parameter") {
		t.Fatalf("expected bidirectional channel parameter rejection, got %v", err)
	}
}

func TestRunRejectsMalformedInterfaceDirective(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

// ffigen:interface extra
type Reader interface {
	Read([]byte) (int64, error)
}
`)

	err := Run(Options{
		PackageName: "pkgmode",
		Output:      filepath.Join(workspace, "ffigen_demo.go"),
		Args:        []string{filepath.Join(workspace, "api.go")},
	})
	if err == nil || !strings.Contains(err.Error(), "ffigen:interface does not accept arguments") {
		t.Fatalf("expected malformed interface directive rejection, got %v", err)
	}
}

func TestRunDirectoryModeIgnoresImportedSiblingTestModules(t *testing.T) {
	workspace := makeModuleTempDir(t)
	depDir := filepath.Join(workspace, "dep")
	if err := os.Mkdir(depDir, 0o755); err != nil {
		t.Fatalf("mkdir dep: %v", err)
	}
	writeTestFile(t, depDir, "dep.go", `package dep

type Item struct{}

// ffigen:module depmod
type DepModule interface {
	New() *Item
}
`)
	writeTestFile(t, depDir, "dep_test.go", `package dep

// ffigen:module wrong
type WrongModule interface {
	Wrong() int64
}
`)
	importPath := "gopkg.d7z.net/go-mini/core/ffigen/" + filepath.Base(workspace) + "/dep"
	writeTestFile(t, workspace, "api.go", `package pkgmode

import dep "`+importPath+`"

// ffigen:module demo
type Demo interface {
	New() *dep.Item
}
`)

	outputDir := filepath.Join(workspace, "gen")
	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}
	code := readGeneratedCode(t, filepath.Join(outputDir, "ffigen_pkgmode.go"))
	if !strings.Contains(code, "HostRef<depmod.Item>") {
		t.Fatalf("expected imported non-test module metadata to resolve, got:\n%s", code)
	}
	if strings.Contains(code, "wrong.Item") {
		t.Fatalf("imported _test.go module metadata leaked into generated schema:\n%s", code)
	}
}

func TestRunFileModeIgnoresSiblingTestFiles(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

// ffigen:module demo
type Demo interface {
	Ping() string
}
`)
	writeTestFile(t, workspace, "api_test.go", `package pkgmode

import engine "gopkg.d7z.net/go-mini/core"

var _ = engine.NewMiniExecutor
`)

	outputPath := filepath.Join(workspace, "ffigen_demo.go")

	if err := runFileModeForTest("pkgmode", outputPath, []string{filepath.Join(workspace, "api.go")}); err != nil {
		t.Fatalf("runFileMode should ignore sibling _test.go files, got %v", err)
	}
}

func TestDetectGenerationModeRejectsGeneratedFileInput(t *testing.T) {
	mode, err := detectGenerationMode([]string{"./", "ffigen_ops.go"})
	if err == nil || !strings.Contains(err.Error(), "generated file") {
		t.Fatalf("expected generated-file rejection, got mode=%v err=%v", mode, err)
	}
}

func TestRunDirFlattensEmbeddedInterfaces(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

// ffigen:module io
// ffigen:interface
type Reader interface {
	Read([]byte) (int64, error)
}

// ffigen:module io
// ffigen:interface
type Writer interface {
	Write([]byte) (int64, error)
}

// ffigen:module io
// ffigen:interface
type ReadWriter interface {
	Reader
	Writer
}

// ffigen:module io
type IO interface {
	ReadAll(r Reader) ([]byte, error)
	Copy(dst Writer, src Reader) (int64, error)
}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)
	if !strings.Contains(code, `var io_ReadWriter_FFI_InterfaceSchema = runtime.MustParseRuntimeInterfaceSpec("interface{Read(Array<Byte>) tuple(Int64, Error);Write(Array<Byte>) tuple(Int64, Error);}`) {
		t.Fatalf("expected flattened ReadWriter interface schema, got:\n%s", code)
	}
	if !strings.Contains(code, "func SurfaceReadWriterSchema(") {
		t.Fatalf("expected schema surface helper for interface target")
	}
	if !strings.Contains(code, `function(io.Reader) tuple(Array<Byte>, Error)`) {
		t.Fatalf("expected named Reader parameter schema, got:\n%s", code)
	}
	if !strings.Contains(code, `function(io.Writer, io.Reader) tuple(Int64, Error)`) {
		t.Fatalf("expected named Writer/Reader parameter schema, got:\n%s", code)
	}
	if !strings.Contains(code, `schema.AddInterface("io", "Reader", io_Reader_FFI_InterfaceSchema)`) {
		t.Fatalf("expected IO surface to include referenced Reader interface, got:\n%s", code)
	}
}

func TestRunDirectoryModeSkipsUnmarkedInterfaces(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

type Reader interface {
	Read([]byte) (int64, error)
}
`)

	outputDir := filepath.Join(workspace, "gen")

	if err := runDirectoryModeForTest("pkgmode", outputDir, workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	code := readGeneratedCode(t, generatedPath)
	if strings.Contains(code, "RegisterReaderSchema") || strings.Contains(code, "type ReaderProxy") {
		t.Fatalf("expected unmarked interface to be skipped, got:\n%s", code)
	}
}

func TestRunDirRejectsEmbeddedMethodConflict(t *testing.T) {
	readerA := &ast.InterfaceType{
		Methods: &ast.FieldList{List: []*ast.Field{
			{
				Names: []*ast.Ident{ast.NewIdent("Read")},
				Type:  mustParseFuncType(t, "func([]byte) (int64, error)"),
			},
		}},
	}
	readerB := &ast.InterfaceType{
		Methods: &ast.FieldList{List: []*ast.Field{
			{
				Names: []*ast.Ident{ast.NewIdent("Read")},
				Type:  mustParseFuncType(t, "func(string) (int64, error)"),
			},
		}},
	}
	broken := &ast.InterfaceType{
		Methods: &ast.FieldList{List: []*ast.Field{
			{Type: ast.NewIdent("ReaderA")},
			{Type: ast.NewIdent("ReaderB")},
		}},
	}

	_, err := NewGenerator(Options{PackageName: "pkgmode", Output: "unused"}).flattenInterfaceType("Broken", broken, map[string]*ast.InterfaceType{
		"ReaderA": readerA,
		"ReaderB": readerB,
		"Broken":  broken,
	})
	if err == nil || !strings.Contains(err.Error(), "method conflict for Read") {
		t.Fatalf("expected embedded interface conflict, got %v", err)
	}
}

func runDirectoryModeForTest(packageName, output, dir string) error {
	return NewGenerator(Options{PackageName: packageName, Output: output}).runDirectoryMode(dir)
}

func runFileModeForTest(packageName, output string, args []string) error {
	return NewGenerator(Options{PackageName: packageName, Output: output}).runFileMode(args)
}

func makeModuleTempDir(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// Keep test packages inside the module so go list can derive module metadata.
	workspace, err := os.MkdirTemp(cwd, "ffigen-main-test-") //nolint:usetesting // must stay inside the module for go list
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(workspace)
	})
	return workspace
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func readGeneratedCode(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated output: %v", err)
	}
	return string(content)
}

func mustParseFuncType(t *testing.T, src string) *ast.FuncType {
	t.Helper()
	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("parse func type %q: %v", src, err)
	}
	fn, ok := expr.(*ast.FuncType)
	if !ok {
		t.Fatalf("parsed %q as %T", src, expr)
	}
	return fn
}
