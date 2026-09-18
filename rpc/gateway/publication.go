package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/d7z-team/mini-go/rpc"
	rpcrouter "github.com/d7z-team/mini-go/rpc/router"
)

const (
	PublicationProtocol     = "minigo.rpc.gateway.publication.v3"
	maxPublishedProviders   = 1024
	maxProviderReferences   = 4096
	maxProviderLabels       = 256
	maxPublishedBundleBytes = 4 << 20
)

// PublishedProvider describes one remotely mounted provider and the exact
// source contracts needed to compile clients for it.
type PublishedProvider struct {
	ID         string                        `json:"id"`
	Contract   rpc.Contract                  `json:"contract"`
	Options    rpcrouter.RegistrationOptions `json:"options"`
	References []rpcrouter.ContractReference `json:"references,omitempty"`
}

// PublicationSnapshot is the immutable provider group offered by one run
// process connection.
type PublicationSnapshot struct {
	Protocol   string              `json:"protocol"`
	ProcessID  string              `json:"processID"`
	Generation uint64              `json:"generation"`
	ID         string              `json:"id"`
	Providers  []PublishedProvider `json:"providers"`
}

// Publisher exposes one immutable publication snapshot to a Gateway.
type Publisher struct {
	provider  rpc.Provider
	published chan struct{}
	mu        sync.Mutex
	state     publicationState
}

// PublicationProvider explicitly pairs one runtime provider with the
// language-neutral schemas made available through the Gateway catalog.
type PublicationProvider struct {
	Provider rpc.Provider
	Bundles  []rpcrouter.MRPCBundle
}

type publicationState uint8

const (
	publicationCandidate publicationState = iota
	publicationReady
	publicationPublished
)

// NewPublisher constructs the control provider used by a run publication
// session. Each supplied provider keeps its own service identity.
func NewPublisher(processID string, generation uint64, providers []PublicationProvider) (*Publisher, error) {
	processID = strings.TrimSpace(processID)
	if processID == "" || generation == 0 || len(providers) == 0 {
		return nil, errors.New("Gateway publication requires process ID, generation, and providers")
	}
	if len(providers) > maxPublishedProviders {
		return nil, errors.New("Gateway publication contains too many providers")
	}
	repository := rpcrouter.NewContractRepository()
	snapshot := PublicationSnapshot{Protocol: PublicationProtocol, ProcessID: processID, Generation: generation, Providers: make([]PublishedProvider, len(providers))}
	for index, entry := range providers {
		provider := entry.Provider
		if provider == nil {
			return nil, errors.New("Gateway publication contains a nil provider")
		}
		contract, err := rpc.NormalizeContract(provider.RPCContract(), rpc.NormalizeLimits(rpc.Limits{}))
		if err != nil {
			return nil, err
		}
		id := fmt.Sprintf("provider-%d", index+1)
		if identified, ok := provider.(rpc.IdentifiedProvider); ok && identified.RPCProviderID() != "" {
			id = identified.RPCProviderID()
		}
		var references []rpcrouter.ContractReference
		for _, bundle := range entry.Bundles {
			size := len(bundle.ImportPath) + len(bundle.Hash)
			for _, file := range bundle.Files {
				size += len(file.Path) + len(file.Text) + len(file.Hash)
			}
			if size > maxPublishedBundleBytes {
				return nil, errors.New("Gateway publication contains an oversized contract bundle")
			}
		}
		if err := repository.Publish(entry.Bundles...); err != nil {
			return nil, err
		}
		for _, bundle := range entry.Bundles {
			references = append(references, rpcrouter.ContractReference{ImportPath: bundle.ImportPath, Hash: bundle.Hash})
		}
		sort.Slice(references, func(i, j int) bool {
			if references[i].ImportPath != references[j].ImportPath {
				return references[i].ImportPath < references[j].ImportPath
			}
			return references[i].Hash < references[j].Hash
		})
		snapshot.Providers[index] = PublishedProvider{ID: id, Contract: contract, Options: rpcrouter.RegistrationOptions{Name: id}, References: references}
	}
	normalized, err := normalizePublicationSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	snapshot = normalized
	publisher := &Publisher{published: make(chan struct{})}
	publisher.provider, err = NewMiniGoGatewayControlPublicationProvider(publicationHandler{
		snapshot: snapshot, repository: repository, publisher: publisher,
	})
	if err != nil {
		return nil, err
	}
	return publisher, nil
}

type publicationHandler struct {
	snapshot   PublicationSnapshot
	repository *rpcrouter.ContractRepository
	publisher  *Publisher
}

func (handler publicationHandler) Snapshot(context.Context) (MiniGoGatewayControlPublicationSnapshot, error) {
	return publicationSnapshotToWire(handler.snapshot), nil
}

func (handler publicationHandler) Resolve(ctx context.Context, importPath, hash string) (MiniGoGatewayControlContractBundle, error) {
	bundle, err := handler.repository.ResolveContract(ctx, rpcrouter.ContractReference{ImportPath: importPath, Hash: hash})
	return contractBundleToWire(bundle), err
}

func (handler publicationHandler) Ready(_ context.Context, snapshotID string) error {
	if snapshotID != handler.snapshot.ID {
		return rpc.StatusError{Code: rpc.CodeProtocol, Message: "publication Ready snapshot identity mismatch"}
	}
	handler.publisher.mu.Lock()
	if handler.publisher.state == publicationCandidate {
		handler.publisher.state = publicationReady
	}
	handler.publisher.mu.Unlock()
	return nil
}

func (handler publicationHandler) Published(_ context.Context, snapshotID string) error {
	if snapshotID != handler.snapshot.ID {
		return rpc.StatusError{Code: rpc.CodeProtocol, Message: "publication Published snapshot identity mismatch"}
	}
	handler.publisher.mu.Lock()
	switch handler.publisher.state {
	case publicationCandidate:
		handler.publisher.mu.Unlock()
		return rpc.StatusError{Code: rpc.CodeProtocol, Message: "publication is not ready"}
	case publicationReady:
		handler.publisher.state = publicationPublished
		close(handler.publisher.published)
	}
	handler.publisher.mu.Unlock()
	return nil
}

// Provider returns the publication control provider exposed on /publish.
func (p *Publisher) Provider() rpc.Provider {
	if p == nil {
		return nil
	}
	return p.provider
}

// WaitPublished waits until the Gateway exposes the exact snapshot.
func (p *Publisher) WaitPublished(ctx context.Context) error {
	if p == nil {
		return errors.New("Gateway publisher is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-p.published:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type publicationClient struct {
	client *MiniGoGatewayControlPublicationClient
}

func newPublicationClient(ctx context.Context, binder rpc.Binder) (*publicationClient, error) {
	client, err := BindMiniGoGatewayControlPublicationClient(ctx, binder, rpc.BindOptions{})
	if err != nil {
		return nil, err
	}
	return &publicationClient{client: client}, nil
}

func (c *publicationClient) Snapshot(ctx context.Context) (PublicationSnapshot, error) {
	wired, err := c.client.Snapshot(ctx)
	if err != nil {
		return PublicationSnapshot{}, err
	}
	snapshot, err := publicationSnapshotFromWire(wired)
	if err != nil {
		return PublicationSnapshot{}, err
	}
	snapshot, err = normalizePublicationSnapshot(snapshot)
	if err != nil {
		return PublicationSnapshot{}, err
	}
	return snapshot, nil
}

func (c *publicationClient) Resolve(ctx context.Context, reference rpcrouter.ContractReference) (rpcrouter.MRPCBundle, error) {
	wired, err := c.client.Resolve(ctx, reference.ImportPath, reference.Hash)
	if err != nil {
		return rpcrouter.MRPCBundle{}, err
	}
	bundle := contractBundleFromWire(wired)
	normalized, err := rpcrouter.NewMRPCBundle(bundle.ImportPath, bundle.Files)
	if err != nil || normalized.ImportPath != reference.ImportPath || normalized.Hash != reference.Hash {
		return rpcrouter.MRPCBundle{}, errors.New("publication Resolve returned a different bundle identity")
	}
	return normalized, nil
}

func (c *publicationClient) Ready(ctx context.Context, snapshotID string) error {
	return c.client.Ready(ctx, snapshotID)
}

func (c *publicationClient) Published(ctx context.Context, snapshotID string) error {
	return c.client.Published(ctx, snapshotID)
}

func (c *publicationClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

func normalizePublicationSnapshot(snapshot PublicationSnapshot) (PublicationSnapshot, error) {
	if snapshot.Protocol != PublicationProtocol || strings.TrimSpace(snapshot.ProcessID) == "" || snapshot.Generation == 0 || len(snapshot.Providers) == 0 {
		return PublicationSnapshot{}, errors.New("invalid Gateway publication snapshot")
	}
	if len(snapshot.Providers) > maxPublishedProviders {
		return PublicationSnapshot{}, errors.New("Gateway publication contains too many providers")
	}
	snapshot.ProcessID = strings.TrimSpace(snapshot.ProcessID)
	seenProviders := make(map[string]struct{}, len(snapshot.Providers))
	for index := range snapshot.Providers {
		provider := &snapshot.Providers[index]
		provider.ID = strings.TrimSpace(provider.ID)
		if provider.ID == "" {
			return PublicationSnapshot{}, errors.New("Gateway publication contains an empty provider ID")
		}
		if _, duplicate := seenProviders[provider.ID]; duplicate {
			return PublicationSnapshot{}, errors.New("Gateway publication contains duplicate provider ID " + provider.ID)
		}
		seenProviders[provider.ID] = struct{}{}
		contract, err := rpc.NormalizeContract(provider.Contract, rpc.NormalizeLimits(rpc.Limits{}))
		if err != nil {
			return PublicationSnapshot{}, err
		}
		provider.Contract = contract
		provider.Options.Name = provider.ID
		if len(provider.Options.Labels) > maxProviderLabels {
			return PublicationSnapshot{}, errors.New("Gateway publication provider contains too many labels")
		}
		if len(provider.References) > maxProviderReferences {
			return PublicationSnapshot{}, errors.New("Gateway publication provider contains too many contract references")
		}
		seenReferences := make(map[rpcrouter.ContractReference]struct{}, len(provider.References))
		for _, reference := range provider.References {
			if strings.TrimSpace(reference.ImportPath) == "" || len(reference.Hash) != sha256.Size*2 || strings.ToLower(reference.Hash) != reference.Hash {
				return PublicationSnapshot{}, errors.New("Gateway publication contains an invalid contract reference")
			}
			if _, err := hex.DecodeString(reference.Hash); err != nil {
				return PublicationSnapshot{}, errors.New("Gateway publication contains an invalid contract hash")
			}
			if _, duplicate := seenReferences[reference]; duplicate {
				return PublicationSnapshot{}, errors.New("Gateway publication contains duplicate contract references")
			}
			seenReferences[reference] = struct{}{}
		}
		sort.Slice(provider.References, func(i, j int) bool {
			if provider.References[i].ImportPath != provider.References[j].ImportPath {
				return provider.References[i].ImportPath < provider.References[j].ImportPath
			}
			return provider.References[i].Hash < provider.References[j].Hash
		})
	}
	sort.Slice(snapshot.Providers, func(i, j int) bool { return snapshot.Providers[i].ID < snapshot.Providers[j].ID })
	wantID := snapshot.ID
	snapshot.ID = ""
	material, err := json.Marshal(snapshot)
	if err != nil {
		return PublicationSnapshot{}, err
	}
	sum := sha256.Sum256(material)
	snapshot.ID = hex.EncodeToString(sum[:])
	if wantID != "" && wantID != snapshot.ID {
		return PublicationSnapshot{}, errors.New("Gateway publication snapshot identity mismatch")
	}
	return snapshot, nil
}
