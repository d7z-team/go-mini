package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	envCacheRoot = "MINIGO_CACHE"
	envDebug     = "MINIGO_DEBUG"
)

type compilerCacheSettings struct {
	root   string
	trace  bool
	hash   bool
	verify bool
}

func (environment commandEnvironment) compilerCacheSettings() (compilerCacheSettings, error) {
	root, err := environment.cacheRoot(envCacheRoot, "cache")
	if err != nil {
		return compilerCacheSettings{}, err
	}
	settings := compilerCacheSettings{root: root}
	value := strings.TrimSpace(environment.variable(envDebug))
	if value == "" {
		return settings, nil
	}
	seen := make(map[string]struct{})
	for _, field := range strings.Split(value, ",") {
		name, raw, found := strings.Cut(strings.TrimSpace(field), "=")
		if !found || name == "" || (raw != "0" && raw != "1") {
			return compilerCacheSettings{}, fmt.Errorf("invalid %s entry %q; want name=0 or name=1", envDebug, field)
		}
		if _, exists := seen[name]; exists {
			return compilerCacheSettings{}, fmt.Errorf("duplicate %s key %q", envDebug, name)
		}
		seen[name] = struct{}{}
		enabled := raw == "1"
		switch name {
		case "cachetrace":
			settings.trace = enabled
		case "cachehash":
			settings.hash = enabled
		case "cacheverify":
			settings.verify = enabled
		default:
			return compilerCacheSettings{}, fmt.Errorf("unknown %s key %q", envDebug, name)
		}
	}
	return settings, nil
}

func (environment commandEnvironment) cacheRoot(variable, leaf string) (string, error) {
	root := strings.TrimSpace(environment.variable(variable))
	if root == "" {
		return filepath.Join(environment.temporaryDirectory(), "mini-go", leaf), nil
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("%s must be an absolute path", variable)
	}
	return filepath.Clean(root), nil
}
