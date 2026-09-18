package rpc

import (
	"context"
	"errors"
)

type ResourceRef struct {
	Epoch    uint64 `json:"epoch"`
	ObjectID uint64 `json:"object_id"`
	TypeHash string `json:"type_hash"`
}

func validateResourceRef(ref ResourceRef) error {
	if ref.Epoch == 0 || ref.ObjectID == 0 || len(ref.TypeHash) != 64 {
		return errors.New("invalid rpc resource reference")
	}
	return nil
}

type Resource interface {
	Invoke(context.Context, string, []Value) ([]Value, error)
	Close(context.Context) error
}

type Exporter interface {
	Export(Resource, string) (Value, error)
}

type Resolver interface {
	Resolve(ResourceRef) (Resource, error)
}

type callExporterKey struct{}

func Export(ctx context.Context, resource Resource, typeHash string) (Value, error) {
	exporter, _ := ctx.Value(callExporterKey{}).(Exporter)
	if exporter == nil {
		return Value{}, errors.New("rpc call cannot export resources")
	}
	return exporter.Export(resource, typeHash)
}

// Resolve returns the provider resource represented by a same-route
// reference passed as an RPC argument.
func Resolve(ctx context.Context, ref ResourceRef) (Resource, error) {
	resolver, _ := ctx.Value(callResolverKey{}).(Resolver)
	if resolver == nil {
		return nil, StatusError{Code: CodeInvalidArgument, Message: "rpc call cannot resolve resources"}
	}
	return resolver.Resolve(ref)
}

type callResolverKey struct{}
