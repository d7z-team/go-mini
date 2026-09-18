package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFormatsStandardInput(t *testing.T) {
	var output bytes.Buffer
	if err := runFormat(testCommandEnvironment(t, "."), nil, strings.NewReader("package   main\nfunc main( ){ }\n"), &output, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "package main\n\nfunc main() {}\n" {
		t.Fatalf("formatted output = %q", output.String())
	}
}

func TestRunListsAndWritesFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "main.mgo")
	if err := os.WriteFile(path, []byte("package   main\nfunc main( ){ }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runFormat(testCommandEnvironment(t, directory), []string{"-l", "-w", "main.mgo"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "main.mgo" {
		t.Fatalf("listed path = %q", output.String())
	}
	formatted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(formatted) != "package main\n\nfunc main() {}\n" {
		t.Fatalf("formatted file = %q", formatted)
	}
}

func TestRunCheckReportsDifferencesWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.mgo")
	input := "package   main\nfunc main( ){ }\n"
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := runFormat(testCommandEnvironment(t, "."), []string{"-check", path}, strings.NewReader(""), &output, &output)
	if err == nil || !strings.Contains(err.Error(), "not formatted") {
		t.Fatalf("check error = %v", err)
	}
	if strings.TrimSpace(output.String()) != path {
		t.Fatalf("check output = %q", output.String())
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || string(data) != input {
		t.Fatalf("check modified source: data=%q err=%v", data, readErr)
	}
	output.Reset()
	if err := runFormat(testCommandEnvironment(t, "."), []string{"-w", path}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatal(err)
	}
	if err := runFormat(testCommandEnvironment(t, "."), []string{"-check", path}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatalf("formatted check failed: %v", err)
	}
}

func TestRunValidatesAllFilesBeforeWriting(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.mgo")
	second := filepath.Join(directory, "second.mgo")
	input := "package   first\n"
	if err := os.WriteFile(first, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("package second\nfunc broken( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runFormat(testCommandEnvironment(t, "."), []string{"-w", first, second}, strings.NewReader(""), &output, &output); err == nil {
		t.Fatal("format succeeded with an invalid input")
	}
	data, err := os.ReadFile(first)
	if err != nil || string(data) != input {
		t.Fatalf("first source changed before validation completed: data=%q err=%v", data, err)
	}
	if output.Len() != 0 {
		t.Fatalf("failed batch wrote output %q", output.String())
	}
}

func TestRunWritePreservesPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.mgo")
	if err := os.WriteFile(path, []byte("package   main\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o750); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runFormat(testCommandEnvironment(t, "."), []string{"-w", path}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("file mode = %o, want 750", got)
	}
}

func TestRunRejectsConflictingOrPathOnlyFlags(t *testing.T) {
	for _, args := range [][]string{{"-w", "-check", "main.mgo"}, {"-check"}} {
		var output bytes.Buffer
		if err := runFormat(testCommandEnvironment(t, "."), args, strings.NewReader("package main\n"), &output, &output); err == nil {
			t.Fatalf("runFormat(%q) succeeded", args)
		}
	}
}

func TestFormatValidatesSuffixBeforeWritingBatch(t *testing.T) {
	directory := t.TempDir()
	input := "package   main\n"
	writeCommandFile(t, directory, "main.mgo", input)
	writeCommandFile(t, directory, "binding.go", "package main\n")
	var output bytes.Buffer
	err := runFormat(testCommandEnvironment(t, directory), []string{"-w", "main.mgo", "binding.go"}, strings.NewReader(""), &output, &output)
	if err == nil || !strings.Contains(err.Error(), ".mgo or .mrpc") {
		t.Fatalf("format error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "main.mgo"))
	if err != nil || string(data) != input || output.Len() != 0 {
		t.Fatalf("batch partially committed: source=%q output=%q error=%v", data, output.String(), err)
	}
}
