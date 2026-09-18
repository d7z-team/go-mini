package semantic

import "sort"

func testDependencies(members []DependencyExport) []DependencyPackage {
	packages := make(map[string][]DependencyExport)
	for _, member := range members {
		packages[member.ModulePath] = append(packages[member.ModulePath], member)
	}
	paths := make([]string, 0, len(packages))
	for path := range packages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var result []DependencyPackage
	for _, path := range paths {
		result = append(result, DependencyPackage{ModulePath: path, Members: packages[path]})
	}
	return result
}
