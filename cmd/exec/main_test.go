package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestLoadProgramMergesFilesViaCompilePipeline(t *testing.T) {
	tempDir := t.TempDir()
	fileA := filepath.Join(tempDir, "a.mgo")
	fileB := filepath.Join(tempDir, "b.mgo")

	if err := os.WriteFile(fileA, []byte("package main\n\nfunc helper() Int64 { return 7 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("package main\n\nfunc main() { if helper() != 7 { panic(\"bad\") } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	executor := engine.NewMiniExecutor()
	program, err := loadProgram(executor, &execOptions{inputs: []string{fileA, fileB}, run: true})
	if err != nil {
		t.Fatalf("loadProgram failed: %v", err)
	}
	if program.Program.Package != "main" {
		t.Fatalf("unexpected package: %s", program.Program.Package)
	}
	if _, ok := program.Program.Functions["helper"]; !ok {
		t.Fatalf("expected merged helper function")
	}
	if _, ok := program.Program.Functions["main"]; !ok {
		t.Fatalf("expected merged main function")
	}
}

func TestRunCompilesWritesBytecodeAndExecutes(t *testing.T) {
	tempDir := t.TempDir()
	sourceFile := filepath.Join(tempDir, "main.mgo")
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

func TestLoadProgramFromDirectoryLoadsMGOFiles(t *testing.T) {
	tempDir := t.TempDir()
	fileA := filepath.Join(tempDir, "helper.mgo")
	fileB := filepath.Join(tempDir, "main.mgo")
	ignored := filepath.Join(tempDir, "ignored.go")

	if err := os.WriteFile(fileA, []byte("package main\n\nfunc helper() Int64 { return 9 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("package main\n\nfunc main() { if helper() != 9 { panic(\"bad\") } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignored, []byte("package main\n\nfunc ignored() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	executor := engine.NewMiniExecutor()
	program, err := loadProgram(executor, &execOptions{inputs: []string{tempDir}, run: true})
	if err != nil {
		t.Fatalf("loadProgram from dir failed: %v", err)
	}
	if _, ok := program.Program.Functions["helper"]; !ok {
		t.Fatalf("expected helper function from .mgo input")
	}
	if _, ok := program.Program.Functions["ignored"]; ok {
		t.Fatalf("did not expect .go file to be loaded in directory mode")
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
