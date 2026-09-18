package main

import (
	"context"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/tooling/dap"
)

func TestDAPLaunchReportsMissingImportedMember(t *testing.T) {
	directory := t.TempDir()
	writeCommandFile(t, directory, "lib/lib.mgo", "package lib\n")
	writeCommandFile(t, directory, "main.mgo", "package main\nimport \"example.com/dap/lib\"\nfunc main() { lib.Missing() }\n")
	target, err := launchDAPTarget(context.Background(), testCommandEnvironment(t, directory), dap.LaunchConfig{Module: "example.com/dap"})
	if target.Cleanup != nil {
		defer target.Cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "semantic.import.member.missing") || target.Program != nil {
		t.Fatalf("launch target=%#v error=%v", target, err)
	}
}

func TestDAPLaunchesNamedSourceFile(t *testing.T) {
	directory := t.TempDir()
	writeCommandFile(t, directory, "main.mgo", "package main\nfunc main() {}\n")
	target, err := launchDAPTarget(context.Background(), testCommandEnvironment(t, directory), dap.LaunchConfig{Root: "main.mgo"})
	if err != nil {
		t.Fatal(err)
	}
	if target.Program == nil || target.RootPath != directory || target.ModulePath != workspace.CommandLinePackage {
		t.Fatalf("target = %#v", target)
	}
	if target.Cleanup == nil {
		t.Fatal("target has no cleanup")
	}
	if err := target.Cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchDAPTargetCapturesConsoleOutput(t *testing.T) {
	directory := t.TempDir()
	writeCommandFile(t, directory, "main.mgo", "package main\nfunc main() { println(\"debug\") }\n")
	target, err := launchDAPTarget(context.Background(), testCommandEnvironment(t, directory), dap.LaunchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer target.Cleanup()
	if _, err := target.Program.RunWithOptions(target.Options); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, _, _, err := target.Output.ReadOutput(0, 0)
	if err != nil || stdout != "debug\n" || stderr != "" {
		t.Fatalf("DAP output = %q, %q, %v", stdout, stderr, err)
	}
}
