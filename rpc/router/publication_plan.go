package router

import (
	"errors"
	"sort"
	"sync"

	"github.com/d7z-team/mini-go/rpc"
)

// ProviderEntry describes one provider in an atomic publication.
type ProviderEntry struct {
	Provider rpc.Provider
	Options  RegistrationOptions
	Bundles  []MRPCBundle
}

// MountedEntry describes one provider reached through another Binder.
type MountedEntry struct {
	Binder   rpc.Binder
	Contract rpc.Contract
	Options  RegistrationOptions
	Bundles  []MRPCBundle
}

type publicationPlanState uint8

const (
	publicationPlanReady publicationPlanState = iota
	publicationPlanCommitted
	publicationPlanClosed
)

// PublicationPlan owns one validated, not-yet-visible publication change.
// A plan may be committed or closed exactly once.
type PublicationPlan struct {
	mu       sync.Mutex
	router   *Router
	current  *Publication
	prepared preparedPublication
	state    publicationPlanState
}

// Publish validates and exposes all entries atomically.
func (g *Router) Publish(entries []ProviderEntry) (*Publication, error) {
	plan, err := g.PreparePublish(entries)
	if err != nil {
		return nil, err
	}
	return plan.Commit()
}

// PublishMounts atomically exposes providers reached through remote Binders.
func (g *Router) PublishMounts(entries []MountedEntry) (*Publication, error) {
	plan, err := g.PreparePublishMounts(entries)
	if err != nil {
		return nil, err
	}
	return plan.Commit()
}

// Replace atomically installs entries in place of current. Existing leases on
// current remain pinned until they close, but it stops accepting new binds as
// soon as the new publication becomes visible.
func (g *Router) Replace(current *Publication, entries []ProviderEntry) (*Publication, error) {
	plan, err := g.PrepareReplace(current, entries)
	if err != nil {
		return nil, err
	}
	return plan.Commit()
}

// ReplaceMounts atomically replaces current with providers reached through
// remote Binders.
func (g *Router) ReplaceMounts(current *Publication, entries []MountedEntry) (*Publication, error) {
	plan, err := g.PrepareReplaceMounts(current, entries)
	if err != nil {
		return nil, err
	}
	return plan.Commit()
}

// PreparePublish validates entries without changing Router visibility.
func (g *Router) PreparePublish(entries []ProviderEntry) (*PublicationPlan, error) {
	return g.preparePublicationPlan(nil, entries)
}

// PrepareReplace validates an atomic replacement without changing current.
func (g *Router) PrepareReplace(current *Publication, entries []ProviderEntry) (*PublicationPlan, error) {
	if current == nil {
		return nil, errors.New("current RPC publication is required")
	}
	return g.preparePublicationPlan(current, entries)
}

// PreparePublishMounts validates remote Binder mounts without exposing them.
func (g *Router) PreparePublishMounts(entries []MountedEntry) (*PublicationPlan, error) {
	providers, err := g.mountedEntries(entries)
	if err != nil {
		return nil, err
	}
	return g.PreparePublish(providers)
}

// PrepareReplaceMounts validates remote Binder replacement without exposing it.
func (g *Router) PrepareReplaceMounts(current *Publication, entries []MountedEntry) (*PublicationPlan, error) {
	providers, err := g.mountedEntries(entries)
	if err != nil {
		return nil, err
	}
	return g.PrepareReplace(current, providers)
}

func (g *Router) mountedEntries(entries []MountedEntry) ([]ProviderEntry, error) {
	if g == nil {
		return nil, errors.New("RPC Router is nil")
	}
	providers := make([]ProviderEntry, len(entries))
	for index, entry := range entries {
		provider, err := newMountedProvider(entry.Binder, entry.Contract, g.limits)
		if err != nil {
			return nil, err
		}
		providers[index] = ProviderEntry{Provider: provider, Options: entry.Options, Bundles: entry.Bundles}
	}
	return providers, nil
}

func (g *Router) preparePublicationPlan(current *Publication, entries []ProviderEntry) (*PublicationPlan, error) {
	if g == nil {
		return nil, errors.New("RPC Router is nil")
	}
	prepared, err := g.preparePublication(entries)
	if err != nil {
		return nil, err
	}
	return &PublicationPlan{router: g, current: current, prepared: prepared, state: publicationPlanReady}, nil
}

// Commit atomically makes the prepared provider group visible.
func (p *PublicationPlan) Commit() (*Publication, error) {
	if p == nil {
		return nil, errors.New("RPC publication plan is nil")
	}
	p.mu.Lock()
	if p.state != publicationPlanReady {
		p.mu.Unlock()
		return nil, errors.New("RPC publication plan is no longer ready")
	}
	p.state = publicationPlanCommitted
	router, current, prepared := p.router, p.current, p.prepared
	p.router = nil
	p.current = nil
	p.prepared = preparedPublication{}
	p.mu.Unlock()
	return router.commitPublication(current, prepared)
}

// Close abandons a prepared publication change.
func (p *PublicationPlan) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.state == publicationPlanReady {
		p.state = publicationPlanClosed
		p.router = nil
		p.current = nil
		p.prepared = preparedPublication{}
	}
	p.mu.Unlock()
	return nil
}

func (g *Router) preparePublication(entries []ProviderEntry) (preparedPublication, error) {
	if len(entries) == 0 {
		return preparedPublication{}, errors.New("RPC publication requires at least one provider")
	}
	if err := duplicateProviderName(entries); err != nil {
		return preparedPublication{}, err
	}
	prepared := preparedPublication{entries: make([]preparedProviderEntry, len(entries)), pathHashes: make(map[string]string)}
	var bundles []MRPCBundle
	for index, entry := range entries {
		if entry.Provider == nil {
			return preparedPublication{}, errors.New("RPC publication contains a nil provider")
		}
		contract, err := rpc.NormalizeContract(entry.Provider.RPCContract(), g.limits)
		if err != nil {
			return preparedPublication{}, err
		}
		methods := make(map[rpc.Method]struct{}, len(contract.Methods))
		for _, method := range contract.Methods {
			methods[method] = struct{}{}
		}
		prepared.entries[index] = preparedProviderEntry{provider: entry.Provider, methods: methods, options: normalizeRegistrationOptions(entry.Options)}
		bundles = append(bundles, entry.Bundles...)
	}

	validation := NewContractRepository()
	if err := validation.Publish(bundles...); err != nil {
		return preparedPublication{}, err
	}
	references := make([]ContractReference, 0, len(bundles))
	seenReferences := make(map[ContractReference]struct{}, len(bundles))
	for _, bundle := range bundles {
		reference := ContractReference{ImportPath: bundle.ImportPath, Hash: bundle.Hash}
		if hash, exists := prepared.pathHashes[reference.ImportPath]; exists && hash != reference.Hash {
			return preparedPublication{}, errors.New("RPC publication contains multiple hashes for " + reference.ImportPath)
		}
		prepared.pathHashes[reference.ImportPath] = reference.Hash
		if _, duplicate := seenReferences[reference]; !duplicate {
			seenReferences[reference] = struct{}{}
			references = append(references, reference)
		}
	}
	sort.Slice(references, func(i, j int) bool {
		if references[i].ImportPath != references[j].ImportPath {
			return references[i].ImportPath < references[j].ImportPath
		}
		return references[i].Hash < references[j].Hash
	})
	prepared.bundles = bundles
	prepared.references = references
	return prepared, nil
}
