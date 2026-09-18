package workspace

import (
	"errors"
	"sort"
	"sync"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

type cachedPackageHeader struct {
	source      SourcePackage
	header      PackageHeader
	diagnostics []source.Diagnostic
}

type packageHeaderCache struct {
	mu     sync.RWMutex
	values map[string]cachedPackageHeader
}

func loadCachedPackageHeader(cache *packageHeaderCache, pkg SourcePackage, buildTarget target.Target, limits Limits) (SourcePackage, PackageHeader, []source.Diagnostic, bool, error) {
	key := pkg.ModulePath
	if cache != nil {
		cache.mu.RLock()
		cached, found := cache.values[key]
		cache.mu.RUnlock()
		if found {
			return clonePackage(cached.source), clonePackageHeader(cached.header), cloneDiagnostics(cached.diagnostics), true, nil
		}
	}
	raw := clonePackage(pkg)
	selected, diagnostics, err := SelectPackage(raw, buildTarget)
	if err != nil || source.HasErrors(diagnostics) {
		return raw, PackageHeader{}, diagnostics, true, err
	}
	header, scannedDiagnostics, err := ScanPackageHeaderWithLimits(selected, limits)
	diagnostics = append(diagnostics, scannedDiagnostics...)
	if err != nil {
		return raw, PackageHeader{}, diagnostics, true, err
	}
	if cache != nil {
		value := cachedPackageHeader{source: raw, header: header, diagnostics: cloneDiagnostics(diagnostics)}
		cache.mu.Lock()
		cache.values[key] = value
		cache.mu.Unlock()
	}
	return clonePackage(raw), clonePackageHeader(header), cloneDiagnostics(diagnostics), true, nil
}

func (l *Loader) loadPackageHeader(modulePath string) (SourcePackage, PackageHeader, []source.Diagnostic, bool, error) {
	pkg, found, err := l.sources.Package(modulePath)
	if err != nil || !found {
		return SourcePackage{}, PackageHeader{}, nil, found, err
	}
	return loadCachedPackageHeader(l.headers, pkg, l.target, l.limits)
}

func clonePackageHeader(header PackageHeader) PackageHeader {
	header.Source = clonePackage(header.Source)
	header.Candidates = append([]SourceCandidate(nil), header.Candidates...)
	header.Imports = append([]string(nil), header.Imports...)
	header.importSpans = cloneImportSpans(header.importSpans)
	return header
}

func cloneImportSpans(spans map[string]source.Span) map[string]source.Span {
	if len(spans) == 0 {
		return nil
	}
	cloned := make(map[string]source.Span, len(spans))
	for modulePath, span := range spans {
		cloned[modulePath] = span
	}
	return cloned
}

func cloneDiagnostics(diagnostics []source.Diagnostic) []source.Diagnostic {
	out := append([]source.Diagnostic(nil), diagnostics...)
	for index := range out {
		out[index].Related = append([]source.RelatedDiagnostic(nil), out[index].Related...)
	}
	return out
}

// Loader reuses immutable package headers and dependency graphs for one source snapshot.
type Loader struct {
	sources SourceSet
	target  target.Target
	limits  Limits
	headers *packageHeaderCache
}

func NewLoader(sources SourceSet, buildTarget target.Target, limits Limits) (*Loader, error) {
	normalized, err := target.Normalize(buildTarget)
	if err != nil {
		return nil, err
	}
	return &Loader{
		sources: sources, target: normalized, limits: normalizeLimits(limits),
		headers: &packageHeaderCache{values: make(map[string]cachedPackageHeader)},
	}, nil
}

func (l *Loader) LoadHeadersForRoots(roots []string) (HeaderGraph, error) {
	if l == nil || l.sources == nil {
		return HeaderGraph{}, errors.New("nil workspace loader")
	}
	normalizedRoots := append([]string(nil), roots...)
	var err error
	for index, root := range normalizedRoots {
		normalizedRoots[index], err = normalizeModulePath(root)
		if err != nil {
			return HeaderGraph{}, err
		}
	}
	sort.Strings(normalizedRoots)
	return l.loadHeadersForRoots(normalizedRoots)
}
