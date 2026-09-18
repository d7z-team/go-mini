package fixtures

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestDistributedManifests(t *testing.T) {
	for _, name := range []string{"rpc", "runtime", "stdlib-host"} {
		t.Run(name, func(t *testing.T) {
			root := os.DirFS("../../testdata/" + name)
			data, err := os.ReadFile("../../testdata/" + name + "/manifest.json")
			if err != nil {
				t.Fatal(err)
			}
			var manifest Manifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			if err := manifest.Validate(root); err != nil {
				t.Fatal(err)
			}
			if err := manifest.ValidateInputs(os.DirFS("../..")); err != nil {
				t.Fatal(err)
			}
			for path, entry := range manifest.Files {
				if entry.CompilerID != "" && entry.CompilerID != bytecode.CompilerIdentity {
					t.Fatalf("%s: stale compiler identity", path)
				}
			}
		})
	}
}

func TestManifestDetectsCorruptMissingAndUnlistedInputs(t *testing.T) {
	manifest := Describe(map[string][]byte{"input": []byte("valid")}, func(string) Entry { return Entry{Kind: "source", Source: "input", Oracle: "handwritten"} })
	for _, root := range []fstest.MapFS{
		{"input": {Data: []byte("corrupt")}},
		{},
		{"input": {Data: []byte("valid")}, "extra": {Data: []byte("unlisted")}},
	} {
		if err := manifest.Validate(root); err == nil {
			t.Fatal("invalid corpus accepted")
		}
	}
	if err := manifest.Validate(fstest.MapFS{"input": {Data: []byte("valid")}}); err != nil {
		t.Fatal(err)
	}
}

func TestManifestInputSelection(t *testing.T) {
	root := fstest.MapFS{
		"nested/input":     {Data: []byte("valid")},
		"nested/README.md": {Data: []byte("documentation")},
		"manifest.json":    {Data: []byte("manifest")},
	}
	files, err := ReadTree(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Describe(files, func(name string) Entry {
		return Entry{Kind: "source", Source: name, Oracle: "handwritten"}
	})
	if err := manifest.Validate(root); err != nil {
		t.Fatal(err)
	}
	if _, ok := files["nested/input"]; !ok {
		t.Fatal("nested input missing")
	}
	for _, name := range []string{"nested/README.md", "manifest.json"} {
		if _, ok := files[name]; ok {
			t.Fatalf("metadata included as fixture: %s", name)
		}
	}
}

func TestManifestClosesFilesOnReadAndHashFailures(t *testing.T) {
	manifest := Describe(map[string][]byte{"input": []byte("valid")}, func(string) Entry {
		return Entry{Kind: "source", Source: "input", Oracle: "handwritten"}
	})
	readError := errors.New("injected read failure")
	for _, test := range []struct {
		name      string
		data      string
		readErr   error
		wantError string
	}{
		{name: "valid", data: "valid"},
		{name: "hash mismatch", data: "corrupt", wantError: "fixture hash mismatch"},
		{name: "read failure", data: "valid", readErr: readError, wantError: readError.Error()},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := &trackedFixtureFS{MapFS: fstest.MapFS{"input": {Data: []byte(test.data)}}, readErr: test.readErr}
			err := manifest.Validate(root)
			if test.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate = %v, want %q", err, test.wantError)
			}
			if test.readErr != nil && !errors.Is(err, test.readErr) {
				t.Fatalf("read error identity lost: %v", err)
			}
			if root.open != 0 {
				t.Fatalf("%d files left open", root.open)
			}
		})
	}
}
