package workspace

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type mergedSourceSet struct {
	sets  []SourceSet
	paths []string
	owner map[string]int
}

func MergeSourceSets(sets ...SourceSet) (SourceSet, error) {
	out := &mergedSourceSet{sets: append([]SourceSet(nil), sets...), owner: map[string]int{}}
	for i, set := range out.sets {
		if set == nil {
			return nil, errors.New("nil source set")
		}
		paths, err := set.PackagePaths()
		if err != nil {
			return nil, err
		}
		for _, modulePath := range paths {
			modulePath, err = normalizeModulePath(modulePath)
			if err != nil {
				return nil, err
			}
			if _, exists := out.owner[modulePath]; exists {
				return nil, fmt.Errorf("source package %q is provided by multiple source sets", modulePath)
			}
			out.owner[modulePath] = i
			out.paths = append(out.paths, modulePath)
		}
	}
	sort.Strings(out.paths)
	return out, nil
}

func (s *mergedSourceSet) PackagePaths() ([]string, error) {
	return append([]string(nil), s.paths...), nil
}

func (s *mergedSourceSet) Package(modulePath string) (SourcePackage, bool, error) {
	owner, ok := s.owner[strings.TrimSpace(modulePath)]
	if !ok {
		return SourcePackage{}, false, nil
	}
	return s.sets[owner].Package(modulePath)
}
