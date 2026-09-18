package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandRequiresValidSubcommandInputs(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{nil, "check|run|test"},
		{[]string{"unknown"}, "unknown command"},
		{[]string{"test", "-module", "example", "-run", "["}, "invalid -run expression"},
		{[]string{"test", "-module", "example", "-skip", "["}, "invalid -skip expression"},
		{[]string{"test", "-module", "example", "-count", "0"}, "-count must be at least 1"},
	}
	for _, test := range tests {
		var stdout, stderr bytes.Buffer
		err := runCLI(test.args, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("runCLI(%q) error = %v, want substring %q", test.args, err, test.want)
		}
		if stdout.Len() != 0 {
			t.Fatalf("runCLI(%q) wrote stdout %q", test.args, stdout.String())
		}
	}
}

func TestTestCommandSelectsTestsAtExecution(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "value.mgo"), []byte("package example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	emptyDir := filepath.Join(dir, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(emptyDir, "value.mgo"), []byte("package empty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testSource := `package example
import "testing"
func TestAlpha(t *testing.T) { t.Fatal("must be filtered") }
func TestBeta(t *testing.T) {}
`
	if err := os.WriteFile(filepath.Join(dir, "value_test.mgo"), []byte(testSource), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(dir, "cache")
	t.Setenv(envCacheRoot, cacheDir)
	var stdout, stderr bytes.Buffer
	if err := runCLI([]string{"-C", dir, "test", "-module", "example", "-run", "^TestBeta$", "."}, &stdout, &stderr); err != nil {
		t.Fatalf("selected test failed: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ok\texample") {
		t.Fatalf("selected test output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCLI([]string{"-C", dir, "test", "-module", "example", "-run", "^TestBeta$", "./..."}, &stdout, &stderr); err != nil {
		t.Fatalf("recursive selected test failed: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "?\texample/empty\t[no test files]") {
		t.Fatalf("recursive test output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCLI([]string{"-C", dir, "test", "-module", "example", "-run", "^TestAlpha$", "-skip", "Alpha", "."}, &stdout, &stderr); err != nil {
		t.Fatalf("empty test selection failed: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ok\texample") {
		t.Fatalf("empty test selection output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCLI([]string{"-C", dir, "test", "-module", "example", "."}, &stdout, &stderr); err == nil || !strings.Contains(stderr.String(), "FAIL\texample") {
		t.Fatalf("full test error=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func TestCheckAndRunWorkspace(t *testing.T) {
	dir := t.TempDir()
	commandDir := filepath.Join(dir, "cmd", "hello")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(commandDir, "main.mgo"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	childDir := filepath.Join(commandDir, "child")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childDir, "value.mgo"), []byte("package child\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cacheDir := filepath.Join(dir, "cache")
	t.Setenv(envCacheRoot, cacheDir)
	if err := runCLI([]string{"-C", commandDir, "check", "./..."}, &stdout, &stderr); err != nil {
		t.Fatalf("check failed: %v; stderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("successful check wrote %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCLI([]string{"-C", commandDir, "run"}, &stdout, &stderr); err != nil {
		t.Fatalf("run failed: %v; stderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("run output = %q", stdout.String())
	}
}
