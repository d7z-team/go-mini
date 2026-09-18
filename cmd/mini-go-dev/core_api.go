package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/stdlib"
)

const (
	baselinePath = "cmd/mini-go-dev/testdata/go1.26.6.json.gz"
	schema       = "mini-go-core-api"
	version      = 1
	goVersion    = "go1.26.6"
)

type snapshot struct {
	Schema    string       `json:"schema"`
	Version   int          `json:"version"`
	GoVersion string       `json:"go_version"`
	Packages  []apiPackage `json:"packages"`
}

type apiPackage struct {
	Path         string           `json:"path"`
	Complete     bool             `json:"complete"`
	Declarations []apiDeclaration `json:"declarations"`
}

type apiDeclaration struct {
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Alias      bool       `json:"alias,omitempty"`
	Untyped    bool       `json:"untyped,omitempty"`
	Exact      string     `json:"exact,omitempty"`
	Fields     []apiField `json:"fields,omitempty"`
	Methods    []apiField `json:"methods,omitempty"`
	TypeParams []apiField `json:"type_params,omitempty"`
}

type apiField struct {
	Name     string `json:"name,omitempty"`
	Type     string `json:"type"`
	Tag      string `json:"tag,omitempty"`
	Embedded bool   `json:"embedded,omitempty"`
	Variadic bool   `json:"variadic,omitempty"`
}

type difference struct {
	Package string `json:"package"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Want    string `json:"want,omitempty"`
	Got     string `json:"got,omitempty"`
}

var completePackages = map[string]bool{
	"cmp": true, "encoding/binary": true, "errors": true, "io": true,
	"io/fs": true, "iter": true, "math/bits": true, "path": true,
	"sync": true, "testing/fstest": true, "time": true, "unicode": true,
}

var runnerDeclarations = map[string]bool{
	"func:Main":   true,
	"type:Report": true,
	"type:Result": true,
}

func trackedDeclaration(packagePath, kind, name string) bool {
	return packagePath != "testing" || !runnerDeclarations[kind+":"+name]
}

func runCoreAPI(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "snapshot" && args[0] != "verify" {
		return errors.New("usage: mini-go-dev core-api snapshot|verify [-json]")
	}
	flags := flag.NewFlagSet("mini-go-dev core-api "+args[0], flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "write machine-readable differences")
	path := flags.String("baseline", baselinePath, "API baseline path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if !strings.HasPrefix(runtime.Version(), goVersion) && args[0] == "snapshot" {
		return fmt.Errorf("API baseline requires %s, got %s", goVersion, runtime.Version())
	}
	if args[0] == "snapshot" {
		baseline, err := buildGoSnapshot()
		if err != nil {
			return err
		}
		if err := writeSnapshot(*path, baseline); err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "wrote %s\n", *path)
		return err
	}
	want, err := readSnapshot(*path)
	if err != nil {
		return err
	}
	got, err := buildMiniGoSnapshot(want)
	if err != nil {
		return err
	}
	differences := compareSnapshots(want, got)
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(differences); err != nil {
			return err
		}
	} else {
		for _, diff := range differences {
			fmt.Fprintf(stdout, "%s: %s %s: %s", diff.Package, diff.Kind, diff.Name, diff.Code)
			if diff.Want != "" || diff.Got != "" {
				fmt.Fprintf(stdout, " (want %q, got %q)", diff.Want, diff.Got)
			}
			fmt.Fprintln(stdout)
		}
	}
	if len(differences) != 0 {
		return fmt.Errorf("core API verification found %d differences", len(differences))
	}
	return nil
}

func packagePaths() ([]string, error) {
	library := stdlib.Open()
	sources, err := workspace.StandardLibrary(library)
	if err != nil {
		return nil, err
	}
	paths, err := sources.PackagePaths()
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(paths))
	for _, path := range paths {
		set[path] = true
	}
	out := make([]string, 0, len(set))
	for path := range set {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}
