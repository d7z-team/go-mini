package dap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/d7z-team/mini-go/compiler/workspace"
	protocol "github.com/google/go-dap"
)

func TestBreakpointUsesRegisteredLibraryIdentity(t *testing.T) {
	app, library := t.TempDir(), t.TempDir()
	filename := filepath.Join(library, "value.mgo")
	if err := os.WriteFile(filename, []byte("package rules\nfunc Value() int { return 42 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := workspace.LoadSources(t.Context(), app, "app", []workspace.DirectorySource{{Module: "rules", Directory: library}})
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{target: LaunchTarget{RootPath: app, ModulePath: "app", Locations: loaded.Locations}}
	module, file, err := session.sourceIdentity(protocol.Source{Path: filename})
	if err != nil || module != "rules" || file != "value.mgo" {
		t.Fatalf("identity: %q %q %v", module, file, err)
	}
	if _, _, err := session.sourceIdentity(protocol.Source{Path: filepath.Join(library, "missing.mgo")}); err == nil {
		t.Fatal("unregistered breakpoint accepted")
	}
}
