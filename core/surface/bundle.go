package surface

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type BindFunc func(runtime.FFIBindContext) (*runtime.BoundFFISurface, error)

type LibraryFile struct {
	Filename string
	Language string
	Code     string
}

type LibraryModule struct {
	Path  string
	Files []LibraryFile
}

type Bundle struct {
	Schema    *runtime.FFISurfaceSchema
	Bind      BindFunc
	Templates []calltemplate.FunctionTemplate
	Libraries []LibraryModule
	Err       error
}

func New(schema *runtime.FFISurfaceSchema, bind BindFunc, templates ...calltemplate.FunctionTemplate) *Bundle {
	return &Bundle{
		Schema:    runtime.CloneFFISurfaceSchema(schema),
		Bind:      bind,
		Templates: append([]calltemplate.FunctionTemplate(nil), templates...),
	}
}

func Router(schema *runtime.FFISurfaceSchema, bridge ffigo.FFIBridge) *Bundle {
	return New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		bound := runtime.NewBoundFFISurfaceFromSchema(schema)
		if err := bound.BindSchemaRoutes(schema, bridge); err != nil {
			return nil, err
		}
		return bound, nil
	})
}

func Routes(bridge ffigo.FFIBridge, routes ...runtime.FFIRouteDecl) *Bundle {
	schema := runtime.NewFFISurfaceSchema()
	err := schema.AddRouteDecls(routes)
	bundle := Router(schema, bridge)
	if err != nil {
		bundle.Err = err
	}
	return bundle
}

func GoFile(filename, code string) LibraryFile {
	return LibraryFile{Filename: filename, Language: "go", Code: code}
}

func Library(path string, files ...LibraryFile) *Bundle {
	return Libraries(LibraryModule{Path: path, Files: files})
}

func Libraries(modules ...LibraryModule) *Bundle {
	return &Bundle{Libraries: cloneLibraryModules(modules)}
}

func Templates(items ...calltemplate.FunctionTemplate) *Bundle {
	return &Bundle{Templates: append([]calltemplate.FunctionTemplate(nil), items...)}
}

func (m LibraryModule) Hash() string {
	path := strings.TrimSpace(m.Path)
	parts := []string{"vm-library", path}
	files := cloneLibraryFiles(m.Files)
	sort.Slice(files, func(i, j int) bool {
		if files[i].Filename != files[j].Filename {
			return files[i].Filename < files[j].Filename
		}
		if files[i].Language != files[j].Language {
			return files[i].Language < files[j].Language
		}
		return files[i].Code < files[j].Code
	})
	for _, file := range files {
		language := strings.TrimSpace(file.Language)
		if language == "" || language == "go" {
			language = "go-mini/go"
		}
		parts = append(parts, strings.TrimSpace(file.Filename), language, file.Code)
	}
	return runtime.VersionedModuleRequirementHash(parts...)
}

func Merge(items ...*Bundle) *Bundle {
	merged := &Bundle{Schema: runtime.NewFFISurfaceSchema()}
	libraries := make(map[string]LibraryModule)
	libraryHashes := make(map[string]string)
	for _, item := range items {
		if item == nil {
			continue
		}
		if item.Err != nil {
			return &Bundle{Err: item.Err}
		}
		if item.Schema != nil {
			if err := merged.Schema.Merge(item.Schema); err != nil {
				return &Bundle{Err: err}
			}
		}
		if item.Bind != nil {
			prev := merged.Bind
			next := item.Bind
			merged.Bind = func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
				out := runtime.NewBoundFFISurface(merged.Schema)
				if prev != nil {
					prevSurface, err := prev(ctx)
					if err != nil {
						return nil, err
					}
					if err := out.Merge(prevSurface); err != nil {
						return nil, err
					}
				}
				nextSurface, err := next(ctx)
				if err != nil {
					return nil, err
				}
				if err := out.Merge(nextSurface); err != nil {
					return nil, err
				}
				return out, nil
			}
		}
		merged.Templates = append(merged.Templates, item.Templates...)
		for _, module := range item.Libraries {
			path := strings.TrimSpace(module.Path)
			if path == "" {
				return &Bundle{Err: errors.New("surface library missing module path")}
			}
			hash := module.Hash()
			if existing := libraryHashes[path]; existing != "" {
				if existing != hash {
					return &Bundle{Err: fmt.Errorf("surface library %s conflicts with existing source", path)}
				}
				continue
			}
			libraryHashes[path] = hash
			module.Path = path
			module.Files = cloneLibraryFiles(module.Files)
			libraries[path] = module
		}
	}
	if len(merged.Schema.Packages) == 0 && len(merged.Schema.Types) == 0 {
		merged.Schema = nil
	}
	if len(libraries) > 0 {
		paths := make([]string, 0, len(libraries))
		for path := range libraries {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		merged.Libraries = make([]LibraryModule, 0, len(paths))
		for _, path := range paths {
			merged.Libraries = append(merged.Libraries, libraries[path])
		}
	}
	return merged
}

func cloneLibraryModules(in []LibraryModule) []LibraryModule {
	if len(in) == 0 {
		return nil
	}
	out := make([]LibraryModule, len(in))
	for i, module := range in {
		out[i] = LibraryModule{
			Path:  strings.TrimSpace(module.Path),
			Files: cloneLibraryFiles(module.Files),
		}
	}
	return out
}

func cloneLibraryFiles(in []LibraryFile) []LibraryFile {
	if len(in) == 0 {
		return nil
	}
	out := make([]LibraryFile, len(in))
	copy(out, in)
	return out
}
