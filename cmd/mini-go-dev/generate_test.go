package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestContractSpecWritesCurrentContract(t *testing.T) {
	output := filepath.Join(t.TempDir(), "bytecode.json")
	if err := runDevCLI([]string{"contract-spec", "-out", output}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var spec bytecode.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Format != bytecode.Format || spec.Version != bytecode.CurrentVersion || spec.OpcodeSet != bytecode.OpcodeSet {
		t.Fatalf("generated stale contract: %#v", spec)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	if err := runDevCLI([]string{"unknown"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("unknown command accepted")
	}
}
