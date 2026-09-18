package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/analysis"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/stdlib"
	docgen "github.com/d7z-team/mini-go/tooling/doc"
)

func runDoc(environment commandEnvironment, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("mini-go doc", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputDirectory := flags.String("out", "docs/reference", "generated Markdown directory")
	check := flags.Bool("check", false, "verify generated Markdown without writing")
	var tags stringList
	var sourceConfig sourceOptions
	sourceConfig.flags(flags)
	flags.Var(&tags, "tag", "enable a Mini-Go build tag; repeatable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	selectors := flags.Args()
	if len(selectors) == 0 {
		selectors = []string{"./..."}
	}

	stdlibSources := stdlib.Open()
	standardSourceSet, err := workspace.StandardLibrary(stdlibSources)
	if err != nil {
		return err
	}
	stdlibPaths, err := standardSourceSet.PackagePaths()
	if err != nil {
		return err
	}
	stdlibSet := make(map[string]bool, len(stdlibPaths))
	for _, modulePath := range stdlibPaths {
		stdlibSet[modulePath] = true
	}
	needsModule := false
	for _, selector := range selectors {
		if selector == "./..." || selector != "std" && !stdlibSet[strings.TrimSpace(selector)] {
			needsModule = true
		}
	}
	var modulePaths []string
	var sources workspace.SourceSet
	sources = standardSourceSet
	if needsModule {
		module, err := sourceConfig.load(environment, false)
		if err != nil {
			return err
		}
		moduleSelectors := make([]string, 0, len(selectors))
		for _, selector := range selectors {
			if selector != "std" && !stdlibSet[strings.TrimSpace(selector)] {
				moduleSelectors = append(moduleSelectors, selector)
			}
		}
		modulePaths, err = workspace.SelectPackages(module.Root, module.Root, module.ModulePath, module.Documents, moduleSelectors)
		if err != nil {
			return err
		}
		sources, err = workspace.MergeSourceSets(module.Sources, standardSourceSet)
		if err != nil {
			return err
		}
	}
	selected := map[string]bool{}
	for _, modulePath := range modulePaths {
		selected[modulePath] = true
	}
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		switch {
		case selector == "std":
			for _, modulePath := range stdlibPaths {
				selected[modulePath] = true
			}
		case stdlibSet[selector]:
			selected[selector] = true
		}
	}
	roots := make([]string, 0, len(selected))
	for modulePath := range selected {
		roots = append(roots, modulePath)
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		return errors.New("documentation selection contains no packages")
	}
	buildTarget, err := target.Normalize(target.Target{Tags: append([]string(nil), tags...)})
	if err != nil {
		return err
	}
	checked, err := analysis.CheckWorkspace(analysis.WorkspaceRequest{Roots: roots, Target: buildTarget, Sources: sources})
	if err != nil {
		return err
	}
	if source.HasErrors(checked.Diagnostics) {
		writeDiagnostics(stderr, checked.Diagnostics)
		return errors.New("documentation source check failed")
	}
	catalog, err := docgen.Build(docgen.BuildRequest{Workspace: checked, Packages: roots})
	if err != nil {
		return err
	}
	generated, err := docgen.Render(catalog)
	if err != nil {
		return err
	}
	output := strings.TrimSpace(*outputDirectory)
	if output == "" {
		return errors.New("documentation output directory is empty")
	}
	if !filepath.IsAbs(output) {
		output = environment.path(output)
	}
	if *check {
		if err := compareMarkdownTree(output, generated.Files); err != nil {
			return fmt.Errorf("generated documentation is stale: %w", err)
		}
		_, err = fmt.Fprintln(stdout, "documentation is current")
		return err
	}
	if err := replaceMarkdownTree(output, generated.Files); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, output)
	return err
}

func compareMarkdownTree(root string, generated []docgen.File) error {
	want := make(map[string][]byte, len(generated))
	for _, file := range generated {
		want[filepath.FromSlash(file.Path)] = file.Text
	}
	actual := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		actual[relative] = path
		return nil
	})
	if err != nil {
		return err
	}
	for name, expected := range want {
		path, ok := actual[name]
		if !ok {
			return fmt.Errorf("missing %s", filepath.ToSlash(name))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, expected) {
			return fmt.Errorf("content differs for %s", filepath.ToSlash(name))
		}
		delete(actual, name)
	}
	if len(actual) != 0 {
		names := make([]string, 0, len(actual))
		for name := range actual {
			names = append(names, filepath.ToSlash(name))
		}
		sort.Strings(names)
		return fmt.Errorf("unexpected %s", names[0])
	}
	return nil
}

func replaceMarkdownTree(root string, generated []docgen.File) error {
	if err := compareMarkdownTree(root, generated); err == nil {
		return nil
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".mini-go-doc-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for _, file := range generated {
		name := filepath.Join(staging, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(name, file.Text, 0o644); err != nil {
			return err
		}
	}
	backup, err := os.MkdirTemp(parent, ".mini-go-doc-backup-*")
	if err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		return err
	}
	defer os.RemoveAll(backup)
	hadOutput := false
	if _, err := os.Stat(root); err == nil {
		if err := os.Rename(root, backup); err != nil {
			return err
		}
		hadOutput = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, root); err != nil {
		if hadOutput {
			_ = os.Rename(backup, root)
		}
		return err
	}
	if hadOutput {
		return os.RemoveAll(backup)
	}
	return nil
}
