package bootstrap

import (
	"os"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/format"
	miniruntime "github.com/d7z-team/mini-go/runtime"
)

func TestPortableCompilerLibraries(t *testing.T) {
	sources, session, err := newCompilerSession(BuildOptions{Filesystem: os.DirFS("../..")})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, root := range []string{
		compilerModulePath + "/analysis", compilerModulePath + "/doc", compilerModulePath + "/format",
		compilerModulePath + "/language", compilerModulePath + "/service",
	} {
		t.Run(root, func(t *testing.T) {
			result, err := compiler.Check(compiler.Request{Context: t.Context(), Root: root, Sources: sources})
			if err != nil || !result.OK() {
				t.Fatalf("portable source check: %v; %v", err, result.Diagnostics)
			}
		})
	}
}

func TestPortableToolProjectionExecutes(t *testing.T) {
	instance := portableInstance(t, `package probe
import (
 "github.com/d7z-team/mini-go/compiler/analysis"
 "github.com/d7z-team/mini-go/compiler/doc"
 "github.com/d7z-team/mini-go/compiler/format"
 "github.com/d7z-team/mini-go/compiler/source"
 "github.com/d7z-team/mini-go/compiler/workspace"
)
func Run(text string) string {
 formatted := format.Source("sample", "sample.mgo", text)
 if source.HasErrors(formatted.Diagnostics) {return "format failed"}
 sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath:"sample",Files:[]source.File{{Path:"sample.mgo",Text:formatted.Text}}}})
 if err != nil {return err.Error()}
 facts, err := analysis.CheckWorkspace(analysis.WorkspaceRequest{Root:"sample",Sources:sources})
 if err != nil {return err.Error()}
 if source.HasErrors(facts.Diagnostics) {return "analysis failed"}
 catalog, err := doc.Build(doc.BuildRequest{Workspace:facts,Packages:[]string{"sample"}})
 if err != nil {return err.Error()}
 comment, ok := catalog.LookupComment("sample", "", "Answer")
 if !ok {return "missing comment"}
 return comment.Text+"\n"+formatted.Text
}`)
	const input = "package sample\n// Answer returns the value.\nfunc Answer()int{return 42}\n"
	result, err := instance.CallEntry(t.Context(), miniruntime.HostString(input))
	if err != nil {
		t.Fatal(err)
	}
	actual, ok := result.Values[0].StringValue()
	want := "Answer returns the value.\n" + format.Source("sample", "sample.mgo", input).Text
	if !ok || actual != want {
		t.Fatalf("tool projection: got %q, want %q", actual, want)
	}
}
