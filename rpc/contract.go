package rpc

import (
	"context"
	"fmt"
	"sort"
)

type Method struct {
	ID               string
	Service          string
	Name             string
	ContractHash     string
	ResourceTypeHash string
}

type Call struct {
	Method    Method
	Receiver  *ResourceRef
	Arguments []Value
}

// UnaryInvoker performs one complete unary call and returns its provisional
// result. The caller remains responsible for Accept or Discard.
type UnaryInvoker func(context.Context, Call) (*Result, error)

// UnaryInterceptor wraps outbound calls made through an MRPC Host session.
// Interceptors must not decide the provisional Result.
type UnaryInterceptor func(context.Context, Call, UnaryInvoker) (*Result, error)

type MethodHandler func(context.Context, []Value) ([]Value, error)

type MethodBinding struct {
	Method Method
	Invoke MethodHandler
}

const ContractProtocol = "minigo.rpc.contract.v2"

// Contract is the complete immutable method set fixed for one binding.
type Contract struct {
	Protocol string
	Methods  []Method
}

// CheckMethodSupport checks exact method support without binding a provider.
// Missing methods are unimplemented; a declared ID with a different contract
// is a failed precondition. This checks declarations, not service health.
func CheckMethodSupport(available, required []Method) error {
	if len(required) == 0 {
		return nil
	}
	exact := make(map[Method]struct{}, len(available))
	ids := make(map[string]struct{}, len(available))
	for _, method := range available {
		exact[method] = struct{}{}
		ids[method.ID] = struct{}{}
	}
	var missing error
	for _, method := range required {
		if _, ok := exact[method]; ok {
			continue
		}
		if _, ok := ids[method.ID]; ok {
			return StatusError{Code: CodeFailedPrecondition, Message: "RPC method contract does not match: " + method.ID}
		}
		if missing == nil {
			missing = StatusError{Code: CodeUnimplemented, Message: "RPC method is not implemented: " + method.ID}
		}
	}
	return missing
}

// Provider exposes one or more service implementations. BindRPC fixes all
// requested methods to one implementation generation.
type Provider interface {
	RPCContract() Contract
	BindRPC(context.Context, BindRequest) (ProviderLease, error)
}

// ProviderLease owns the provider state selected by one binding transaction.
type ProviderLease interface {
	Invoke(context.Context, Method, []Value) (*ProviderResult, error)
	Close() error
}

// NormalizeContract validates, clones, and orders an exact RPC contract.
func NormalizeContract(contract Contract, limits Limits) (Contract, error) {
	return normalizeContract(contract, normalizeLimits(limits).MaxMethods)
}

func normalizeContract(contract Contract, limit int) (Contract, error) {
	if contract.Protocol != ContractProtocol {
		return Contract{}, StatusError{Code: CodeProtocol, Message: "unsupported RPC contract protocol"}
	}
	if len(contract.Methods) == 0 {
		return Contract{}, StatusError{Code: CodeInvalidArgument, Message: "RPC contract is empty"}
	}
	if len(contract.Methods) > limit {
		return Contract{}, StatusError{Code: CodeResourceExhausted, Message: "RPC contract limit exceeded"}
	}
	seen := make(map[Method]struct{}, len(contract.Methods))
	methods := make([]Method, 0, len(contract.Methods))
	for _, method := range contract.Methods {
		if err := validateMethod(method); err != nil {
			return Contract{}, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
		}
		if _, duplicate := seen[method]; duplicate {
			return Contract{}, StatusError{Code: CodeInvalidArgument, Message: "duplicate RPC method: " + method.ID}
		}
		seen[method] = struct{}{}
		methods = append(methods, method)
	}
	sort.Slice(methods, func(i, j int) bool {
		if methods[i].ID != methods[j].ID {
			return methods[i].ID < methods[j].ID
		}
		return methods[i].ContractHash < methods[j].ContractHash
	})
	return Contract{Protocol: ContractProtocol, Methods: methods}, nil
}

func cloneContract(contract Contract) Contract {
	contract.Methods = append([]Method(nil), contract.Methods...)
	return contract
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func clonePeerInfo(peer PeerInfo) PeerInfo {
	peer.Attributes = cloneLabels(peer.Attributes)
	return peer
}

// BindOptions supplies runtime routing input. It is not part of the MRPC
// contract or compiler cache identity.
type BindOptions struct {
	AffinityKey string
	Labels      map[string]string
}

// PeerInfo is authenticated connection metadata supplied by the embedding
// transport. MRPC callers cannot set remote Endpoint peer metadata.
type PeerInfo struct {
	Identity   string
	Attributes map[string]string
}

// CallInfo is immutable routing context attached to a provider invocation.
type CallInfo struct {
	Method   Method
	Peer     PeerInfo
	Provider string
}

type callInfoKey struct{}

// CallInfoFromContext returns the provider call selected for ctx.
func CallInfoFromContext(ctx context.Context) (CallInfo, bool) {
	if ctx == nil {
		return CallInfo{}, false
	}
	info, ok := ctx.Value(callInfoKey{}).(CallInfo)
	return info, ok
}

func withCallInfo(ctx context.Context, method Method, peer PeerInfo, provider string) context.Context {
	peer = clonePeerInfo(peer)
	return context.WithValue(ctx, callInfoKey{}, CallInfo{Method: method, Peer: peer, Provider: provider})
}

type BindRequest struct {
	Contract Contract
	Options  BindOptions
	Peer     PeerInfo
	Provider string
	Hops     int
}

// Binder resolves a complete set of exact method contracts atomically.
type Binder interface {
	Bind(context.Context, BindRequest) (*RouteSet, error)
}

func validateMethod(method Method) error {
	if method.ID == "" || method.Service == "" || method.Name == "" || len(method.ContractHash) != 64 || method.ID != method.Service+"."+method.Name {
		return fmt.Errorf("invalid rpc method %q", method.ID)
	}
	if method.ResourceTypeHash != "" && len(method.ResourceTypeHash) != 64 {
		return fmt.Errorf("invalid rpc resource method %q", method.ID)
	}
	return nil
}
