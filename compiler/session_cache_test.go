package compiler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/target"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestSessionCacheCancellationReleasesLocalLock(t *testing.T) {
	shared := cache.New(cache.NewMemoryBackend())
	session := newSessionCache(shared, cache.TransientConfig{})
	defer session.Close()
	action := cache.NewCompileAction(Identity(), target.Target{}, "example/data", "data", nil, nil)
	release, err := shared.LockCompile(t.Context(), action)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	unlock, err := session.LockCompile(ctx, action)
	release()
	if unlock != nil {
		unlock()
		t.Fatal("acquired held shared lock")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error = %v", err)
	}
	retryCtx, retryCancel := context.WithTimeout(t.Context(), time.Second)
	defer retryCancel()
	unlock, err = session.LockCompile(retryCtx, action)
	if err != nil {
		t.Fatalf("local lock retained after cancel: %v", err)
	}
	unlock()
}

func TestSessionCacheReleasesCompileLockAfterStore(t *testing.T) {
	shared := cache.New(cache.NewMemoryBackend())
	session := newSessionCache(shared, cache.TransientConfig{})
	action := cache.NewCompileAction(Identity(), target.Target{}, "example/data", "data", nil, nil)
	release, err := session.LockCompile(t.Context(), action)
	if err != nil {
		t.Fatal(err)
	}
	artifact := ir.Artifact{
		Format: ir.Format, Version: ir.CurrentVersion,
		Module:    ir.Module{Path: "example/data", Package: "data"},
		OpcodeSet: ir.OpcodeSet,
	}
	data, err := cache.FromArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifactHash, err := ir.Hash(&artifact)
	if err != nil {
		t.Fatal(err)
	}
	symbols := ir.PackageSymbols{ModulePath: artifact.Module.Path, CodeHash: artifactHash}
	if _, err := session.StoreCompile(action, artifact, symbols, data); err != nil {
		t.Fatal(err)
	}
	release()
	second, err := session.LockCompile(t.Context(), action)
	if err != nil {
		t.Fatal(err)
	}
	second()
}

func TestSessionCacheRejectsInvalidActions(t *testing.T) {
	session := newSessionCache(nil, cache.TransientConfig{})
	defer session.Close()
	invalidTarget := target.Target{Tags: []string{"invalid tag"}}
	if _, err := session.LookupCompile(cache.Action{Target: invalidTarget}); err == nil {
		t.Fatal("invalid compile action accepted")
	}
	if _, err := session.LookupCompileManifest(cache.Action{Target: invalidTarget}); err == nil {
		t.Fatal("invalid manifest action accepted")
	}
	if _, err := session.LookupPrepare(cache.PrepareAction{Target: invalidTarget}); err == nil {
		t.Fatal("invalid prepare action accepted")
	}
}

func BenchmarkSessionCacheManifestMiss(b *testing.B) {
	session := newSessionCache(nil, cache.TransientConfig{})
	defer session.Close()
	action := cache.NewCompileAction(Identity(), target.Target{}, "example/data", "data", nil, nil)
	b.ReportAllocs()
	for b.Loop() {
		lookup, err := session.LookupCompileManifest(action)
		if err != nil || lookup.Hit {
			b.Fatalf("lookup = %v, %v", lookup, err)
		}
	}
}
