package integrations_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	minigo "github.com/d7z-team/mini-go"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func newIntegrationEngine(t *testing.T, directory, modulePath string, buildTarget target.Target) *minigo.Engine {
	t.Helper()
	sources, err := workspace.DiscoverIndexedSourceTree(os.DirFS(filepath.Join("testdata", directory)), ".", modulePath)
	if err != nil {
		t.Fatal(err)
	}
	cacheRoot, err := cache.ResolveDiskRoot("")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := minigo.New(minigo.Config{Sources: sources, Target: buildTarget, Cache: cache.NewDiskBackend(cacheRoot)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Errorf("close engine: %v", err)
		}
	})
	return engine
}

func callIntegrationEntry(t *testing.T, program *minigoruntime.Program, entry string, args ...minigoruntime.HostValue) minigoruntime.HostValue {
	t.Helper()
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Call(context.Background(), entry, args...)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Values) != 1 {
		t.Fatalf("entry %q returned %#v", entry, result.Values)
	}
	return result.Values[0]
}

func requireIntegrationInt(t *testing.T, value minigoruntime.HostValue, want int64) {
	t.Helper()
	got, ok := value.Int64()
	if !ok || got != want {
		t.Fatalf("result = %#v, want %d", value, want)
	}
}
