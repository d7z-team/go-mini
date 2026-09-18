package runtimecheck

import (
	"fmt"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

// CompileBlock prepares a reusable execution image on the Go host.
func CompileBlock(module, source string) ([]byte, error) {
	sources, err := workspace.NewTreeSourceSet(module, []workspace.TreeFile{{Path: "main.mgo", Text: source}})
	if err != nil {
		return nil, err
	}
	session, err := compiler.New(compiler.Options{Sources: sources, Optimization: compiler.OptimizationFull})
	if err != nil {
		return nil, err
	}
	defer session.Close()
	prepared, err := session.Prepare(module, []compiler.EntryPoint{{Name: "default", ModulePath: module, Function: "Main"}})
	if err != nil {
		return nil, err
	}
	if !prepared.Checked.OK() || prepared.Image == nil {
		return nil, fmt.Errorf("compile block %s: %v", module, prepared.Checked.Diagnostics)
	}
	return encodeJSON(prepared.Image)
}
