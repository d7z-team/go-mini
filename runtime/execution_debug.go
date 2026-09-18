package runtime

import (
	"context"
	"errors"
	"fmt"
)

// Updates reports background debugger state changes after the invocation root returned.
func (e *Execution) Updates() <-chan struct{} {
	if e == nil {
		return nil
	}
	return e.updates
}

func (e *Execution) Continue() (RunResult, error) { return e.resumeHost("") }
func (e *Execution) StepInto() (RunResult, error) { return e.resumeHost(debugStepInto) }
func (e *Execution) StepOver() (RunResult, error) { return e.resumeHost(debugStepOver) }
func (e *Execution) StepOut() (RunResult, error)  { return e.resumeHost(debugStepOut) }

func (e *Execution) RequestPause() error {
	if e == nil || e.instance == nil || e.instance.vm == nil {
		return errors.New("invalid execution")
	}
	state := e.State()
	if state != ExecutionRunning && state != ExecutionPending {
		return fmt.Errorf("execution is %s", state)
	}
	return e.instance.vm.requestPause()
}

func (e *Execution) resumeHost(mode debugStepMode) (RunResult, error) {
	result, err := e.resumeDebug(mode)
	if err != nil {
		return RunResult{}, err
	}
	return hostResult(result, e.instance.vm.limits)
}

func (e *Execution) resumeDebug(mode debugStepMode) (vmResult, error) {
	if e == nil || e.instance == nil {
		return vmResult{}, errors.New("execution is not paused")
	}
	background := e.instance.isBackgroundPaused(e)
	if e.State() != ExecutionPaused && !background {
		return vmResult{}, errors.New("execution is not paused")
	}
	if err := e.instance.vm.enterOwnerContext(context.Background()); err != nil {
		return vmResult{}, err
	}
	defer e.instance.vm.leaveOwner()
	background = e.instance.isBackgroundPaused(e)
	if e.State() != ExecutionPaused && !background {
		return vmResult{}, errors.New("execution is not paused")
	}
	if mode != "" {
		paused := e.instance.vm.paused
		if paused == nil || paused.frame == nil || paused.frame.revision == nil || paused.frame.revision.symbols == nil {
			return vmResult{}, ErrDebugSymbolsUnavailable
		}
	}
	outcome := e.instance.vm.resumePaused(mode)
	if background {
		e.instance.clearBackgroundPause(e)
		e.mu.Lock()
		e.pause = nil
		e.debugInspection = nil
		e.mu.Unlock()
		switch outcome.state {
		case ExecutionPaused:
			e.captureBackgroundPause(outcome.pause)
		case ExecutionFailed, ExecutionCanceled:
			e.instance.fail(outcome.err)
		default:
			e.instance.signalSupervisor()
		}
		e.notify()
		return vmResult{}, outcome.err
	}
	e.capture(outcome)
	_, result, executionErr := e.stateSnapshot()
	return result, executionErr
}

func (e *Execution) captureBackgroundPause(pause *debugEvent) {
	if e == nil || pause == nil {
		return
	}
	e.mu.Lock()
	e.pause = pause
	e.err = nil
	e.buildDebugInspectionLocked()
	e.mu.Unlock()
	e.instance.setBackgroundPause(e)
	e.notify()
}

func (e *Execution) PauseEvent() (Event, bool) {
	if e == nil {
		return Event{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.pause == nil {
		return Event{}, false
	}
	return e.pause.publicEvent(), true
}

func (e *Execution) DebugError() (ExecutionError, bool) {
	if e == nil {
		return ExecutionError{}, false
	}
	e.mu.RLock()
	executionErr := e.err
	e.mu.RUnlock()
	if executionErr == nil {
		return ExecutionError{}, false
	}
	err, projectionErr := ProjectExecutionError(executionErr)
	if projectionErr != nil {
		return ExecutionError{}, false
	}
	return err, true
}
