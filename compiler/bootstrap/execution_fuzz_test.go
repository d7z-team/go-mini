package bootstrap_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func FuzzNativeCompileAndVMExecution(f *testing.F) {
	for _, seed := range [][]byte{{0, 2, 9}, {1, 11}, {2, 3, 5, 8}, {3, 7, 4}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32 {
			t.Skip()
		}
		sourceText, want := generatedExecutionCase(data)
		sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
			ModulePath: "generated/main",
			Files:      []source.File{{Path: "main.mgo", Text: sourceText}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		buildCache := cache.NewTransient(cache.TransientConfig{MaxEntries: 8, MaxBytes: 32 << 20})
		defer buildCache.Close()
		for level := compiler.OptimizationNone; level <= compiler.OptimizationFull; level++ {
			prepared, err := compiler.Prepare(compiler.Request{
				Root: "generated/main", Sources: sources, Cache: buildCache, Optimization: level,
				EntryPoints: []compiler.EntryPoint{{Name: "result", ModulePath: "generated/main", Function: "Result"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !prepared.Checked.OK() || prepared.Image == nil {
				t.Fatalf("O%d generated program did not compile: %#v\n%s", level, prepared.Checked.Diagnostics, sourceText)
			}
			program, err := minigoruntime.LoadExecutionImage(*prepared.Image)
			if err != nil {
				t.Fatal(err)
			}
			instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
			if err != nil {
				t.Fatal(err)
			}
			result, callErr := instance.Call(context.Background(), "result")
			closeErr := instance.Close()
			if callErr != nil || closeErr != nil {
				t.Fatalf("O%d execute: call=%v close=%v", level, callErr, closeErr)
			}
			if len(result.Values) != 1 {
				t.Fatalf("O%d Result returned %#v", level, result.Values)
			}
			got, ok := result.Values[0].Int64()
			if !ok || got != int64(want) {
				t.Fatalf("O%d Result() = %#v, want %d\n%s", level, result.Values[0], want, sourceText)
			}
		}
	})
}

func generatedExecutionCase(data []byte) (string, int) {
	byteAt := func(index int) int {
		if index >= len(data) {
			return 0
		}
		return int(data[index])
	}
	a := byteAt(1)%21 - 10
	b := byteAt(2)%21 - 10
	c := byteAt(3)%21 - 10
	switch byteAt(0) % 4 {
	case 0:
		want := b*2 - a
		if a > b {
			want = a*2 - b
		}
		return fmt.Sprintf("package main\nfunc Result() int { a, b := %d, %d; if a > b { return a*2 - b }; return b*2 - a }\n", a, b), want
	case 1:
		n := byteAt(1) % 21
		want := 0
		for i := 0; i < n; i++ {
			if i%2 == 0 {
				want += i
			} else {
				want -= i
			}
		}
		return fmt.Sprintf("package main\nfunc Result() int { total := 0; for i := 0; i < %d; i++ { if i%%2 == 0 { total += i } else { total -= i } }; return total }\n", n), want
	case 2:
		return fmt.Sprintf("package main\nfunc Result() int { values := []int{%d, %d, %d}; lookup := map[string]int{\"a\": values[0], \"c\": values[2]}; values[1] += lookup[\"a\"]; return values[1] + lookup[\"c\"] }\n", a, b, c), a + b + c
	default:
		return fmt.Sprintf(`package main
type Pair struct { Left int; Right int }
func (p Pair) Total() int { return p.Left + p.Right }
func Identity[T any](value T) T { return value }
func Result() int { pair := Identity(Pair{Left: %d, Right: %d}); return pair.Total() }
`, a, b), a + b
	}
}
