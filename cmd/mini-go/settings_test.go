package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/cache"
)

func TestCommandSettingsUseStandardDefaults(t *testing.T) {
	temporary := t.TempDir()
	environment := commandEnvironment{tempDir: temporary, getenv: func(string) string { return "" }}
	compiler, err := environment.compilerCacheSettings()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(temporary, "mini-go", "cache"); compiler.root != want {
		t.Fatalf("compiler cache root = %q, want %q", compiler.root, want)
	}
}

func TestCommandSettingsParseEnvironment(t *testing.T) {
	compilerRoot := filepath.Join(t.TempDir(), "compiler")
	values := map[string]string{
		"MINIGO_CACHE": compilerRoot,
		"MINIGO_DEBUG": "cachetrace=1,cachehash=0,cacheverify=1",
	}
	environment := commandEnvironment{getenv: func(name string) string { return values[name] }}
	compiler, err := environment.compilerCacheSettings()
	if err != nil {
		t.Fatal(err)
	}
	if compiler.root != compilerRoot || !compiler.trace || compiler.hash || !compiler.verify {
		t.Fatalf("compiler settings = %#v", compiler)
	}
}

func TestCommandSettingsRejectInvalidEnvironment(t *testing.T) {
	tests := []struct {
		name     string
		variable string
		value    string
	}{
		{name: "compiler relative path", variable: envCacheRoot, value: "relative"},
		{name: "debug shape", variable: envDebug, value: "cachetrace"},
		{name: "debug value", variable: envDebug, value: "cachetrace=true"},
		{name: "debug unknown", variable: envDebug, value: "cache=1"},
		{name: "debug duplicate", variable: envDebug, value: "cachetrace=1,cachetrace=0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := commandEnvironment{
				tempDir: t.TempDir(),
				getenv: func(name string) string {
					if name == test.variable {
						return test.value
					}
					return ""
				},
			}
			_, err := environment.compilerCacheSettings()
			if err == nil {
				t.Fatal("invalid environment was accepted")
			}
		})
	}
}

func TestCacheDebugSettingsTraceEvents(t *testing.T) {
	var output bytes.Buffer
	trace := newCacheTrace(compilerCacheSettings{trace: true, hash: true}, &output)
	trace(cache.Event{Kind: "miss", ModulePath: "example", ActionKey: "action", Reason: "not found"})
	trace(cache.Event{Kind: "prepare_hash", ModulePath: "example", ActionKey: "prepare", MaterialJSON: []byte(`{"root":"example"}`)})
	text := output.String()
	if !strings.Contains(text, "cache miss example") || !strings.Contains(text, "cache prepare_hash example prepare") {
		t.Fatalf("trace output = %q", text)
	}
}
