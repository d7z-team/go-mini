package rpccheck

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/rpc"
	"github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/runtime/bytecode"
	"github.com/d7z-team/mini-go/stdlib"
	"github.com/d7z-team/mini-go/tooling/fixtures"
	"github.com/d7z-team/mini-go/tooling/mrpc"
)

// GenerateFixtures prepares small images on the host; ordinary wire fixtures
// stay handwritten and independent of compiler identity.
func GenerateFixtures(root fs.FS) (map[string][]byte, error) {
	inputs := make(map[string][]byte)
	var files []workspace.TreeFile
	err := fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name == "images" {
				return fs.SkipDir
			}
			return nil
		}
		if name == "manifest.json" || strings.HasSuffix(name, ".md") {
			return nil
		}
		data, err := fs.ReadFile(root, name)
		if err != nil {
			return err
		}
		inputs[name] = data
		if strings.HasSuffix(name, ".mgo") {
			files = append(files, workspace.TreeFile{Path: name, Text: string(data)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sources, err := workspace.NewTreeSourceSet("rpcfixture", files)
	if err != nil {
		return nil, err
	}
	standard, err := workspace.StandardLibrary(stdlib.Open())
	if err != nil {
		return nil, err
	}
	merged, err := workspace.MergeSourceSets(sources, standard)
	if err != nil {
		return nil, err
	}
	cacheRoot, err := cache.ResolveDiskRoot("")
	if err != nil {
		return nil, err
	}
	session, err := compiler.New(compiler.Options{Sources: merged, Cache: cache.New(cache.NewDiskBackend(cacheRoot)), Optimization: compiler.OptimizationFull})
	if err != nil {
		return nil, err
	}
	defer session.Close()
	outputs := make(map[string][]byte)
	cases, err := fs.ReadDir(root, "source")
	if err != nil {
		return nil, err
	}
	type imageCase struct {
		name, source string
		optimization compiler.OptimizationLevel
	}
	var images []imageCase
	for _, entry := range cases {
		if !entry.IsDir() {
			continue
		}
		images = append(images, imageCase{entry.Name(), entry.Name(), compiler.OptimizationFull})
		if entry.Name() == "provider" {
			// A second compiler profile changes the revision while retaining
			// the same source-level service and resource contracts.
			images = append(images, imageCase{"provider-patch", entry.Name(), compiler.OptimizationNone})
		}
	}
	for _, fixture := range images {
		name := fixture.name
		module := "rpcfixture/source/" + fixture.source
		compilerSession := session
		if fixture.optimization != compiler.OptimizationFull {
			compilerSession, err = compiler.New(compiler.Options{Sources: merged, Cache: cache.New(cache.NewDiskBackend(cacheRoot)), Optimization: fixture.optimization})
			if err != nil {
				return nil, err
			}
		}
		prepared, prepareErr := compilerSession.Prepare(module, []compiler.EntryPoint{{Name: "default", ModulePath: module, Function: "Main"}})
		if compilerSession != session {
			compilerSession.Close()
		}
		err := prepareErr
		if err != nil {
			return nil, err
		}
		if !prepared.Checked.OK() || prepared.Image == nil {
			return nil, fmt.Errorf("RPC image %s: %v", name, prepared.Checked.Diagnostics)
		}
		if _, err := runtime.LoadExecutionImage(*prepared.Image); err != nil {
			return nil, fmt.Errorf("fixture %s: %w", name, err)
		}
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(prepared.Image); err != nil {
			return nil, err
		}
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(encoded.Bytes()); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		outputs[path.Join("images", name+".json.gz")] = compressed.Bytes()
	}
	for name, data := range outputs {
		inputs[name] = data
	}
	manifest := fixtures.Describe(inputs, func(name string) fixtures.Entry {
		entry := fixtures.Entry{Kind: "golden", Source: name, Oracle: "reviewed protocol contract"}
		switch {
		case strings.HasPrefix(name, "images/"):
			entry = fixtures.Entry{Kind: "image", Source: "source/" + strings.TrimSuffix(path.Base(name), ".json.gz"), Oracle: "independent Go and Rust execution", CompilerID: bytecode.CompilerIdentity}
			if name == "images/provider-patch.json.gz" {
				entry.Source = "source/provider"
				entry.Oracle = "unoptimized revision with the same RPC contract"
			}
		case strings.HasPrefix(name, "generated/"):
			entry = fixtures.Entry{Kind: "binding", Source: "schema/", Oracle: "MRPC catalog and generator"}
		case strings.HasPrefix(name, "source/"):
			entry.Kind = "source"
		case strings.HasPrefix(name, "schema/"):
			entry.Kind = "schema"
		case strings.HasPrefix(name, "tls/"):
			entry.Kind = "test-credential"
		case strings.Contains(name, "scenarios"):
			entry.Kind = "scenario"
			entry.Oracle = "handwritten observable results"
		}
		return entry
	})
	manifest.Protocols = map[string]string{"mrpc": mrpc.ProtocolDomain, "rpc": rpc.ContractProtocol, "ffi": rpc.FFIProtocol, "endpoint": rpc.EndpointProtocol}
	if err := manifest.AddInputs("stdlib/src/", stdlib.Open()); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	outputs["manifest.json"] = append(data, '\n')
	return outputs, nil
}
