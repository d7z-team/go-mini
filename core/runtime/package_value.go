package runtime

import (
	"errors"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type ValueSpec struct {
	Type     RuntimeType
	Doc      string
	ReadOnly bool
}

type PackageValueProvider interface {
	Bind(FFIBindContext) (*Var, error)
}

type FFIBindContext struct {
	Registry *ffigo.HandleRegistry
}

type StaticHostRefProvider struct {
	ElementType TypeSpec
	Value       interface{}
	Bridge      ffigo.FFIBridge
}

func (p StaticHostRefProvider) Bind(ctx FFIBindContext) (*Var, error) {
	if ctx.Registry == nil {
		return nil, errors.New("package value requires a handle registry")
	}
	if p.ElementType.IsEmpty() {
		return nil, errors.New("package value missing host reference element type")
	}
	typ, err := ParseRuntimeType(HostRefType(p.ElementType))
	if err != nil {
		return nil, err
	}
	handle := ctx.Registry.RegisterPinnedTyped(p.Value, p.ElementType.String())
	return NewPinnedHostRefVar(handle, p.Bridge, typ), nil
}

type FFIPackageValue struct {
	Name     string
	Spec     *ValueSpec
	Provider PackageValueProvider
}

type BoundPackageValue struct {
	Name  string
	Spec  *ValueSpec
	Value *Var
}
