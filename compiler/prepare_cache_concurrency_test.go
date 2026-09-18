package compiler_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

type coordinatedPrepareCache struct {
	cache.Cache
	attempts      atomic.Int32
	firstLocked   chan struct{}
	secondAttempt chan struct{}
	proceed       chan struct{}
}

func (c *coordinatedPrepareCache) LockPrepare(ctx context.Context, action cache.PrepareAction) (func(), error) {
	attempt := c.attempts.Add(1)
	if attempt == 2 {
		close(c.secondAttempt)
	}
	release, err := c.Cache.LockPrepare(ctx, action)
	if err != nil {
		return nil, err
	}
	if attempt == 1 {
		close(c.firstLocked)
		<-c.proceed
	}
	return release, nil
}

func TestConcurrentPrepareSharedHitHonorsOutputOptions(t *testing.T) {
	for _, test := range []struct {
		name          string
		firstSymbols  bool
		secondSymbols bool
		secondVerify  bool
	}{
		{name: "symbols", firstSymbols: true, secondSymbols: true},
		{name: "verify", secondVerify: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
				ModulePath: "example/main",
				Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() { value := 42; _ = value }\n"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			shared := &coordinatedPrepareCache{
				Cache: cache.New(cache.NewMemoryBackend()), firstLocked: make(chan struct{}),
				secondAttempt: make(chan struct{}), proceed: make(chan struct{}),
			}
			type outcome struct {
				result compiler.PrepareResult
				err    error
			}
			firstResult := make(chan outcome, 1)
			secondResult := make(chan outcome, 1)
			go func() {
				result, err := compiler.Prepare(compiler.Request{Root: "example/main", Sources: sources, Cache: shared, Symbols: test.firstSymbols})
				firstResult <- outcome{result: result, err: err}
			}()
			<-shared.firstLocked
			go func() {
				result, err := compiler.Prepare(compiler.Request{
					Root: "example/main", Sources: sources, Cache: shared,
					Symbols: test.secondSymbols, CacheVerify: test.secondVerify,
				})
				secondResult <- outcome{result: result, err: err}
			}()
			<-shared.secondAttempt
			close(shared.proceed)
			first, second := <-firstResult, <-secondResult
			if first.err != nil || first.result.Image == nil {
				t.Fatalf("first Prepare = %#v, %v", first.result, first.err)
			}
			if second.err != nil || second.result.Image == nil || second.result.Image.Hash != first.result.Image.Hash {
				t.Fatalf("shared Prepare = %#v, %v", second.result, second.err)
			}
			if second.result.Checked.Stats.PrepareCacheHits != 1 {
				t.Fatalf("shared Prepare did not report a cache hit: %#v", second.result.Checked.Stats)
			}
			if test.secondSymbols {
				if second.result.Symbols == nil {
					t.Fatal("shared Prepare discarded requested symbols")
				}
				if err := ir.ValidateProgramSymbols(second.result.Image, second.result.Symbols); err != nil {
					t.Fatal(err)
				}
			}
			if test.secondVerify && second.result.Checked.Stats.ImagesLinked != 1 {
				t.Fatalf("shared Prepare bypassed cache verification: %#v", second.result.Checked.Stats)
			}
		})
	}
}
