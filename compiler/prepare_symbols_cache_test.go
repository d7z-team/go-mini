package compiler_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestPrepareSymbolsAreIndependentOfCachePopulationOrder(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "example/root", Files: []source.File{{Path: "root.mgo", Text: `package root
import "example/extra"
import "example/target"
func Main() int { return extra.Identity(target.Main()) }
`}}},
		{ModulePath: "example/extra", Files: []source.File{{Path: "extra.mgo", Text: `package extra
func Identity[T any](v T) T { return v }
`}}},
		{ModulePath: "example/target", Files: []source.File{{Path: "target.mgo", Text: `package target
import "example/lib"
func Main() int { return lib.Identity(42) }
`}}},
		{ModulePath: "example/lib", Files: []source.File{{Path: "lib.mgo", Text: `package lib
func Identity[T any](v T) T { return v }
`}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for level := compiler.OptimizationNone; level <= compiler.OptimizationFull; level++ {
		var wantImage, wantSymbols []byte
		for _, prewarm := range []string{"", "example/root", "example/target"} {
			backend := cache.NewMemoryBackend()
			roots := []string{"example/target", "example/target"}
			if prewarm != "" {
				roots = append([]string{prewarm}, roots...)
			}
			for _, root := range roots {
				result, err := compiler.Prepare(compiler.Request{
					Root: root, Sources: sources, Cache: cache.New(backend), Symbols: true, Optimization: level,
					EntryPoints: []compiler.EntryPoint{{Name: "default", ModulePath: root, Function: "Main"}},
				})
				if err != nil || result.Image == nil || result.Symbols == nil {
					t.Fatalf("prepare %s O%d (prewarm %q): %v, %v", root, level, prewarm, err, result.Checked.Diagnostics)
				}
				if root != "example/target" {
					continue
				}
				image, err := json.Marshal(result.Image)
				if err != nil {
					t.Fatal(err)
				}
				symbols, err := json.Marshal(result.Symbols)
				if err != nil {
					t.Fatal(err)
				}
				if wantImage == nil {
					wantImage, wantSymbols = image, symbols
				} else if !bytes.Equal(image, wantImage) || !bytes.Equal(symbols, wantSymbols) {
					t.Fatalf("O%d prewarm %q changed target image or symbols", level, prewarm)
				}
			}
		}
	}
}

func TestPrepareCacheTracksSourceOrigins(t *testing.T) {
	backend := cache.NewMemoryBackend()
	var graphHash string
	for _, origin := range []string{"original.go", "renamed.go"} {
		sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
			ModulePath: "example/main",
			Files:      []source.File{{Path: "input.mgo", OriginPath: origin, Text: "package main\nfunc main() { value := 42; _ = value }\n"}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		for attempt := range 2 {
			result, err := compiler.Prepare(compiler.Request{Root: "example/main", Sources: sources, Cache: cache.New(backend), Symbols: true})
			if err != nil || result.Image == nil || result.Symbols == nil {
				t.Fatalf("Prepare = %#v, %v", result, err)
			}
			if err := ir.ValidateProgramSymbols(result.Image, result.Symbols); err != nil {
				t.Fatal(err)
			}
			files := result.Symbols.Packages["example/main"].Files
			if len(files) != 1 || files[0].Path != origin {
				t.Fatalf("symbol files = %v", files)
			}
			if attempt == 0 && result.Checked.GraphHash == graphHash {
				t.Fatal("source origin reused graph identity")
			}
			if attempt == 1 && result.Checked.Stats.PrepareCacheHits != 1 {
				t.Fatalf("warm stats = %+v", result.Checked.Stats)
			}
			graphHash = result.Checked.GraphHash
		}
	}
}

func TestPrepareCacheStoresSymbolsSeparatelyFromCode(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/main",
		Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() { value := 42; _ = value }\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	backend := cache.NewMemoryBackend()
	request := compiler.Request{Root: "example/main", Sources: sources, Cache: cache.New(backend)}
	codeOnly, err := compiler.Prepare(request)
	if err != nil || codeOnly.Image == nil || codeOnly.Symbols != nil {
		t.Fatalf("code-only Prepare = %#v, %v", codeOnly, err)
	}
	request.Cache = cache.New(backend)
	request.Symbols = true
	withSymbols, err := compiler.Prepare(request)
	if err != nil || withSymbols.Image == nil || withSymbols.Symbols == nil || withSymbols.Checked.Stats.PackageCacheHits == 0 {
		t.Fatalf("symbol Prepare = %#v, %v", withSymbols, err)
	}
	if withSymbols.Image.Hash != codeOnly.Image.Hash {
		t.Fatalf("symbols changed execution image hash: %s != %s", withSymbols.Image.Hash, codeOnly.Image.Hash)
	}
	if err := ir.ValidateProgramSymbols(withSymbols.Image, withSymbols.Symbols); err != nil {
		t.Fatal(err)
	}
	request.Cache = cache.New(backend)
	warm, err := compiler.Prepare(request)
	if err != nil || warm.Image == nil || warm.Symbols == nil || warm.Checked.Stats.PrepareCacheHits != 1 {
		t.Fatalf("warm symbol Prepare = %#v, %v", warm, err)
	}
	if withSymbols.Symbols.Hash != warm.Symbols.Hash {
		t.Fatalf("fresh and cached symbol linking differ: %s != %s", withSymbols.Symbols.Hash, warm.Symbols.Hash)
	}
	bad := ir.CloneProgramSymbols(*warm.Symbols)
	pkg := bad.Packages["example/main"]
	pkg.CodeHash = strings.Repeat("0", 64)
	bad.Packages["example/main"] = pkg
	bad.Hash, err = ir.HashProgramSymbols(bad)
	if err != nil {
		t.Fatal(err)
	}
	symbolAction := cache.NewSymbolAction(compiler.Identity(), warm.Image.Hash, warm.Checked.GraphHash, uint8(compiler.OptimizationNone))
	if err := cache.New(backend).StoreSymbols(symbolAction, bad); err != nil {
		t.Fatal(err)
	}
	request.Cache = cache.New(backend)
	repaired, err := compiler.Prepare(request)
	if err != nil || repaired.Image == nil || repaired.Symbols == nil {
		t.Fatalf("repaired symbol Prepare = %#v, %v", repaired, err)
	}
	if err := ir.ValidateProgramSymbols(repaired.Image, repaired.Symbols); err != nil {
		t.Fatalf("Prepare returned corrupt cached symbols: %v", err)
	}
	if repaired.Image.Hash != warm.Image.Hash || repaired.Symbols.Hash == bad.Hash || repaired.Checked.Stats.ImagesLinked != 0 {
		t.Fatalf("symbol repair rebuilt code or retained corruption: %#v", repaired)
	}
	if repaired.Symbols.Hash != warm.Symbols.Hash {
		t.Fatal("symbol reconstruction changed the sidecar")
	}
}

func TestPrepareCacheReusesCodeWhenOnlySourceSymbolsChange(t *testing.T) {
	makeSources := func(text string) workspace.SourceSet {
		sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
			ModulePath: "example/main", Files: []source.File{{Path: "main.mgo", Text: text}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return sources
	}
	backend := cache.NewMemoryBackend()
	first, err := compiler.Prepare(compiler.Request{
		Root: "example/main", Sources: makeSources("package main\nfunc main() { value := 42; _ = value }\n"),
		Cache: cache.New(backend), Symbols: true,
	})
	if err != nil || first.Image == nil || first.Symbols == nil {
		t.Fatalf("initial Prepare = %#v, %v", first, err)
	}
	second, err := compiler.Prepare(compiler.Request{
		Root: "example/main", Sources: makeSources("package main\n\n// main evaluates the same code.\nfunc main() { value := 42; _ = value }\n"),
		Cache: cache.New(backend), Symbols: true,
	})
	if err != nil || second.Image == nil || second.Symbols == nil {
		t.Fatalf("source-only Prepare = %#v, %v", second, err)
	}
	if second.Image.Hash != first.Image.Hash {
		t.Fatalf("source-only edit changed code hash: %s != %s", second.Image.Hash, first.Image.Hash)
	}
	if second.Symbols.Hash == first.Symbols.Hash {
		t.Fatal("source-only edit reused stale program symbols")
	}
	if second.Checked.Stats.PrepareCacheHits != 1 || second.Checked.Stats.ImagesLinked != 0 {
		t.Fatalf("source-only edit relinked code: %#v", second.Checked.Stats)
	}
}

func TestDiskCachePersistsOptimizationAndSymbolOutputs(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/main",
		Files: []source.File{{Path: "main.mgo", Text: `package main
func Value() bool {
	value := true
	unused := false
	if true {
		return value == true
	}
	return unused
}
`}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cacheRoot := t.TempDir()
	prepare := func(level compiler.OptimizationLevel, symbols bool) compiler.PrepareResult {
		t.Helper()
		session, err := compiler.New(compiler.Options{
			Sources:      sources,
			Cache:        cache.New(cache.NewDiskBackend(cacheRoot)),
			Optimization: level,
			Symbols:      symbols,
		})
		if err != nil {
			t.Fatal(err)
		}
		result, err := session.Prepare("example/main", []compiler.EntryPoint{{Name: "value", ModulePath: "example/main", Function: "Value"}})
		if err != nil || result.Image == nil || !result.Checked.OK() {
			t.Fatalf("Prepare O%d/symbols=%t: diagnostics=%#v err=%v", level, symbols, result.Checked.Diagnostics, err)
		}
		return result
	}

	for level := compiler.OptimizationNone; level <= compiler.OptimizationFull; level++ {
		codeOnly := prepare(level, false)
		withSymbols := prepare(level, true)
		if codeOnly.Image.Hash != withSymbols.Image.Hash || codeOnly.Symbols != nil || withSymbols.Symbols == nil {
			t.Fatalf("O%d output separation failed: code=%#v symbols=%#v", level, codeOnly, withSymbols)
		}
		warm := prepare(level, true)
		if warm.Image.Hash != withSymbols.Image.Hash || warm.Symbols == nil || warm.Checked.Stats.PrepareCacheHits != 1 || warm.Checked.Stats.ImagesLinked != 0 {
			t.Fatalf("warm O%d: hash=%s want=%s stats=%#v", level, warm.Image.Hash, withSymbols.Image.Hash, warm.Checked.Stats)
		}
	}
}

func TestPrepareCacheVerifyDetectsDifferentSymbols(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/main",
		Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() {}\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	shared := cache.New(cache.NewMemoryBackend())
	first, err := compiler.Prepare(compiler.Request{Root: "example/main", Sources: sources, Cache: shared, Symbols: true})
	if err != nil || first.Image == nil || first.Symbols == nil {
		t.Fatalf("first Prepare = %#v, %v", first, err)
	}
	wrong := ir.CloneProgramSymbols(*first.Symbols)
	pkg := wrong.Packages["example/main"]
	pkg.Functions[0].Name = "wrong"
	wrong.Packages["example/main"] = pkg
	wrong.Hash, err = ir.HashProgramSymbols(wrong)
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Prepare(compiler.Request{
		Root: "example/main", Sources: sources, Cache: symbolOverrideCache{Cache: shared, symbols: wrong}, Symbols: true, CacheVerify: true,
	})
	if err == nil || !strings.Contains(err.Error(), "symbol cache verify failed") {
		t.Fatalf("symbol verification error = %v", err)
	}
}

type symbolOverrideCache struct {
	cache.Cache
	symbols ir.ProgramSymbols
}

func (c symbolOverrideCache) LookupSymbols(cache.SymbolAction) (cache.SymbolLookup, error) {
	return cache.SymbolLookup{Symbols: c.symbols, Hit: true, Reason: "test override"}, nil
}
