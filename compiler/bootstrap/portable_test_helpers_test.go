package bootstrap

import (
	"context"
	"os"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	miniruntime "github.com/d7z-team/mini-go/runtime"
)

func portableInstance(t *testing.T, text string) *miniruntime.Instance {
	t.Helper()
	base, session, err := newCompilerSession(BuildOptions{Filesystem: os.DirFS("../..")})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	guest, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "probe", Files: []source.File{{Path: "main.mgo", Text: text}}}})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := workspace.MergeSourceSets(base, guest)
	if err != nil {
		t.Fatal(err)
	}
	cacheRoot, err := cache.ResolveDiskRoot("")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := compiler.Prepare(compiler.Request{Context: t.Context(), Root: "probe", Sources: sources, Cache: cache.New(cache.NewDiskBackend(cacheRoot)), EntryPoints: []compiler.EntryPoint{{Name: "default", ModulePath: "probe", Function: "Run"}}})
	if err != nil || prepared.Image == nil {
		t.Fatalf("prepare: %v, %v", err, prepared.Checked.Diagnostics)
	}
	program, err := miniruntime.LoadExecutionImage(*prepared.Image)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := program.Instantiate(context.Background(), miniruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Error(err)
		}
	})
	return instance
}
