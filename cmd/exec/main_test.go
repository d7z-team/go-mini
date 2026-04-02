package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestLoadSourceProgramMergesFiles(t *testing.T) {
	tempDir := t.TempDir()
	fileA := filepath.Join(tempDir, "a.go")
	fileB := filepath.Join(tempDir, "b.go")

	if err := os.WriteFile(fileA, []byte("package main\n\nfunc helper() Int64 { return 7 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("package main\n\nfunc main() { if helper() != 7 { panic(\"bad\") } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	program, err := loadSourceProgram([]string{fileA, fileB})
	if err != nil {
		t.Fatalf("loadSourceProgram failed: %v", err)
	}
	if program.Package != "main" {
		t.Fatalf("unexpected package: %s", program.Package)
	}
	if _, ok := program.Functions["helper"]; !ok {
		t.Fatalf("expected merged helper function")
	}
	if _, ok := program.Functions["main"]; !ok {
		t.Fatalf("expected merged main function")
	}
}

func TestRunCompilesWritesBytecodeAndExecutes(t *testing.T) {
	tempDir := t.TempDir()
	sourceFile := filepath.Join(tempDir, "main.go")
	bytecodeFile := filepath.Join(tempDir, "program.json")
	source := `package main
func main() {
	println("exec ok")
}
`
	if err := os.WriteFile(sourceFile, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"-o", bytecodeFile, "-run", sourceFile}); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	payload, err := os.ReadFile(bytecodeFile)
	if err != nil {
		t.Fatalf("read bytecode output: %v", err)
	}
	if !strings.Contains(string(payload), "\"format\": \"go-mini-bytecode\"") {
		t.Fatalf("expected bytecode header in output")
	}
}

func TestLoadProgramFromBytecode(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	compiled, err := executor.CompileGoCode(`package main
func main() {}
`)
	if err != nil {
		t.Fatalf("CompileGoCode failed: %v", err)
	}
	payload, err := compiled.MarshalBytecodeJSON()
	if err != nil {
		t.Fatalf("MarshalBytecodeJSON failed: %v", err)
	}

	bytecodeFile := filepath.Join(t.TempDir(), "program.json")
	if err := os.WriteFile(bytecodeFile, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	program, err := loadProgram(executor, &execOptions{bytecode: bytecodeFile, run: true})
	if err != nil {
		t.Fatalf("loadProgram failed: %v", err)
	}
	if err := program.Execute(context.Background()); err != nil {
		t.Fatalf("execute bytecode program failed: %v", err)
	}
}
