package surface

import (
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type BindFunc func(runtime.FFIBindContext) (*runtime.BoundFFISurface, error)

type Bundle struct {
	Schema    *runtime.FFISurfaceSchema
	Bind      BindFunc
	Templates []calltemplate.FunctionTemplate
	Err       error
}

func New(schema *runtime.FFISurfaceSchema, bind BindFunc, templates ...calltemplate.FunctionTemplate) *Bundle {
	return &Bundle{
		Schema:    runtime.CloneFFISurfaceSchema(schema),
		Bind:      bind,
		Templates: append([]calltemplate.FunctionTemplate(nil), templates...),
	}
}

func Templates(items ...calltemplate.FunctionTemplate) *Bundle {
	return &Bundle{Templates: append([]calltemplate.FunctionTemplate(nil), items...)}
}

func Merge(items ...*Bundle) *Bundle {
	merged := &Bundle{Schema: runtime.NewFFISurfaceSchema()}
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
	}
	if len(merged.Schema.Packages) == 0 && len(merged.Schema.Types) == 0 {
		merged.Schema = nil
	}
	return merged
}
