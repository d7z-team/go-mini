package main

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/d7z-team/mini-go/compiler/workspace"
)

type sourceOptions struct {
	module   string
	mappings stringList
}

func (options *sourceOptions) flags(flags *flag.FlagSet) {
	flags.StringVar(&options.module, "module", "", "logical import prefix for the source directory (default app)")
	flags.Var(&options.mappings, "source", "additional import/path=directory source mapping; repeatable")
}

func (options sourceOptions) load(environment commandEnvironment, explicit bool) (workspace.Directory, error) {
	mappings := make([]workspace.DirectorySource, 0, len(options.mappings))
	for _, value := range options.mappings {
		module, directory, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(module) == "" || strings.TrimSpace(directory) == "" {
			return workspace.Directory{}, fmt.Errorf("invalid source mapping %q; want import/path=directory", value)
		}
		mappings = append(mappings, workspace.DirectorySource{Module: module, Directory: directory})
	}
	if explicit {
		if options.module != "" {
			return workspace.Directory{}, errors.New("-module requires directory input")
		}
		if len(mappings) == 0 {
			return workspace.Directory{}, nil
		}
		first := mappings[0]
		for i := range mappings {
			if !filepath.IsAbs(mappings[i].Directory) {
				mappings[i].Directory = environment.path(mappings[i].Directory)
			}
		}
		return workspace.LoadSources(environment.context(), environment.path(first.Directory), first.Module, mappings[1:])
	}
	return workspace.LoadSources(environment.context(), environment.workingDir, options.module, mappings)
}
