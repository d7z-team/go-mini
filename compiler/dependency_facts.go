package compiler

import (
	"sort"

	check "github.com/d7z-team/mini-go/compiler/semantic"
)

// semanticDependencies keeps directly imported values and the type metadata
// of their dependency closure. A public signature may reference a type from a
// package the caller does not import. Source import lookup still owns visibility.
func semanticDependencies(imports []string, lookup func(string) ([]check.DependencyExport, []string, bool)) []check.DependencyPackage {
	direct := make(map[string]bool, len(imports))
	for _, path := range imports {
		direct[path] = true
	}
	seen := make(map[string]bool)
	var result []check.DependencyPackage
	pending := append([]string(nil), imports...)
	for len(pending) != 0 {
		path := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[path] {
			continue
		}
		seen[path] = true
		exports, dependencies, ok := lookup(path)
		if !ok {
			continue
		}
		pending = append(pending, dependencies...)
		pkg := check.DependencyPackage{ModulePath: path}
		for _, member := range exports {
			if direct[path] || member.Kind == check.ObjectType {
				pkg.Members = append(pkg.Members, member)
			}
		}
		if direct[path] || len(pkg.Members) != 0 {
			sort.Slice(pkg.Members, func(i, j int) bool { return pkg.Members[i].Name < pkg.Members[j].Name })
			result = append(result, pkg)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ModulePath < result[j].ModulePath })
	return result
}
