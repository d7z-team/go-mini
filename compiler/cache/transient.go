package cache

import (
	"context"
	"errors"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/types"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

// TransientConfig bounds structured compiler state owned by a live service.
type TransientConfig struct {
	MaxEntries int
	MaxBytes   int64
}

// TransientStats reports observable cache behavior and current ownership.
type TransientStats struct {
	Hits, Misses, Stores, Evictions, Clears uint64
	Entries                                 int
	// Bytes is the estimated memory owned by cached structured values.
	Bytes int64
}

type transientCompileEntry struct {
	lookup   Lookup
	manifest Manifest
	size     int64
}

type transientPrepareEntry struct {
	lookup  PrepareLookup
	symbols *ir.ProgramSymbols
	size    int64
}

type transientOrderEntry struct {
	key       string
	isPrepare bool
}

// TransientCache keeps validated structured values without persistent JSON
// round trips. It is bounded and independent from disk cache storage.
type TransientCache struct {
	mu      cacheMutex
	config  TransientConfig
	compile map[string]transientCompileEntry
	prepare map[string]transientPrepareEntry
	order   []transientOrderEntry
	locks   actionLocks
	bytes   int64
	stats   TransientStats
}

func NewTransient(config TransientConfig) *TransientCache {
	if config.MaxEntries <= 0 {
		config.MaxEntries = 128
	}
	if config.MaxBytes <= 0 {
		config.MaxBytes = 256 << 20
	}
	return &TransientCache{
		config: config, compile: make(map[string]transientCompileEntry),
		prepare: make(map[string]transientPrepareEntry),
	}
}

func (c *TransientCache) LookupCompileManifest(action Action) (ManifestLookup, error) {
	key, err := action.Key()
	if err != nil {
		return ManifestLookup{}, err
	}
	c.mu.Lock()
	entry, ok := c.compile[key]
	if ok {
		c.stats.Hits++
	} else {
		c.stats.Misses++
	}
	c.mu.Unlock()
	if !ok {
		return ManifestLookup{Reason: "transient action missing"}, nil
	}
	manifest := entry.manifest
	manifest.RuntimeDependencies = append([]string(nil), manifest.RuntimeDependencies...)
	return ManifestLookup{Manifest: manifest, Hit: true, Reason: "transient hit"}, nil
}

func (c *TransientCache) LookupCompile(action Action) (Lookup, error) {
	key, err := action.Key()
	if err != nil {
		return Lookup{}, err
	}
	c.mu.Lock()
	entry, ok := c.compile[key]
	if ok {
		c.stats.Hits++
	} else {
		c.stats.Misses++
	}
	c.mu.Unlock()
	if !ok {
		return Lookup{Reason: "transient action missing"}, nil
	}
	lookup := entry.lookup
	lookup.Artifact = ir.CloneArtifact(lookup.Artifact)
	lookup.Symbols = ir.ClonePackageSymbols(lookup.Symbols)
	lookup.ExportData = clonePackageData(lookup.ExportData)
	lookup.Hit = true
	lookup.Reason = "transient hit"
	return lookup, nil
}

func (c *TransientCache) StoreCompile(action Action, artifact ir.Artifact, symbols ir.PackageSymbols, data PackageData) (Manifest, error) {
	if artifact.Module.Path != action.ModulePath || artifact.Module.Package != action.Package || !artifactRequirementsUnbound(artifact) {
		return Manifest{}, errors.New("artifact does not match cache action")
	}
	if err := data.Validate(); err != nil {
		return Manifest{}, err
	}
	key, err := action.Key()
	if err != nil {
		return Manifest{}, err
	}
	artifactHash, err := ir.Hash(&artifact)
	if err != nil {
		return Manifest{}, err
	}
	if err := ir.ValidatePackageSymbols(&artifact, artifactHash, &symbols); err != nil {
		return Manifest{}, err
	}
	if data.ModulePath != artifact.Module.Path || data.Package != artifact.Module.Package || data.ArtifactHash != artifactHash {
		return Manifest{}, errors.New("export data does not match artifact")
	}
	manifest := NewManifest(artifact, artifactHash, data.ExportHash)
	entry := transientCompileEntry{
		lookup:   Lookup{Artifact: ir.CloneArtifact(artifact), Symbols: ir.ClonePackageSymbols(symbols), ExportData: clonePackageData(data), ArtifactHash: artifactHash, ExportHash: data.ExportHash},
		manifest: manifest, size: estimateArtifactBytes(artifact) + estimatePackageSymbolsBytes(symbols) + estimatePackageDataBytes(data),
	}
	c.mu.Lock()
	if previous, ok := c.compile[key]; ok {
		c.bytes -= previous.size
	} else {
		c.order = append(c.order, transientOrderEntry{key: key})
	}
	c.compile[key] = entry
	c.bytes += entry.size
	c.stats.Stores++
	c.evictLocked()
	c.mu.Unlock()
	manifest.RuntimeDependencies = append([]string(nil), manifest.RuntimeDependencies...)
	return manifest, nil
}

func (c *TransientCache) LookupPrepare(action PrepareAction) (PrepareLookup, error) {
	key, err := action.Key()
	if err != nil {
		return PrepareLookup{}, err
	}
	c.mu.Lock()
	entry, ok := c.prepare[key]
	if ok {
		c.stats.Hits++
	} else {
		c.stats.Misses++
	}
	c.mu.Unlock()
	if !ok {
		return PrepareLookup{Reason: "transient action missing"}, nil
	}
	lookup := entry.lookup
	lookup.Image = ir.CloneExecutionImage(lookup.Image)
	lookup.TestManifest = append([]TestEntry(nil), lookup.TestManifest...)
	lookup.Hit = true
	lookup.Reason = "transient hit"
	return lookup, nil
}

func (c *TransientCache) StorePrepare(action PrepareAction, output PreparedOutput) error {
	if err := validatePreparedOutput(action, output, true); err != nil {
		return err
	}
	key, err := action.Key()
	if err != nil {
		return err
	}
	entry := transientPrepareEntry{
		lookup: PrepareLookup{Image: ir.CloneExecutionImage(output.Image), TestManifest: append([]TestEntry(nil), output.TestManifest...)},
		size:   estimateExecutionImageBytes(output.Image) + int64(len(output.TestManifest))*64,
	}
	c.mu.Lock()
	if previous, ok := c.prepare[key]; ok {
		c.bytes -= previous.size
	} else {
		c.order = append(c.order, transientOrderEntry{key: key, isPrepare: true})
	}
	c.prepare[key] = entry
	c.bytes += entry.size
	c.stats.Stores++
	c.evictLocked()
	c.mu.Unlock()
	return nil
}

func (c *TransientCache) LookupSymbols(action SymbolAction) (SymbolLookup, error) {
	key, err := action.Key()
	if err != nil {
		return SymbolLookup{}, err
	}
	key = "symbols\x00" + key
	c.mu.Lock()
	entry, ok := c.prepare[key]
	if ok {
		c.stats.Hits++
	} else {
		c.stats.Misses++
	}
	c.mu.Unlock()
	if !ok || entry.symbols == nil {
		return SymbolLookup{Reason: "transient action missing"}, nil
	}
	return SymbolLookup{Symbols: ir.CloneProgramSymbols(*entry.symbols), Hit: true, Reason: "transient hit"}, nil
}

func (c *TransientCache) StoreSymbols(action SymbolAction, symbols ir.ProgramSymbols) error {
	if err := validateSymbolAction(action, symbols); err != nil {
		return err
	}
	key, err := action.Key()
	if err != nil {
		return err
	}
	key = "symbols\x00" + key
	owned := ir.CloneProgramSymbols(symbols)
	entry := transientPrepareEntry{symbols: &owned, size: estimateProgramSymbolsBytes(symbols)}
	c.mu.Lock()
	if previous, ok := c.prepare[key]; ok {
		c.bytes -= previous.size
	} else {
		c.order = append(c.order, transientOrderEntry{key: key, isPrepare: true})
	}
	c.prepare[key] = entry
	c.bytes += entry.size
	c.stats.Stores++
	c.evictLocked()
	c.mu.Unlock()
	return nil
}

func (c *TransientCache) LockCompile(ctx context.Context, action Action) (func(), error) {
	key, err := action.Key()
	if err != nil {
		return nil, err
	}
	return c.locks.acquire(ctx, "compile\x00"+key)
}

func (c *TransientCache) LockPrepare(ctx context.Context, action PrepareAction) (func(), error) {
	key, err := action.Key()
	if err != nil {
		return nil, err
	}
	return c.locks.acquire(ctx, "prepare\x00"+key)
}

func (c *TransientCache) LockSymbols(ctx context.Context, action SymbolAction) (func(), error) {
	key, err := action.Key()
	if err != nil {
		return nil, err
	}
	return c.locks.acquire(ctx, "symbols\x00"+key)
}

func (c *TransientCache) evictLocked() {
	for len(c.compile)+len(c.prepare) > c.config.MaxEntries || c.bytes > c.config.MaxBytes {
		oldest := c.order[0]
		c.order[0] = transientOrderEntry{}
		c.order = c.order[1:]
		if oldest.isPrepare {
			if entry, ok := c.prepare[oldest.key]; ok {
				delete(c.prepare, oldest.key)
				c.bytes -= entry.size
				c.stats.Evictions++
			}
		} else if entry, ok := c.compile[oldest.key]; ok {
			delete(c.compile, oldest.key)
			c.bytes -= entry.size
			c.stats.Evictions++
		}
	}
}

func (c *TransientCache) Stats() TransientStats {
	c.mu.Lock()
	stats := c.stats
	stats.Entries = len(c.compile) + len(c.prepare)
	stats.Bytes = c.bytes
	c.mu.Unlock()
	return stats
}

func (c *TransientCache) Clear() {
	c.mu.Lock()
	c.compile = make(map[string]transientCompileEntry)
	c.prepare = make(map[string]transientPrepareEntry)
	c.order = nil
	c.bytes = 0
	c.stats.Clears++
	c.mu.Unlock()
}

func (c *TransientCache) Close() { c.Clear() }

func clonePackageData(data PackageData) PackageData {
	out := data
	out.TypeTable = types.CloneTable(data.TypeTable)
	out.Constants = make([]ir.Constant, len(data.Constants))
	for index, constant := range data.Constants {
		out.Constants[index] = constant
		out.Constants[index].Value = append([]byte(nil), constant.Value...)
	}
	out.Exports = append([]ir.Export(nil), data.Exports...)
	out.Requirements = make([]ir.Requirement, len(data.Requirements))
	for index, requirement := range data.Requirements {
		out.Requirements[index] = requirement
		out.Requirements[index].Exports = append([]string(nil), requirement.Exports...)
	}
	out.SourceFiles = append([]ir.SourceFile(nil), data.SourceFiles...)
	out.GenericTemplates = make([]GenericTemplate, len(data.GenericTemplates))
	for index, template := range data.GenericTemplates {
		out.GenericTemplates[index] = template
		out.GenericTemplates[index].Decl = ast.CloneDecl(template.Decl)
		out.GenericTemplates[index].References = append([]GenericReference(nil), template.References...)
	}
	return out
}

func estimateArtifactBytes(artifact ir.Artifact) int64 {
	size := int64(len(artifact.Module.Path) + len(artifact.Module.Package))
	for _, constant := range artifact.Constants {
		size += int64(len(constant.ID) + len(constant.Value) + 32)
	}
	for _, function := range artifact.Functions {
		size += int64(len(function.ID)) + int64(len(function.Locals)+len(function.Upvalues))*64
		for _, instruction := range function.Instructions {
			size += int64(len(instruction.Op) + len(instruction.Payload))
		}
	}
	size += int64(len(artifact.TypeTable.Nodes))*128 + int64(len(artifact.Globals)+len(artifact.Exports)+len(artifact.Requirements))*64
	return size
}

func estimatePackageDataBytes(data PackageData) int64 {
	return int64(len(data.ModulePath)+len(data.Package)+len(data.ArtifactHash)+len(data.ExportHash)) +
		int64(len(data.TypeTable.Nodes))*128 + int64(len(data.Constants)+len(data.Exports)+len(data.Requirements))*64 +
		int64(len(data.GenericTemplates))*256
}

func estimateExecutionImageBytes(image ir.ExecutionImage) int64 {
	size := int64(len(image.CompilerID) + len(image.Root) + len(image.Hash))
	for modulePath, archive := range image.Packages {
		size += int64(len(modulePath) + len(archive.Artifact) + len(archive.ArtifactHash))
	}
	return size + int64(len(image.Entries))*64
}

func estimateProgramSymbolsBytes(symbols ir.ProgramSymbols) int64 {
	size := int64(len(symbols.ProgramHash) + len(symbols.CompilerID) + len(symbols.Hash))
	for modulePath, symbols := range symbols.Packages {
		size += int64(len(modulePath)) + estimatePackageSymbolsBytes(symbols)
	}
	return size
}

func estimatePackageSymbolsBytes(symbols ir.PackageSymbols) int64 {
	size := int64(len(symbols.ModulePath) + len(symbols.SourceHash) + len(symbols.CodeHash))
	for _, file := range symbols.Files {
		size += int64(len(file.ID) + len(file.Path) + len(file.Hash))
	}
	for _, global := range symbols.Globals {
		size += int64(len(global.ID) + len(global.Name))
	}
	for _, function := range symbols.Functions {
		size += int64(len(function.ID) + len(function.Name))
		if function.Declaration != nil {
			size += int64(len(function.Declaration.File)) + 16
		}
		for _, local := range function.Locals {
			size += int64(len(local.ID)+len(local.Name)) + 24
			if local.Declaration != nil {
				size += int64(len(local.Declaration.File)) + 16
			}
		}
		for _, upvalue := range function.Upvalues {
			size += int64(len(upvalue.ID) + len(upvalue.Name))
		}
		for _, scope := range function.Scopes {
			size += 16 + int64(len(scope.Ranges))*16
		}
		for _, location := range function.Locations {
			size += 8
			for _, point := range location.Points {
				size += int64(len(point.File)) + 16
			}
		}
	}
	return size
}
