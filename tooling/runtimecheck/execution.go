package runtimecheck

import (
	"context"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/runtime/bytecode"
	"github.com/d7z-team/mini-go/stdlib"
)

// ExecutionVector contains the actual Go compiler output and VM observation.
type ExecutionVector struct {
	Name          string                     `json:"name"`
	Optimization  compiler.OptimizationLevel `json:"optimization"`
	Image         *bytecode.ExecutionImage   `json:"image"`
	Symbols       *bytecode.ProgramSymbols   `json:"symbols"`
	ResultType    string                     `json:"result_type"`
	ResultInteger string                     `json:"result_integer"`
}

// GenerateExecutionVectors compiles and runs a small deterministic corpus at
// every optimization level. Compiler/runtime errors abort the whole output.
func GenerateExecutionVectors(root fs.FS) ([]byte, error) {
	standard, err := workspace.StandardLibrary(stdlib.Open())
	if err != nil {
		return nil, err
	}
	cacheRoot, err := cache.ResolveDiskRoot("")
	if err != nil {
		return nil, err
	}
	backend := cache.New(cache.NewDiskBackend(cacheRoot))
	var vectors []ExecutionVector
	cases, err := fs.ReadDir(root, ".")
	if err != nil {
		return nil, err
	}
	for _, entry := range cases {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		caseRoot, err := fs.Sub(root, name)
		if err != nil {
			return nil, err
		}
		var files []workspace.TreeFile
		err = fs.WalkDir(caseRoot, ".", func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".mgo") {
				return nil
			}
			text, err := fs.ReadFile(caseRoot, path)
			if err == nil {
				files = append(files, workspace.TreeFile{Path: path, Text: string(text)})
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		for level := compiler.OptimizationNone; level <= compiler.OptimizationFull; level++ {
			sources, err := workspace.NewTreeSourceSet("runtime/oracle", files)
			if err != nil {
				return nil, err
			}
			var compileSources workspace.SourceSet = sources
			if strings.HasPrefix(name, "stdlib_") {
				compileSources, err = workspace.MergeSourceSets(sources, standard)
				if err != nil {
					return nil, err
				}
			}
			session, err := compiler.New(compiler.Options{Sources: compileSources, Cache: backend, Optimization: level, Symbols: true})
			if err != nil {
				return nil, err
			}
			prepared, err := session.Prepare("runtime/oracle", []compiler.EntryPoint{{Name: "default", ModulePath: "runtime/oracle", Function: "Main"}})
			session.Close()
			if err != nil {
				return nil, err
			}
			if !prepared.Checked.OK() || prepared.Image == nil {
				return nil, fmt.Errorf("compile oracle %s: %v", name, prepared.Checked.Diagnostics)
			}
			program, err := runtime.LoadExecutionImage(*prepared.Image)
			if err != nil {
				return nil, err
			}
			instance, err := program.Instantiate(context.Background(), runtime.InstanceOptions{})
			if err != nil {
				return nil, err
			}
			execution, runErr := instance.Start("default")
			var result runtime.RunResult
			if runErr == nil {
				for remaining := 10_000_000; remaining > 0; {
					state, steps, pollErr := execution.PollSteps(min(remaining, 4096))
					remaining -= steps
					if pollErr != nil {
						runErr = pollErr
						break
					}
					if state == runtime.ExecutionCompleted {
						result, runErr = execution.Result()
						break
					}
					if state != runtime.ExecutionRunning || steps == 0 || remaining == 0 {
						runErr = fmt.Errorf("oracle %s did not complete: state=%s, remaining=%d", name, state, remaining)
						break
					}
				}
			}
			closeErr := instance.Close()
			if runErr != nil {
				return nil, runErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if len(result.Values) != 1 {
				return nil, fmt.Errorf("oracle %s returned %d values", name, len(result.Values))
			}
			value, ok := result.Values[0].Int64()
			if !ok {
				return nil, fmt.Errorf("oracle %s returned %s", name, result.Values[0].Type())
			}
			vectors = append(vectors, ExecutionVector{Name: name, Optimization: level, Image: prepared.Image, Symbols: prepared.Symbols, ResultType: result.Values[0].Type(), ResultInteger: strconv.FormatInt(value, 10)})
		}
	}
	return encodeJSON(vectors)
}
