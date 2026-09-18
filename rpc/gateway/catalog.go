// Package gateway provides the optional WebSocket transport and control plane
// for RPC routers.
package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/d7z-team/mini-go/rpc"
	rpcrouter "github.com/d7z-team/mini-go/rpc/router"
)

const (
	SnapshotProtocol     = "minigo.rpc.gateway.snapshot.v2"
	maxCatalogReferences = 4096
)

// CatalogSource is the immutable contract repository exposed by a Gateway.
type CatalogSource interface {
	rpcrouter.ContractResolver
	ContractReferences() []rpcrouter.ContractReference
}

// Snapshot fixes the exact remote contract manifest used by one compilation.
type Snapshot struct {
	Protocol   string                        `json:"protocol"`
	ID         string                        `json:"id"`
	GatewayID  string                        `json:"gatewayID,omitempty"`
	RouteEpoch uint64                        `json:"routeEpoch,omitempty"`
	References []rpcrouter.ContractReference `json:"references,omitempty"`
}

func newSnapshot(references []rpcrouter.ContractReference) (Snapshot, error) {
	if len(references) > maxCatalogReferences {
		return Snapshot{}, errors.New("Gateway catalog contains too many contract references")
	}
	references = append([]rpcrouter.ContractReference(nil), references...)
	sort.Slice(references, func(i, j int) bool {
		if references[i].ImportPath != references[j].ImportPath {
			return references[i].ImportPath < references[j].ImportPath
		}
		return references[i].Hash < references[j].Hash
	})
	seen := make(map[string]rpcrouter.ContractReference, len(references))
	unique := references[:0]
	for _, reference := range references {
		if previous, ok := seen[reference.ImportPath]; ok {
			if previous != reference {
				return Snapshot{}, fmt.Errorf("MRPC import path %q has multiple active hashes", reference.ImportPath)
			}
			continue
		}
		if strings.TrimSpace(reference.ImportPath) == "" || len(reference.Hash) != sha256.Size*2 || strings.ToLower(reference.Hash) != reference.Hash {
			return Snapshot{}, errors.New("Gateway returned an invalid MRPC contract reference")
		}
		if _, err := hex.DecodeString(reference.Hash); err != nil {
			return Snapshot{}, errors.New("Gateway returned an invalid MRPC contract hash")
		}
		seen[reference.ImportPath] = reference
		unique = append(unique, reference)
	}
	var material strings.Builder
	for _, reference := range unique {
		material.WriteString(reference.ImportPath)
		material.WriteByte(0)
		material.WriteString(reference.Hash)
		material.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(SnapshotProtocol + "\x00" + material.String()))
	return Snapshot{Protocol: SnapshotProtocol, ID: hex.EncodeToString(sum[:]), References: unique}, nil
}

func snapshotFromRevision(revision rpcrouter.Revision) (Snapshot, error) {
	snapshot, err := newSnapshot(revision.References)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.GatewayID = revision.RouterID
	snapshot.RouteEpoch = revision.RouteEpoch
	return snapshot, nil
}

// NewCatalogProvider exposes source discovery through ordinary MRPC calls.
func NewCatalogProvider(source CatalogSource) (rpc.Provider, error) {
	if source == nil {
		return nil, errors.New("Gateway catalog source is required")
	}
	return NewMiniGoGatewayControlCatalogProvider(catalogHandler{source: source})
}

type catalogHandler struct{ source CatalogSource }

func (handler catalogHandler) Snapshot(context.Context) (MiniGoGatewayControlCatalogSnapshot, error) {
	if revisionSource, ok := handler.source.(interface{ Revision() rpcrouter.Revision }); ok {
		snapshot, err := snapshotFromRevision(revisionSource.Revision())
		return catalogSnapshotToWire(snapshot), err
	}
	snapshot, err := newSnapshot(handler.source.ContractReferences())
	return catalogSnapshotToWire(snapshot), err
}

func (handler catalogHandler) Watch(ctx context.Context, gatewayID string, routeEpoch uint64) (MiniGoGatewayControlCatalogSnapshot, error) {
	revisionSource, ok := handler.source.(interface {
		WatchRevision(context.Context, string, uint64) (rpcrouter.Revision, error)
	})
	if !ok {
		return MiniGoGatewayControlCatalogSnapshot{}, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "Gateway catalog source does not support watching"}
	}
	revision, err := revisionSource.WatchRevision(ctx, gatewayID, routeEpoch)
	if err != nil {
		return MiniGoGatewayControlCatalogSnapshot{}, err
	}
	snapshot, err := snapshotFromRevision(revision)
	return catalogSnapshotToWire(snapshot), err
}

func (handler catalogHandler) Resolve(ctx context.Context, importPath, hash string) (MiniGoGatewayControlContractBundle, error) {
	bundle, err := handler.source.ResolveContract(ctx, rpcrouter.ContractReference{ImportPath: importPath, Hash: hash})
	return contractBundleToWire(bundle), err
}

// Client reads immutable source snapshots over any RPC Binder.
type Client struct {
	mu      sync.RWMutex
	watchMu sync.Mutex
	client  *MiniGoGatewayControlCatalogClient
}

// NewClient binds the built-in Gateway catalog contract.
func NewClient(ctx context.Context, binder rpc.Binder) (*Client, error) {
	if binder == nil {
		return nil, errors.New("Gateway RPC binder is required")
	}
	client, err := BindMiniGoGatewayControlCatalogClient(ctx, binder, rpc.BindOptions{})
	if err != nil {
		return nil, err
	}
	return &Client{client: client}, nil
}

// Snapshot returns one canonical catalog view.
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	client := c.currentClient()
	if client == nil {
		return Snapshot{}, errors.New("Gateway catalog client is closed")
	}
	wired, err := client.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := catalogSnapshotFromWire(wired)
	expected, err := newSnapshot(snapshot.References)
	if err != nil || snapshot.Protocol != expected.Protocol || snapshot.ID != expected.ID {
		return Snapshot{}, errors.New("Gateway catalog snapshot identity is invalid")
	}
	expected.GatewayID = snapshot.GatewayID
	expected.RouteEpoch = snapshot.RouteEpoch
	return expected, nil
}

// Watch waits for a Gateway revision newer than after.
func (c *Client) Watch(ctx context.Context, after Snapshot) (Snapshot, error) {
	if c == nil {
		return Snapshot{}, errors.New("Gateway catalog client is closed")
	}
	c.watchMu.Lock()
	defer c.watchMu.Unlock()
	client := c.currentClient()
	if client == nil {
		return Snapshot{}, errors.New("Gateway catalog client is closed")
	}
	wired, err := client.Watch(ctx, after.GatewayID, after.RouteEpoch)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := catalogSnapshotFromWire(wired)
	expected, err := newSnapshot(snapshot.References)
	if err != nil || snapshot.Protocol != expected.Protocol || snapshot.ID != expected.ID || snapshot.GatewayID == "" {
		return Snapshot{}, errors.New("watched Gateway catalog snapshot identity is invalid")
	}
	if snapshot.GatewayID == after.GatewayID && snapshot.RouteEpoch == after.RouteEpoch {
		return Snapshot{}, errors.New("Gateway catalog Watch returned an unchanged revision")
	}
	expected.GatewayID = snapshot.GatewayID
	expected.RouteEpoch = snapshot.RouteEpoch
	return expected, nil
}

// ResolveContract implements rpcrouter.ContractResolver using an exact remote query.
func (c *Client) ResolveContract(ctx context.Context, reference rpcrouter.ContractReference) (rpcrouter.MRPCBundle, error) {
	client := c.currentClient()
	if client == nil {
		return rpcrouter.MRPCBundle{}, errors.New("Gateway catalog client is closed")
	}
	wired, err := client.Resolve(ctx, reference.ImportPath, reference.Hash)
	if err != nil {
		return rpcrouter.MRPCBundle{}, err
	}
	bundle := contractBundleFromWire(wired)
	normalized, err := rpcrouter.NewMRPCBundle(bundle.ImportPath, bundle.Files)
	if err != nil || normalized.ImportPath != reference.ImportPath || normalized.Hash != reference.Hash {
		return rpcrouter.MRPCBundle{}, errors.New("Gateway returned a different MRPC bundle identity")
	}
	return normalized, nil
}

// Close releases the catalog route lease without closing its Binder.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	client := c.client
	c.client = nil
	c.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Close()
}

func (c *Client) currentClient() *MiniGoGatewayControlCatalogClient {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()
	return client
}
