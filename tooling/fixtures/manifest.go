// Package fixtures describes distributed conformance inputs and their provenance.
package fixtures

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"
)

// Entry identifies one distributed fixture and the source of its expected behavior.
type Entry struct {
	SHA256     string `json:"sha256"`
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	Oracle     string `json:"oracle"`
	CompilerID string `json:"compiler_id,omitempty"`
}

// Manifest binds distributed artifacts to their contents and repository inputs.
type Manifest struct {
	Version   int               `json:"version"`
	Protocols map[string]string `json:"protocols,omitempty"`
	Files     map[string]Entry  `json:"files"`
	Inputs    map[string]string `json:"inputs,omitempty"`
}

// AddInputs records repository-owned source inputs outside the fixture directory.
func (manifest *Manifest) AddInputs(prefix string, root fs.FS) error {
	files, err := ReadTree(root)
	if err != nil {
		return err
	}
	if manifest.Inputs == nil {
		manifest.Inputs = make(map[string]string)
	}
	for name, data := range files {
		hash := sha256.Sum256(data)
		manifest.Inputs[prefix+name] = hex.EncodeToString(hash[:])
	}
	return nil
}

// ValidateInputs checks the external source hashes against the repository snapshot.
func (manifest Manifest) ValidateInputs(repository fs.FS) error {
	for name, expected := range manifest.Inputs {
		if !fs.ValidPath(name) {
			return fmt.Errorf("invalid fixture input path %q", name)
		}
		data, err := fs.ReadFile(repository, name)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != expected {
			return fmt.Errorf("fixture source changed: %s", name)
		}
	}
	return nil
}

// ReadTree excludes documentation and the manifest itself; all other inputs
// must be represented, including unreferenced files that would otherwise go stale.
func ReadTree(root fs.FS) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := walkInputs(root, func(name string) error {
		data, err := fs.ReadFile(root, name)
		if err == nil {
			files[name] = data
		}
		return err
	})
	return files, err
}

func walkInputs(root fs.FS, visit func(string) error) error {
	return fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || name == "manifest.json" || strings.HasSuffix(name, ".md") {
			return nil
		}
		return visit(name)
	})
}

// Describe computes hashes while the caller assigns each artifact its provenance.
func Describe(files map[string][]byte, classify func(string) Entry) Manifest {
	manifest := Manifest{Version: 2, Files: make(map[string]Entry, len(files))}
	for name, data := range files {
		entry := classify(name)
		hash := sha256.Sum256(data)
		entry.SHA256 = hex.EncodeToString(hash[:])
		manifest.Files[name] = entry
	}
	return manifest
}

// Validate rejects stale, missing and unlisted distributed files.
func (manifest Manifest) Validate(root fs.FS) error {
	if manifest.Version != 2 {
		return fmt.Errorf("unsupported fixture manifest version %d", manifest.Version)
	}
	for _, name := range slices.Sorted(maps.Keys(manifest.Files)) {
		entry := manifest.Files[name]
		if !fs.ValidPath(name) || entry.Kind == "" || entry.Source == "" || entry.Oracle == "" {
			return fmt.Errorf("invalid fixture entry %q", name)
		}
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	err := walkInputs(root, func(name string) error {
		entry, ok := manifest.Files[name]
		if !ok {
			return fmt.Errorf("fixture missing from manifest: %s", name)
		}
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
			return fmt.Errorf("fixture hash mismatch: %s", name)
		}
		seen[name] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(manifest.Files)) {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("fixture missing: %s", name)
		}
	}
	return nil
}
