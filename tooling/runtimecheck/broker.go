package runtimecheck

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/d7z-team/mini-go/ffi"
	"github.com/d7z-team/mini-go/rpc"
	"github.com/d7z-team/mini-go/stdlib/host/console"
	oshost "github.com/d7z-team/mini-go/stdlib/host/os"
)

func newStandardHost() (*rpc.Host, error) {
	filesystem, err := oshost.NewMemoryFilesystem(nil)
	if err != nil {
		return nil, err
	}
	factories := []func() (rpc.Provider, error){
		func() (rpc.Provider, error) { return console.NewFmtConsoleProvider(console.NewBuffer("")) },
		func() (rpc.Provider, error) { return oshost.NewFilesystemProvider(filesystem) },
		func() (rpc.Provider, error) { return oshost.NewEnvironmentProvider(oshost.Map{}) },
	}
	providers := make([]rpc.Provider, 0, len(factories))
	for _, create := range factories {
		provider, err := create()
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return rpc.NewHost(rpc.HostOptions{Providers: providers})
}

type brokerCommand struct {
	Operation string `json:"operation"`
	ID        uint64 `json:"id"`
	Route     string `json:"route,omitempty"`
	Payload   []byte `json:"payload,omitempty"`
}

type brokerEvent struct {
	Event        string   `json:"event"`
	Version      int      `json:"version,omitempty"`
	ID           uint64   `json:"id,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Payload      []byte   `json:"payload,omitempty"`
	Code         string   `json:"code,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// RunHostBroker serves opaque FFI calls using the actual Go stdlib providers.
// JSON lines carry base64 byte buffers; the broker never interprets guest code.
func RunHostBroker(ctx context.Context, input io.Reader, output io.Writer) error {
	host, err := newStandardHost()
	if err != nil {
		return err
	}
	defer host.Close()
	return serveHostBroker(ctx, host, input, output)
}

func serveHostBroker(ctx context.Context, bridge ffi.Bridge, input io.Reader, output io.Writer) (result error) {
	lifetime, cancel := context.WithCancel(ctx)
	defer cancel()
	session, err := bridge.Open(lifetime)
	if err != nil {
		return err
	}
	var mu, outputMu sync.Mutex
	var outputErr error
	encoder := json.NewEncoder(output)
	emit := func(event brokerEvent) error {
		outputMu.Lock()
		defer outputMu.Unlock()
		if outputErr == nil {
			outputErr = encoder.Encode(event)
		}
		return outputErr
	}
	calls := map[uint64]ffi.Call{}
	results := map[uint64]func(){}
	closing := false
	shutdown := func() error {
		mu.Lock()
		if closing {
			mu.Unlock()
			return nil
		}
		closing = true
		ownedCalls, ownedResults := calls, results
		calls, results = map[uint64]ffi.Call{}, map[uint64]func(){}
		mu.Unlock()
		cancel()
		for _, call := range ownedCalls {
			if call != nil {
				call.Cancel()
			}
		}
		for _, discard := range ownedResults {
			if discard != nil {
				discard()
			}
		}
		return session.Shutdown(context.Background())
	}
	defer func() {
		result = errors.Join(result, shutdown())
		outputMu.Lock()
		result = errors.Join(result, outputErr)
		outputMu.Unlock()
	}()
	var capabilities []string
	if declared, ok := bridge.(ffi.CapabilityBridge); ok {
		capabilities = declared.HostCapabilities()
	}
	if err := emit(brokerEvent{Event: "ready", Version: 1, Capabilities: capabilities}); err != nil {
		return err
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 32<<20)
	var lastID uint64
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var command brokerCommand
		if err := json.Unmarshal(scanner.Bytes(), &command); err != nil {
			return err
		}
		switch command.Operation {
		case "start":
			if command.ID == 0 || command.ID <= lastID {
				return errors.New("broker call IDs must increase")
			}
			lastID = command.ID
			mu.Lock()
			if len(calls)+len(results) >= 4096 {
				mu.Unlock()
				if err := emit(brokerEvent{Event: "start_error", ID: command.ID, Code: "pending_limit", Error: "broker call capacity exceeded"}); err != nil {
					return err
				}
				continue
			}
			calls[command.ID] = nil
			mu.Unlock()
			id := command.ID
			call, startErr := session.Start(lifetime, ffi.Request{Route: command.Route, Payload: command.Payload}, func(result ffi.Result) {
				mu.Lock()
				_, active := calls[id]
				if closing || !active {
					mu.Unlock()
					if result.Discard != nil {
						result.Discard()
					}
					return
				}
				delete(calls, id)
				results[id] = result.Discard
				mu.Unlock()
				event := brokerEvent{Event: "result", ID: id, Payload: result.Payload}
				if result.Err != nil {
					event.Code, event.Error = "host", result.Err.Error()
					if errors.Is(result.Err, ffi.ErrRouteUnavailable) {
						event.Code = "route_unavailable"
					}
				}
				_ = emit(event)
			})
			mu.Lock()
			if _, active := calls[id]; active {
				if startErr == nil {
					calls[id] = call
				} else {
					delete(calls, id)
				}
			}
			mu.Unlock()
			event := brokerEvent{Event: "started", ID: id}
			if startErr != nil {
				event.Event, event.Code, event.Error = "start_error", "host", startErr.Error()
				if errors.Is(startErr, ffi.ErrRouteUnavailable) {
					event.Code = "route_unavailable"
				}
			}
			if err := emit(event); err != nil {
				return err
			}
		case "cancel", "discard", "consumed":
			mu.Lock()
			call := calls[command.ID]
			if command.Operation == "cancel" {
				delete(calls, command.ID)
			}
			discard := results[command.ID]
			delete(results, command.ID)
			mu.Unlock()
			if command.Operation == "cancel" && call != nil {
				call.Cancel()
			}
			if command.Operation != "consumed" && discard != nil {
				discard()
			}
		case "shutdown":
			if err := shutdown(); err != nil {
				return err
			}
			return emit(brokerEvent{Event: "closed"})
		default:
			return fmt.Errorf("unknown broker operation %q", command.Operation)
		}
	}
	return scanner.Err()
}
