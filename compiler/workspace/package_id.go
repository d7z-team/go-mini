package workspace

import (
	"errors"
	"fmt"
	"strings"
)

// PackageID is the structured identity used by workspace graphs and caches.
// Import paths remain source syntax and are resolved to this identity once.
type PackageID struct {
	Namespace string
	Path      string
}

func (id PackageID) String() string {
	if id.Namespace == "std" {
		return "std::" + id.Path
	}
	return id.Namespace + "::" + id.Path
}

func normalizePackageID(id PackageID, importPath string) (PackageID, error) {
	id.Namespace = strings.TrimSpace(id.Namespace)
	id.Path = strings.TrimSpace(id.Path)
	if id.Namespace == "" {
		id.Namespace = "module:" + importPath
	}
	if strings.ContainsAny(id.Namespace, " \t\r\n") || strings.Contains(id.Namespace, "::") {
		return PackageID{}, fmt.Errorf("invalid package namespace %q", id.Namespace)
	}
	if id.Path != "" {
		if _, err := normalizeModulePath(id.Path); err != nil {
			return PackageID{}, fmt.Errorf("invalid package identity path %q", id.Path)
		}
	}
	if id.Namespace == "std" && id.Path == "" {
		return PackageID{}, errors.New("standard-library package identity has an empty path")
	}
	return id, nil
}
