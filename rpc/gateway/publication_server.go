package gateway

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/d7z-team/mini-go/rpc"
	rpcrouter "github.com/d7z-team/mini-go/rpc/router"
)

const publicationWebSocketPath = "/publish"

type remotePublication struct {
	peerIdentity string
	processID    string
	generation   uint64
	snapshotID   string
	endpoint     *rpc.Endpoint
	publication  *rpcrouter.Publication
	committed    bool
}

// PublicationRegistryOptions bounds publication control and retirement work.
type PublicationRegistryOptions struct {
	ControlTimeout time.Duration
	DrainTimeout   time.Duration
}

// PublicationRegistry owns remote provider publications accepted by a
// WebSocket Server. The routed Gateway remains independently owned.
type PublicationRegistry struct {
	gateway        *rpcrouter.Router
	controlTimeout time.Duration
	drainTimeout   time.Duration
	mu             sync.Mutex
	active         map[string]*remotePublication
}

// NewPublicationRegistry constructs the remote publication coordinator for a
// routed Gateway.
func NewPublicationRegistry(gateway *rpcrouter.Router, options PublicationRegistryOptions) (*PublicationRegistry, error) {
	if gateway == nil {
		return nil, errors.New("remote publication registry requires a Gateway")
	}
	if options.ControlTimeout <= 0 {
		options.ControlTimeout = 10 * time.Second
	}
	if options.DrainTimeout <= 0 {
		options.DrainTimeout = 30 * time.Second
	}
	return &PublicationRegistry{
		gateway: gateway, controlTimeout: options.ControlTimeout, drainTimeout: options.DrainTimeout,
		active: make(map[string]*remotePublication),
	}, nil
}

func (registry *PublicationRegistry) accept(ctx context.Context, endpoint *rpc.Endpoint, peer rpc.PeerInfo) error {
	controlCtx, cancel := context.WithTimeout(ctx, registry.controlTimeout)
	client, err := newPublicationClient(controlCtx, endpoint)
	cancel()
	if err != nil {
		return err
	}
	defer client.Close()
	controlCtx, cancel = context.WithTimeout(ctx, registry.controlTimeout)
	snapshot, err := client.Snapshot(controlCtx)
	cancel()
	if err != nil {
		return err
	}
	entries := make([]rpcrouter.MountedEntry, len(snapshot.Providers))
	for index, provider := range snapshot.Providers {
		bundles := make([]rpcrouter.MRPCBundle, len(provider.References))
		for bundleIndex, reference := range provider.References {
			controlCtx, cancel = context.WithTimeout(ctx, registry.controlTimeout)
			bundle, resolveErr := client.Resolve(controlCtx, reference)
			cancel()
			if resolveErr != nil {
				return resolveErr
			}
			bundles[bundleIndex] = bundle
		}
		entries[index] = rpcrouter.MountedEntry{Binder: endpoint, Contract: provider.Contract, Options: provider.Options, Bundles: bundles}
	}

	registry.mu.Lock()
	current := registry.active[snapshot.ProcessID]
	if current != nil && !current.committed {
		registry.mu.Unlock()
		return rpc.StatusError{Code: rpc.CodeUnavailable, Message: "remote publication commit is in progress"}
	}
	if err := validatePublicationIdentity(current, snapshot, peer.Identity); err != nil {
		registry.mu.Unlock()
		return err
	}
	var plan *rpcrouter.PublicationPlan
	if current == nil {
		plan, err = registry.gateway.PreparePublishMounts(entries)
	} else {
		plan, err = registry.gateway.PrepareReplaceMounts(current.publication, entries)
	}
	if err != nil {
		registry.mu.Unlock()
		return err
	}
	session := &remotePublication{
		peerIdentity: peer.Identity,
		processID:    snapshot.ProcessID, generation: snapshot.Generation, snapshotID: snapshot.ID,
		endpoint: endpoint,
	}
	registry.active[snapshot.ProcessID] = session
	registry.mu.Unlock()

	controlCtx, cancel = context.WithTimeout(ctx, registry.controlTimeout)
	err = client.Ready(controlCtx, snapshot.ID)
	cancel()
	if err != nil {
		return errors.Join(err, registry.abandon(session, current, plan))
	}
	registry.mu.Lock()
	if registry.active[session.processID] != session {
		registry.mu.Unlock()
		_ = plan.Close()
		return errors.New("remote publication was superseded while becoming ready")
	}
	publication, err := plan.Commit()
	if err != nil {
		if current != nil && !endpointClosed(current.endpoint) {
			registry.active[session.processID] = current
		} else {
			delete(registry.active, session.processID)
		}
		registry.mu.Unlock()
		if current != nil && endpointClosed(current.endpoint) {
			return errors.Join(err, current.publication.ForceClose(context.Background()))
		}
		return err
	}
	session.publication = publication
	session.committed = true
	registry.mu.Unlock()

	if err := registry.acknowledgePublished(ctx, endpoint, client, snapshot.ID); err != nil {
		registry.remove(session)
		if current != nil {
			go registry.retire(current)
		}
		return err
	}
	if current != nil {
		go registry.retire(current)
	}
	<-endpoint.Done()
	registry.remove(session)
	return nil
}

func validatePublicationIdentity(current *remotePublication, snapshot PublicationSnapshot, identity string) error {
	if current != nil && current.peerIdentity != identity {
		return rpc.StatusError{Code: rpc.CodePermissionDenied, Message: "publication belongs to another authenticated peer"}
	}
	if current != nil && snapshot.Generation < current.generation {
		return errors.New("remote publication generation is stale")
	}
	if current != nil && snapshot.Generation == current.generation && snapshot.ID != current.snapshotID {
		return errors.New("remote publication generation changed content")
	}
	return nil
}

func (registry *PublicationRegistry) abandon(session, previous *remotePublication, plan *rpcrouter.PublicationPlan) error {
	err := plan.Close()
	registry.mu.Lock()
	if registry.active[session.processID] != session {
		registry.mu.Unlock()
		return err
	}
	if previous != nil && !endpointClosed(previous.endpoint) {
		registry.active[session.processID] = previous
		registry.mu.Unlock()
		return err
	}
	delete(registry.active, session.processID)
	registry.mu.Unlock()
	if previous != nil && previous.publication != nil {
		err = errors.Join(err, previous.publication.ForceClose(context.Background()))
	}
	return err
}

func (registry *PublicationRegistry) acknowledgePublished(ctx context.Context, endpoint *rpc.Endpoint, client *publicationClient, snapshotID string) error {
	delay := 25 * time.Millisecond
	var lastErr error
	for {
		controlCtx, cancel := context.WithTimeout(ctx, registry.controlTimeout)
		err := client.Published(controlCtx, snapshotID)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(lastErr, ctx.Err())
		case <-endpoint.Done():
			timer.Stop()
			return lastErr
		}
		if delay < time.Second {
			delay *= 2
		}
	}
}

func (registry *PublicationRegistry) retire(session *remotePublication) {
	if session == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), registry.drainTimeout)
	err := session.publication.Close(ctx)
	cancel()
	if err != nil {
		_ = session.publication.ForceClose(context.Background())
	}
	_ = session.endpoint.Close()
}

func endpointClosed(endpoint *rpc.Endpoint) bool {
	if endpoint == nil {
		return true
	}
	select {
	case <-endpoint.Done():
		return true
	default:
		return false
	}
}

func (registry *PublicationRegistry) remove(session *remotePublication) {
	if registry == nil || session == nil {
		return
	}
	registry.mu.Lock()
	if registry.active[session.processID] == session {
		delete(registry.active, session.processID)
		registry.mu.Unlock()
		if session.publication != nil {
			_ = session.publication.ForceClose(context.Background())
		}
		return
	}
	registry.mu.Unlock()
}
