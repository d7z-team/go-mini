package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProvidesStandardLibraryConsole(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "main.mgo"), []byte(`package main

import "fmt"

func main() { fmt.Print("hello from MiniGo") }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := runProgram(testCommandEnvironment(t, directory), nil, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	if got, want := stdout.String(), "hello from MiniGo"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunReadsStandardInput(t *testing.T) {
	directory := t.TempDir()
	writeCommandFile(t, directory, "main.mgo", `package main
import "fmt"
func main() {
 var value string
 if n, err := fmt.Scan(&value); n != 1 || err != nil { panic("scan failed") }
 fmt.Print(value)
}`)
	environment := testCommandEnvironment(t, directory)
	environment.stdin = strings.NewReader("input\n")
	var stdout, stderr bytes.Buffer
	if err := runProgram(environment, []string{"main.mgo"}, &stdout, &stderr); err != nil {
		t.Fatalf("run = %v, stderr: %s", err, &stderr)
	}
	if stdout.String() != "input" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunExplicitFilesWithoutModule(t *testing.T) {
	directory := t.TempDir()
	writeCommandFile(t, directory, "main.mgo", `package main
import (
	_ "embed"
	"fmt"
)
//go:embed assets/value.txt
var value string
func main() { fmt.Print(value + helper()) }
`)
	writeCommandFile(t, directory, "helper.mgo", `//go:build excluded
package main
func helper() string { return " tagged" }
`)
	writeCommandFile(t, directory, "ignored.mgo", "package broken\nfunc missing(\n")
	writeCommandFile(t, directory, "assets/value.txt", "embedded")
	writeCommandFile(t, directory, "assets/ignored.txt", "ignored")

	var stdout, stderr bytes.Buffer
	if err := runProgram(testCommandEnvironment(t, directory), []string{"main.mgo", "helper.mgo"}, &stdout, &stderr); err != nil {
		t.Fatalf("run explicit files: %v\nstderr: %s", err, stderr.String())
	}
	if got, want := stdout.String(), "embedded tagged"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCheck(testCommandEnvironment(t, directory), []string{"main.mgo", "helper.mgo"}, &stderr); err != nil {
		t.Fatalf("check explicit files: %v\nstderr: %s", err, stderr.String())
	}
}

func TestRunExplicitFileUsesRegisteredSources(t *testing.T) {
	directory := t.TempDir()
	writeCommandFile(t, directory, "lib/value.mgo", "package lib\nfunc Value() string { return \"module\" }\n")
	writeCommandFile(t, directory, "scripts/main.mgo", `package main
import (
	"example.com/application/lib"
	"fmt"
)
func main() { fmt.Print(lib.Value()) }
`)

	cacheDirectory := filepath.Join(directory, "cache")
	t.Setenv(envCacheRoot, cacheDirectory)
	t.Setenv(envDebug, "cachetrace=1")
	environment := testCommandEnvironment(t, directory)
	run := func() (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if err := runProgram(environment, []string{"-source", "example.com/application/lib=lib", "scripts/main.mgo"}, &stdout, &stderr); err != nil {
			t.Fatalf("run module file: %v\nstderr: %s", err, stderr.String())
		}
		return stdout.String(), stderr.String()
	}
	if output, _ := run(); output != "module" {
		t.Fatalf("stdout = %q, want %q", output, "module")
	}
	if _, trace := run(); !strings.Contains(trace, "cache prepare_hit command-line-arguments") {
		t.Fatalf("unchanged dependency trace = %q", trace)
	}
	writeCommandFile(t, directory, "lib/value.mgo", "package lib\nfunc Value() string { return \"changed\" }\n")
	if output, trace := run(); output != "changed" || strings.Contains(trace, "cache prepare_hit command-line-arguments") {
		t.Fatalf("changed dependency output=%q trace=%q", output, trace)
	}
}

func TestRunExplicitFileReusesCompilerCache(t *testing.T) {
	directory := t.TempDir()
	writeCommandFile(t, directory, "main.mgo", "package main\nfunc main() {}\n")
	writeCommandFile(t, directory, "ignored.txt", "first")
	cacheDirectory := filepath.Join(directory, "cache")
	t.Setenv(envCacheRoot, cacheDirectory)
	t.Setenv(envDebug, "cachetrace=1")
	environment := testCommandEnvironment(t, directory)

	run := func() string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if err := runProgram(environment, []string{"main.mgo"}, &stdout, &stderr); err != nil {
			t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
		}
		return stderr.String()
	}
	if trace := run(); !strings.Contains(trace, "cache store command-line-arguments") {
		t.Fatalf("first trace = %q", trace)
	}
	writeCommandFile(t, directory, "ignored.txt", "second")
	if trace := run(); !strings.Contains(trace, "cache prepare_hit command-line-arguments") {
		t.Fatalf("second trace = %q", trace)
	}
	t.Setenv(envDebug, "cachetrace=1,cachehash=1,cacheverify=1")
	if trace := run(); !strings.Contains(trace, "cache hash command-line-arguments") ||
		!strings.Contains(trace, "cache verify command-line-arguments") ||
		!strings.Contains(trace, "cache prepare_verify command-line-arguments") {
		t.Fatalf("verified trace = %q", trace)
	}
	writeCommandFile(t, directory, "main.mgo", "package main\nfunc main() { _ = 1 }\n")
	if trace := run(); !strings.Contains(trace, "cache store command-line-arguments") || strings.Contains(trace, "cache prepare_hit command-line-arguments") {
		t.Fatalf("changed source trace = %q", trace)
	}
}

func TestRunExplicitFileInvalidatesCacheForEmbeddedResource(t *testing.T) {
	directory := t.TempDir()
	writeCommandFile(t, directory, "main.mgo", `package main
import _ "embed"
//go:embed value.txt
var value string
func main() { _ = value }
`)
	writeCommandFile(t, directory, "value.txt", "first")
	cacheDirectory := filepath.Join(directory, "cache")
	t.Setenv(envCacheRoot, cacheDirectory)
	t.Setenv(envDebug, "cachetrace=1")
	environment := testCommandEnvironment(t, directory)

	run := func() string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if err := runProgram(environment, []string{"main.mgo"}, &stdout, &stderr); err != nil {
			t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
		}
		return stderr.String()
	}
	_ = run()
	if trace := run(); !strings.Contains(trace, "cache prepare_hit command-line-arguments") {
		t.Fatalf("unchanged trace = %q", trace)
	}
	writeCommandFile(t, directory, "value.txt", "second")
	if trace := run(); !strings.Contains(trace, "cache store command-line-arguments") || strings.Contains(trace, "cache prepare_hit command-line-arguments") {
		t.Fatalf("changed resource trace = %q", trace)
	}
}

func TestExplicitFileInputErrors(t *testing.T) {
	directory := t.TempDir()
	other := t.TempDir()
	writeCommandFile(t, directory, "main.mgo", "package main\nfunc main() {}\n")
	writeCommandFile(t, directory, "main_test.mgo", "package main\n")
	writeCommandFile(t, directory, "library.mgo", "package library\nfunc Value() int { return 1 }\n")
	writeCommandFile(t, directory, "mismatch.mgo", "package mismatch\n")
	writeCommandFile(t, other, "helper.mgo", "package main\n")
	environment := testCommandEnvironment(t, directory)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "mixed", args: []string{"main.mgo", "."}, want: "cannot be mixed"},
		{name: "different directories", args: []string{"main.mgo", filepath.Join(other, "helper.mgo")}, want: "one directory"},
		{name: "host source", args: []string{"main.go"}, want: "must use .mgo"},
		{name: "mixed source", args: []string{"main.mgo", "helper.go"}, want: "must use .mgo"},
		{name: "test source", args: []string{"main_test.mgo"}, want: "is a test file"},
		{name: "missing entry", args: []string{"library.mgo"}, want: "compilation failed"},
		{name: "package mismatch", args: []string{"main.mgo", "mismatch.mgo"}, want: "compilation failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runProgram(environment, test.args, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run error = %v, want %q", err, test.want)
			}
		})
	}
}
