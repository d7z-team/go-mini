package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/scanner"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/token"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

type discoveredTest struct {
	Name string
}

const TestSelectionEntry = "test.select"

// PrepareTest builds the root package's internal and external tests as one executable image.
func PrepareTest(request Request) (PrepareResult, error) {
	request.Context = requestContext(request.Context)
	if err := request.Context.Err(); err != nil {
		return PrepareResult{}, err
	}
	if err := validateOptimizationLevel(request.Optimization); err != nil {
		return PrepareResult{}, err
	}
	normalizedTarget, err := target.Normalize(request.Target)
	if err != nil {
		return PrepareResult{}, err
	}
	request.Target = normalizedTarget
	request.Limits = normalizeCompilerLimits(request.Limits)
	snapshot, err := loadTestSourceSnapshot(request.Context, request.Sources, normalizedTarget, request.Limits)
	if err != nil {
		return PrepareResult{}, err
	}
	return prepareTestWorkspace(request, snapshot)
}

func prepareTestWorkspace(request Request, snapshot *testSourceSnapshot) (PrepareResult, error) {
	request.Context = requestContext(request.Context)
	if err := request.Context.Err(); err != nil {
		return PrepareResult{}, err
	}
	if len(snapshot.limits) != 0 {
		return PrepareResult{Checked: CheckResult{Target: snapshot.target, Diagnostics: append([]source.Diagnostic(nil), snapshot.limits...)}}, nil
	}
	rootPackage, ok := snapshot.byPath[request.Root]
	if !ok {
		return PrepareResult{}, fmt.Errorf("test root package %q is missing", request.Root)
	}
	if diagnostics := snapshot.diagnostics[request.Root]; len(diagnostics) != 0 {
		return PrepareResult{Checked: CheckResult{Target: snapshot.target, Diagnostics: diagnostics}}, nil
	}
	if len(rootPackage.TestFiles) == 0 {
		return testDiagnostic(snapshot.target, "compiler.test.files_missing", fmt.Sprintf("package %s has no test files", request.Root)), nil
	}

	rootHeader, diagnostics, err := workspace.ScanPackageHeaderWithLimits(rootPackage, request.Limits.workspaceLimits())
	if err != nil {
		return PrepareResult{}, err
	}
	if source.HasErrors(diagnostics) {
		return PrepareResult{Checked: CheckResult{Target: snapshot.target, Diagnostics: diagnostics}}, nil
	}
	rootName := rootHeader.Package
	var internalFiles, externalFiles []source.File
	var internalTests, externalTests []discoveredTest
	for _, file := range rootPackage.TestFiles {
		if err := request.Context.Err(); err != nil {
			return PrepareResult{}, err
		}
		compiledPath := strings.TrimSuffix(file.Path, "_test.mgo") + ".test.mgo"
		compiled := source.File{Path: compiledPath, Text: file.Text, Hash: file.Hash}
		packageName, tests, scanDiagnostics := scanTestFile(compiled, scanner.Limits{
			MaxSourceBytes: request.Limits.MaxSourceBytes,
			MaxTokens:      request.Limits.MaxTokens,
			MaxDiagnostics: request.Limits.MaxDiagnostics,
		})
		if source.HasErrors(scanDiagnostics) {
			return PrepareResult{Checked: CheckResult{Target: snapshot.target, Diagnostics: scanDiagnostics}}, nil
		}
		switch packageName {
		case rootName:
			internalFiles = append(internalFiles, compiled)
			internalTests = append(internalTests, tests...)
		case rootName + "_test":
			externalFiles = append(externalFiles, compiled)
			externalTests = append(externalTests, tests...)
		default:
			return testDiagnostic(snapshot.target, "compiler.test.package", fmt.Sprintf("test file %s declares package %q, expected %q or %q", file.Path, packageName, rootName, rootName+"_test")), nil
		}
	}
	if len(internalTests)+len(externalTests) == 0 {
		return testDiagnostic(snapshot.target, "compiler.test.none", fmt.Sprintf("package %s has test files but no Test functions", request.Root)), nil
	}

	hash := sha256.Sum256([]byte("mini-go-test-v2|" + request.Root))
	suffix := hex.EncodeToString(hash[:])[:16]
	internalModule := "minigo.test/" + suffix + "/internal"
	externalModule := "minigo.test/" + suffix + "/external"
	testMainModule := "minigo.test/" + suffix + "/main"

	packages := make([]workspace.SourcePackage, 0, len(snapshot.packages)+3)
	for _, packageSource := range snapshot.packages {
		if err := request.Context.Err(); err != nil {
			return PrepareResult{}, err
		}
		if diagnostics := snapshot.diagnostics[packageSource.ModulePath]; len(diagnostics) != 0 {
			return PrepareResult{Checked: CheckResult{Target: snapshot.target, Diagnostics: diagnostics}}, nil
		}
		pkg := packageSource
		pkg.Files = append([]source.File(nil), packageSource.Files...)
		pkg.TestFiles = nil
		pkg.SourceCandidates = nil
		packages = append(packages, pkg)
	}
	if len(internalFiles) != 0 {
		files := append([]source.File(nil), rootPackage.Files...)
		files = append(files, internalFiles...)
		packages = append(packages, workspace.SourcePackage{
			ModulePath: internalModule,
			Files:      files,
			Resources:  append([]workspace.ResourceFile(nil), rootPackage.Resources...),
		})
	}
	if len(externalFiles) != 0 {
		packages = append(packages, workspace.SourcePackage{
			ModulePath: externalModule,
			Files:      externalFiles,
			Resources:  append([]workspace.ResourceFile(nil), rootPackage.Resources...),
		})
	}
	packages = append(packages, workspace.SourcePackage{
		ModulePath: testMainModule,
		Files:      []source.File{{Path: "_testmain.mgo", Text: testMainSource(request.Root, internalModule, externalModule, internalTests, externalTests)}},
	})
	sources, err := workspace.NewMemorySourceSet(packages)
	if err != nil {
		return PrepareResult{}, err
	}
	testRequest := request
	testRequest.Root = testMainModule
	testRequest.Sources = sources
	testRequest.workspace, err = workspace.NewLoader(sources, request.Target, request.Limits.workspaceLimits())
	if err != nil {
		return PrepareResult{}, err
	}
	testRequest.EntryPoints = []EntryPoint{
		{Name: "default", ModulePath: testMainModule, Function: "MiniGoTestMain"},
		{Name: TestSelectionEntry, ModulePath: testMainModule, Function: "MiniGoTestSelect"},
	}
	tests := make([]discoveredTest, 0, len(internalTests)+len(externalTests))
	tests = append(tests, internalTests...)
	tests = append(tests, externalTests...)
	manifest := make([]cache.TestEntry, 0, len(tests))
	for _, test := range tests {
		manifest = append(manifest, cache.TestEntry{Package: request.Root, Name: test.Name, Index: len(manifest)})
	}
	prepared, err := prepare(testRequest, "prepare_test", manifest)
	if err != nil {
		return PrepareResult{}, err
	}
	prepared.Checked.Stats.TestsDiscovered = len(prepared.TestManifest)
	return prepared, nil
}

func scanTestFile(file source.File, limits scanner.Limits) (string, []discoveredTest, []source.Diagnostic) {
	scanned := scanner.ScanFileWithLimits(file, limits)
	if source.HasErrors(scanned.Diagnostics) {
		return "", nil, scanned.Diagnostics
	}
	tokens := scanned.Tokens
	if len(tokens) < 3 || tokens[0].Kind != token.Package || tokens[1].Kind != token.Ident {
		return "", nil, []source.Diagnostic{{Code: "parser.package", Severity: source.SeverityError, Message: fmt.Sprintf("file %q must start with a package declaration", file.Path)}}
	}
	var tests []discoveredTest
	braceDepth := 0
	for index := 2; index+2 < len(tokens); index++ {
		switch tokens[index].Kind {
		case token.Lbrace:
			braceDepth++
		case token.Rbrace:
			if braceDepth > 0 {
				braceDepth--
			}
		case token.Func:
			if braceDepth == 0 && tokens[index+1].Kind == token.Ident && strings.HasPrefix(tokens[index+1].Lexeme, "Test") && tokens[index+2].Kind == token.Lparen {
				tests = append(tests, discoveredTest{Name: tokens[index+1].Lexeme})
			}
		}
	}
	sort.Slice(tests, func(i, j int) bool { return tests[i].Name < tests[j].Name })
	return tokens[1].Lexeme, tests, nil
}

func testMainSource(root, internalModule, externalModule string, internalTests, externalTests []discoveredTest) string {
	var source strings.Builder
	source.WriteString("package main\n\nimport (\n")
	if len(internalTests) != 0 {
		source.WriteString("\tptest ")
		source.WriteString(strconv.Quote(internalModule))
		source.WriteByte('\n')
	}
	if len(externalTests) != 0 {
		source.WriteString("\tpxtest ")
		source.WriteString(strconv.Quote(externalModule))
		source.WriteByte('\n')
	}
	source.WriteString("\t\"testing\"\n)\n\n")
	source.WriteString("func minigoTests() []testing.InternalTest {\n\treturn []testing.InternalTest{\n")
	for _, test := range internalTests {
		source.WriteString("\t\t{Name: ")
		source.WriteString(strconv.Quote(test.Name))
		source.WriteString(", F: ptest.")
		source.WriteString(test.Name)
		source.WriteString("},\n")
	}
	for _, test := range externalTests {
		source.WriteString("\t\t{Name: ")
		source.WriteString(strconv.Quote(test.Name))
		source.WriteString(", F: pxtest.")
		source.WriteString(test.Name)
		source.WriteString("},\n")
	}
	source.WriteString("\t}\n}\n\nfunc MiniGoTestMain() testing.Report {\n\treturn testing.Main(")
	source.WriteString(strconv.Quote(root))
	source.WriteString(", minigoTests())\n}\n\nfunc MiniGoTestSelect(selected []int) testing.Report {\n\tall := minigoTests()\n\ttests := []testing.InternalTest{}\n\tfor _, index := range selected {\n\t\tif index < 0 || index >= len(all) { panic(\"invalid test selection\") }\n\t\ttests = append(tests, all[index])\n\t}\n\treturn testing.Main(")
	source.WriteString(strconv.Quote(root))
	source.WriteString(", tests)\n}\n\nfunc main() { MiniGoTestMain() }\n")
	return source.String()
}

func testDiagnostic(buildTarget target.Target, code source.DiagnosticCode, message string) PrepareResult {
	return PrepareResult{Checked: CheckResult{Target: buildTarget, Diagnostics: []source.Diagnostic{{Code: code, Severity: source.SeverityError, Message: message}}}}
}
