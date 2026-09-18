package main

import (
	"bytes"
	"testing"
)

func TestSourceMappingsCompileRunAndDocument(t *testing.T) {
	app, dependency := t.TempDir(), t.TempDir()
	writeCommandFile(t, app, "main.mgo", "package main\nimport (\"company/rules\"; \"fmt\")\nfunc main() { fmt.Print(rules.Value()) }\n")
	writeCommandFile(t, dependency, "value.mgo", "package rules\nfunc Value() int { return 42 }\n")
	for _, command := range []string{"check", "run", "doc"} {
		var stdout, stderr bytes.Buffer
		args := []string{"-C", app, command, "-module", "app", "-source", "company/rules=" + dependency}
		if command == "doc" {
			args = append(args, "-out", t.TempDir()+"/reference")
		}
		args = append(args, ".")
		if err := runCLI(args, &stdout, &stderr); err != nil {
			t.Fatalf("%s: %v %s", command, err, stderr.String())
		}
		if command == "run" && stdout.String() != "42" {
			t.Fatalf("output: %q", stdout.String())
		}
	}
}
