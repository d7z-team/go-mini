package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/d7z-team/mini-go/ffi"
)

type instanceLifecycle uint32

const (
	instanceOpen instanceLifecycle = iota
	instanceFaulted
	instanceClosing
	instanceClosed
)

// ErrInstanceFaulted reports that an unrecovered panic or runtime failure left
// the instance state unsuitable for another execution or patch.
var ErrInstanceFaulted = errors.New("runtime instance is faulted")

type Instance struct {
	vm               *vm
	active           *Execution
	lifecycle        atomic.Uint32
	terminalMu       sync.RWMutex
	terminalErr      error
	debugMu          sync.RWMutex
	debugPaused      *Execution
	doneOnce         sync.Once
	done             chan struct{}
	supervisor       chan struct{}
	patchMu          sync.Mutex
	pendingPatch     *PatchPlan
	shutdownOnce     sync.Once
	shutdownDone     chan struct{}
	shutdownErr      error
	hostCapabilities map[string]struct{}
}

func (p *Program) Instantiate(ctx context.Context, options InstanceOptions) (*Instance, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateLimits(options.Limits); err != nil {
		return nil, err
	}
	installed := make(map[string]struct{})
	if bridge, ok := options.FFI.(ffi.CapabilityBridge); ok {
		for _, capability := range bridge.HostCapabilities() {
			if strings.TrimSpace(capability) != capability || capability == "" {
				return nil, fmt.Errorf("FFI bridge advertises invalid host capability %q", capability)
			}
			if _, duplicate := installed[capability]; duplicate {
				return nil, fmt.Errorf("FFI bridge advertises duplicate host capability %q", capability)
			}
			installed[capability] = struct{}{}
		}
	}
	if p != nil && p.code != nil {
		for _, capability := range p.code.image.Capabilities {
			if _, ok := installed[capability]; !ok {
				return nil, fmt.Errorf("program requires unavailable host capability %q", capability)
			}
		}
	}
	if options.FFI != nil {
		session, err := options.FFI.Open(ctx)
		if err != nil {
			return nil, fmt.Errorf("open FFI session: %w", err)
		}
		if session == nil {
			return nil, errors.New("FFI bridge returned a nil session")
		}
		options.ffiSession = session
	}
	vm, err := p.newVM(options)
	if err != nil {
		if options.ffiSession != nil {
			err = errors.Join(err, options.ffiSession.Shutdown(context.Background()))
		}
		return nil, err
	}
	instance := &Instance{
		vm: vm, done: make(chan struct{}), supervisor: make(chan struct{}, 1),
		shutdownDone: make(chan struct{}), hostCapabilities: installed,
	}
	vm.instance = instance
	if _, hasInit := vm.rootModule().executable.Functions[moduleInitFunctionID]; hasInit {
		execution, startErr := instance.start(ctx, false, func(*instanceRevision) (int64, error) {
			scopeID, prepared, prepareErr := vm.prepareRootInitialization()
			if prepareErr != nil {
				return 0, prepareErr
			}
			if !prepared {
				return 0, errors.New("root module initialization was not prepared")
			}
			return scopeID, nil
		})
		if startErr == nil {
			_, startErr = execution.Wait(ctx)
		}
		if startErr != nil {
			_ = instance.Shutdown(context.Background())
			return nil, fmt.Errorf("initialize root module: %w", startErr)
		}
	}
	go instance.supervise()
	return instance, nil
}
