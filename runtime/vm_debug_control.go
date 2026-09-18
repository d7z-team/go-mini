package runtime

import (
	"context"
	"errors"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func (vm *vm) requestPause() error {
	if vm == nil {
		return errors.New("nil VM")
	}
	vm.hostPauseRequested.Store(true)
	return nil
}

func (vm *vm) resumePaused(stepMode debugStepMode) runOutcome {
	if vm == nil {
		return failedRun(errors.New("nil VM"))
	}
	if vm.paused == nil {
		return failedRun(errors.New("vm is not paused"))
	}
	if vm.machine != nil && vm.machine.paused != nil && vm.machine.paused.scope != nil {
		vm.activeRunID = vm.machine.paused.scope.id
	}
	state := vm.paused
	vm.paused = nil
	vm.debugger.clearPausedEvent()
	vm.skipDebugPause = debugResumePoint{
		RunID:      vm.activeRunID,
		Generation: state.frame.revisionGeneration(),
		ModulePath: state.frame.module.executable.Artifact.Module.Path,
		FunctionID: state.functionID,
		PC:         state.frame.pc,
		Active:     true,
	}
	if stepMode != "" {
		vm.debugStep = debugStepState{
			RunID:      vm.activeRunID,
			Mode:       stepMode,
			StartDepth: len(vm.debugParents) + len(vm.callStack),
			Active:     true,
		}
	}
	if vm.machine == nil {
		return failedRun(errors.New("paused VM has no execution machine"))
	}
	return vm.runPrepared(defaultPollQuantum)
}

func (vm *vm) shouldPauseForStep() bool {
	if vm == nil || !vm.debugStep.Active || vm.debugStep.RunID != vm.activeRunID {
		return false
	}
	switch vm.debugStep.Mode {
	case debugStepInto:
		return true
	case debugStepOver:
		return len(vm.debugParents)+len(vm.callStack) <= vm.debugStep.StartDepth
	case debugStepOut:
		return len(vm.debugParents)+len(vm.callStack) < vm.debugStep.StartDepth
	default:
		return false
	}
}

func (vm *vm) runFunction(module *moduleInstance, functionID string, args []vmValue) ([]vmValue, error) {
	executionContextID := vm.currentExecutionContextID()
	if executionContextID <= 0 {
		executionContextID = 1
	}
	if vm.machine == nil {
		return vm.runScheduledFunction(module, functionID, args, nil, executionContextID)
	}
	return nil, errors.New("artifact function re-entry must use a scheduler callback request")
}

func (vm *vm) beginRun() int64 {
	if vm == nil {
		return 0
	}
	vm.nextRunID++
	vm.activeRunID = vm.nextRunID
	return vm.activeRunID
}

func (vm *vm) finishForeground() {
	if vm == nil {
		return
	}
	vm.activeRunID = 0
	vm.callStack = nil
	vm.debugParents = nil
	vm.paused = nil
	vm.debugger.clearPausedEvent()
	vm.skipDebugPause = debugResumePoint{}
	vm.debugStep = debugStepState{}
	vm.hostPauseRequested.Store(false)
	if vm.machine != nil {
		vm.machine.foreground = nil
		if len(vm.machine.scopes) == 0 {
			vm.machine = nil
		}
	}
}

func (vm *vm) finishRun() {
	if vm == nil {
		return
	}
	if vm.machine != nil {
		vm.machine.cancelAllScopes(context.Canceled)
	}
	vm.closeTimers()
	vm.closeFFICalls()
	vm.activeRunID = 0
	vm.callStack = nil
	vm.debugParents = nil
	vm.paused = nil
	vm.debugger.clearPausedEvent()
	vm.skipDebugPause = debugResumePoint{}
	vm.debugStep = debugStepState{}
	vm.hostPauseRequested.Store(false)
	vm.machine = nil
	if vm.instance != nil {
		vm.instance.discardBackgroundPause()
	}
}

func (vm *vm) currentFrame() *frame {
	if vm == nil || len(vm.callStack) == 0 {
		return nil
	}
	return vm.callStack[len(vm.callStack)-1]
}

func (vm *vm) currentExecutionContextID() int64 {
	if frame := vm.currentFrame(); frame != nil && frame.executionContextID > 0 {
		return frame.executionContextID
	}
	if vm != nil && vm.activeRunID != 0 {
		return 1
	}
	return 0
}

func (vm *vm) nextSpawnExecutionContextID() int64 {
	if vm == nil {
		return 0
	}
	vm.nextExecutionContextID++
	return vm.nextExecutionContextID
}

func runtimeLocation(frame *frame, functionID string, pc int) *ir.Location {
	if frame == nil || frame.revision == nil || frame.module == nil {
		return nil
	}
	loc, ok := frame.revision.symbols.nearestLocation(frame.module.modulePath(), functionID, pc)
	if !ok {
		return nil
	}
	return &loc
}

func (vm *vm) runtimeInstructionError(frame *frame, functionID string, pc int, inst *preparedInstruction, err error) Error {
	return Error{
		Generation:         frame.revisionGeneration(),
		ProgramHash:        frame.revisionHash(),
		ModulePath:         frame.module.executable.Artifact.Module.Path,
		FunctionID:         functionID,
		PC:                 pc,
		Op:                 inst.opcodeText(),
		ExecutionContextID: frame.executionContextID,
		Loc:                runtimeLocation(frame, functionID, pc),
		Err:                err,
	}
}
