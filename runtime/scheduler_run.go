package runtime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
)

func (machine *executionMachine) syncCallStack(task *executionTask) {
	machine.vm.callStack = machine.vm.callStack[:0]
	for _, active := range task.frames {
		machine.vm.callStack = append(machine.vm.callStack, active.frame)
	}
	machine.vm.debugParents = append(machine.vm.debugParents[:0], task.debugParents...)
}

func (machine *executionMachine) debugParentSnapshot(task *executionTask) []debugFrame {
	out := append([]debugFrame(nil), task.debugParents...)
	for _, active := range task.frames {
		frame := active.frame
		if frame == nil || frame.module == nil || frame.module.executable == nil {
			continue
		}
		pc := frame.pc
		if pc > 0 {
			pc--
		}
		functionID := frame.function.Decl.ID
		loc, _ := frame.revision.symbols.nearestLocation(frame.module.modulePath(), functionID, pc)
		out = append(out, debugFrame{
			ScopeID:            task.scope.id,
			SymbolsHash:        frame.revision.symbolsHash(),
			SourceHash:         frame.revision.symbols.sourceHash(frame.module.modulePath(), loc.File),
			Generation:         frame.revisionGeneration(),
			ProgramHash:        frame.revisionHash(),
			hasSymbols:         frame.revision != nil && frame.revision.symbols != nil,
			ExecutionContextID: frame.executionContextID,
			ModulePath:         frame.module.modulePath(),
			FunctionID:         functionID,
			PC:                 pc,
			Loc:                loc,
		})
	}
	return out
}

func (vm *vm) pausedEvent() debugEvent {
	if vm == nil || vm.paused == nil || vm.paused.frame == nil {
		return debugEvent{}
	}
	if vm.debugger != nil {
		return vm.debugger.pausedEvent()
	}
	return debugEvent{}
}

func (machine *executionMachine) module(modulePath string) (*moduleInstance, error) {
	module, ok := machine.vm.moduleRegistry().module(strings.TrimSpace(modulePath))
	if !ok {
		return nil, fmt.Errorf("module %q is not loaded", modulePath)
	}
	return module, nil
}

func (machine *executionMachine) directCallTarget(frame *frame, inst *preparedInstruction) (*moduleInstance, loadedFunction, error) {
	current := machine.vm.revision.Load()
	if current != nil && frame.revision == current &&
		inst.callModuleIndex >= 0 && inst.callModuleIndex < len(current.moduleOrder) {
		module := current.moduleOrder[inst.callModuleIndex]
		if module != nil && inst.callFunctionIndex >= 0 && inst.callFunctionIndex < len(module.executable.FunctionOrder) {
			return module, module.executable.FunctionOrder[inst.callFunctionIndex], nil
		}
	}
	modulePath := inst.callModule
	if modulePath == "" {
		modulePath = strings.TrimSpace(inst.call.ModulePath)
		if modulePath == "" {
			modulePath = frame.module.modulePath()
		}
	}
	target, err := machine.module(modulePath)
	if err != nil {
		return nil, loadedFunction{}, err
	}
	function, ok := target.executable.Functions[inst.call.Function]
	if !ok {
		return nil, loadedFunction{}, fmt.Errorf("unknown function %q", inst.call.Function)
	}
	return target, function, nil
}

func (machine *executionMachine) initializeModule(task *executionTask, caller *executionFrame, module *moduleInstance, ready func(*executionTask, *executionFrame) error) error {
	if module == nil || module.executable == nil {
		return errors.New("nil module")
	}
	if module.state.initState == moduleReady {
		if ready == nil {
			return nil
		}
		return ready(task, caller)
	}
	modulePath := module.executable.Artifact.Module.Path
	if module.state.initState == moduleFailed {
		return module.state.initErr
	}
	if module.state.initState == moduleInitializing {
		return fmt.Errorf("module %q initialization cycle", modulePath)
	}
	if _, ok := module.executable.Functions[moduleInitFunctionID]; !ok {
		module.state.initState = moduleReady
		if ready == nil {
			return nil
		}
		return ready(task, caller)
	}
	module.state.beginInitialization()
	initFrame, err := machine.vm.newExecutionFrame(module, moduleInitFunctionID, nil, nil, task.id, 0, false)
	if err != nil {
		module.state.finishInitialization(err)
		return err
	}
	initFrame.abort = func() { module.state.finishInitialization(errors.New("module initialization aborted")) }
	initFrame.resume = func(task *executionTask, caller *executionFrame, _ []vmValue) error {
		module.state.finishInitialization(nil)
		initFrame.abort = nil
		if ready == nil {
			return nil
		}
		return ready(task, caller)
	}
	task.frames = append(task.frames, initFrame)
	return nil
}

func (machine *executionMachine) admitTasks(count int) error {
	active := machine.runnableCount() + len(machine.blocked)
	if machine.paused != nil {
		active++
	}
	if limit := machine.vm.limits.MaxTasks; limit > 0 && active+count > limit {
		return ResourceLimitError{Code: "execution.task_limit", Message: fmt.Sprintf("execution task limit exceeded: max %d", limit)}
	}
	return nil
}

func (machine *executionMachine) enqueueTask(task *executionTask) error {
	if task == nil {
		return errors.New("cannot enqueue nil execution task")
	}
	if err := machine.admitTasks(1); err != nil {
		return err
	}
	machine.addTask(task)
	machine.pushRunnable(task)
	return nil
}

const (
	taskYieldComplete  = "complete"
	taskYieldSpawn     = "spawn"
	taskYieldBlocked   = "blocked"
	taskYieldPause     = "pause"
	taskYieldCooperate = "cooperate"
	taskYieldPoll      = "poll"
)

func (vm *vm) newExecutionFrame(module *moduleInstance, functionID string, args []vmValue, upvalues map[string]*slot, contextID int64, expectedResults int, qualifyResults bool) (*executionFrame, error) {
	if module == nil || module.executable == nil {
		return nil, errors.New("nil module")
	}
	fn, ok := module.executable.Functions[functionID]
	if !ok {
		return nil, fmt.Errorf("unknown function %q", functionID)
	}
	return vm.newPreparedExecutionFrame(module, fn, args, upvalues, contextID, expectedResults, qualifyResults)
}

func (vm *vm) newPreparedExecutionFrame(module *moduleInstance, fn loadedFunction, args []vmValue, upvalues map[string]*slot, contextID int64, expectedResults int, qualifyResults bool) (*executionFrame, error) {
	if len(args) != len(fn.Decl.Signature.Params) {
		return nil, fmt.Errorf("function %s argument count mismatch: got %d, want %d", fn.Decl.ID, len(args), len(fn.Decl.Signature.Params))
	}
	if expectedResults >= 0 && expectedResults != len(fn.Decl.Signature.Results) {
		return nil, fmt.Errorf("function %s result count mismatch: got %d, want %d", fn.Decl.ID, expectedResults, len(fn.Decl.Signature.Results))
	}
	resultSlots := expectedResults
	if resultSlots < 0 {
		resultSlots = 0
	}
	callFrame, allocated, err := newFrame(module, fn, args, upvalues, contextID)
	if err != nil {
		return nil, err
	}
	if allocated {
		if err := vm.chargeRuntimeObject(len(fn.Decl.Locals)+fn.MaxStack+len(upvalues)+resultSlots, 0); err != nil {
			callFrame.recycle()
			return nil, err
		}
	}
	var pinnedRevision *instanceRevision
	if module.revision.retain() {
		pinnedRevision = module.revision
	}
	callFrame.execution = executionFrame{
		frame:           callFrame,
		expectedResults: expectedResults,
		qualifyResults:  qualifyResults,
		pinnedRevision:  pinnedRevision,
	}
	return &callFrame.execution, nil
}

func (vm *vm) runScheduledFunction(module *moduleInstance, functionID string, args []vmValue, upvalues map[string]*slot, contextID int64) ([]vmValue, error) {
	scopeID := vm.beginRun()
	machine, err := vm.prepareScheduledFunction(module, functionID, args, upvalues, contextID, scopeID, false)
	if err != nil {
		return nil, err
	}
	outcome := machine.run(0)
	if outcome.suspended() {
		if outcome.pause != nil {
			return nil, debugPauseError{Event: *outcome.pause}
		}
		return nil, ExecutionPendingError{}
	}
	if machine.foreground != nil {
		machine.publishScopeRoot(machine.foreground.id, outcome.state, outcome.err)
	}
	if outcome.err != nil {
		machine.abortQueuedTasks(outcome.err)
		vm.finishRun()
	} else {
		vm.finishForeground()
	}
	return outcome.result.Values, outcome.err
}

func (vm *vm) prepareScheduledFunction(module *moduleInstance, functionID string, args []vmValue, upvalues map[string]*slot, contextID, scopeID int64, program bool) (*executionMachine, error) {
	root, err := vm.newExecutionFrame(module, functionID, args, upvalues, contextID, -1, false)
	if err != nil {
		return nil, Error{
			Generation:  module.revision.generation,
			ProgramHash: module.revision.code.image.Hash,
			ModulePath:  module.modulePath(),
			FunctionID:  functionID,
			Err:         err,
		}
	}
	if err := vm.chargeAllocation(); err != nil {
		return nil, err
	}
	machine := vm.machine
	if machine == nil {
		machine = &executionMachine{vm: vm}
		vm.machine = machine
	}
	scope := machine.newScope(scopeID, contextID, program, module.revision)
	task := &executionTask{id: contextID, scope: scope, budget: scope.budget, frames: []*executionFrame{root}}
	if machine.foreground != nil {
		machine.abortTask(task)
		delete(machine.scopes, scopeID)
		return nil, errors.New("VM already has an active execution")
	}
	machine.foreground = scope
	if functionID != moduleInitFunctionID {
		if err := machine.initializeModule(task, root, module, nil); err != nil {
			machine.abortTask(task)
			machine.foreground = nil
			delete(machine.scopes, scopeID)
			return nil, err
		}
	}
	machine.addTask(task)
	machine.pushRunnable(task)
	return machine, nil
}

func (vm *vm) prepareRootInitialization() (int64, bool, error) {
	if vm == nil {
		return 0, false, errors.New("nil VM")
	}
	root := vm.rootModule()
	if root == nil || root.executable == nil {
		return 0, false, errors.New("nil root module")
	}
	if root.state.initState == moduleReady {
		return 0, false, nil
	}
	if _, ok := root.executable.Functions[moduleInitFunctionID]; !ok {
		root.state.initState = moduleReady
		return 0, false, nil
	}
	scopeID := vm.beginRun()
	contextID := vm.nextSpawnExecutionContextID()
	machine := vm.machine
	if machine == nil {
		machine = &executionMachine{vm: vm}
		vm.machine = machine
	}
	scope := machine.newScope(scopeID, contextID, false, root.revision)
	task := &executionTask{id: contextID, scope: scope, budget: scope.budget}
	machine.foreground = scope
	if err := machine.initializeModule(task, nil, root, nil); err != nil {
		machine.abortTask(task)
		machine.foreground = nil
		delete(machine.scopes, scopeID)
		vm.finishForeground()
		return 0, false, err
	}
	machine.addTask(task)
	machine.pushRunnable(task)
	return scopeID, true, nil
}

func (machine *executionMachine) run(instructionBudget int) runOutcome {
	machine.pollBudget = instructionBudget
	machine.pollAttempts = 0
	machine.pollExecuted = 0
	if machine.cancelRequestedScopes() != nil {
		return failedRun(context.Canceled)
	}
	if machine.paused != nil {
		machine.pushRunnableFront(machine.paused)
		machine.paused = nil
	}
	for {
		if machine.runnableCount() != 0 {
			task := machine.popRunnable()
			if task.scope != nil {
				machine.vm.activeRunID = task.scope.id
			} else {
				machine.vm.activeRunID = 0
			}
			machine.running = task
			yield, values, err := machine.runTask(task)
			machine.running = nil
			if err != nil {
				scopeCanceled := task.scope != nil && task.execution != nil && task.execution.cancelRequested.Load()
				scopeLimited := task.scope != nil && !task.scope.program && isScopePolicyError(err)
				if scopeCanceled || scopeLimited {
					scope := task.scope
					foreground := machine.foreground == scope
					machine.abortTask(task)
					machine.finishTask(task, nil, err)
					machine.cancelScope(scope, err)
					if foreground {
						return failedRun(err)
					}
					continue
				}
				machine.abortTask(task)
				if task.terminal != nil {
					machine.finishTask(task, nil, err)
					continue
				}
				machine.finishTask(task, nil, err)
				machine.abortQueuedTasks(err)
				return failedRun(err)
			}
			if yield.kind == taskYieldPoll {
				machine.pushRunnableFront(task)
				return runOutcome{state: ExecutionRunning}
			}
			if yield.kind != taskYieldPause {
				task.quantumSteps = 0
			}
			switch yield.kind {
			case taskYieldComplete:
				root := machine.foreground != nil && task.id == machine.foreground.rootID
				program := root && machine.foreground.program
				machine.finishTask(task, values, nil)
				if root {
					if program {
						machine.abortQueuedTasks(nil)
					}
					return runOutcome{state: ExecutionCompleted, result: vmResult{Values: values}}
				}
			case taskYieldSpawn:
				if err := machine.admitTasks(2); err != nil {
					machine.abortTask(yield.child)
					machine.abortTask(task)
					if task.terminal != nil {
						machine.finishTask(task, nil, err)
						continue
					}
					machine.finishTask(task, nil, err)
					machine.abortQueuedTasks(err)
					return failedRun(err)
				}
				machine.addTask(yield.child)
				machine.pushRunnable(yield.child, task)
			case taskYieldBlocked:
				machine.blocked = append(machine.blocked, task)
			case taskYieldPause:
				machine.paused = task
				event := machine.vm.pausedEvent()
				return runOutcome{state: ExecutionPaused, pause: &event}
			case taskYieldCooperate:
				machine.pushRunnable(task)
			default:
				return failedRun(fmt.Errorf("unknown task yield %q", yield.kind))
			}
			if err := machine.wakeBlocked(); err != nil {
				return failedRun(err)
			}
			continue
		}
		if err := machine.wakeBlocked(); err != nil {
			return failedRun(err)
		}
		if machine.runnableCount() != 0 {
			continue
		}
		if len(machine.blocked) != 0 {
			if len(machine.vm.timers) != 0 {
				return runOutcome{state: ExecutionPending}
			}
			for _, task := range machine.blocked {
				if task.blocked != nil && task.blocked.kind == "ffi" {
					return runOutcome{state: ExecutionPending}
				}
			}
			if machine.foreground == nil {
				return runOutcome{state: ExecutionPending}
			}
			return failedRun(machine.allBlockedError())
		}
		machine.vm.callStack = nil
		if machine.foreground == nil {
			return runOutcome{state: ExecutionPending}
		}
		return failedRun(errors.New("root execution context did not complete"))
	}
}

func (machine *executionMachine) runTask(task *executionTask) (taskYield, []vmValue, error) {
	for {
		if task.pendingErr != nil {
			err := task.pendingErr
			task.pendingErr = nil
			return taskYield{}, nil, err
		}
		if len(task.frames) == 0 {
			return taskYield{kind: taskYieldComplete}, nil, errors.New("execution task has no frame")
		}
		if limit := machine.vm.limits.MaxCallDepth; limit > 0 && len(task.frames) > limit {
			return taskYield{}, nil, fmt.Errorf("execution call depth limit exceeded: max %d", limit)
		}
		current := task.frames[len(task.frames)-1]
		if current.completion != nil {
			values, done, err := machine.advanceCompletion(task)
			if err != nil || done {
				return taskYield{kind: taskYieldComplete}, values, err
			}
			if machine.blockedWakePending {
				machine.blockedWakePending = false
				if err := machine.wakeBlocked(); err != nil {
					return taskYield{}, nil, err
				}
			}
			continue
		}
		callFrame := current.frame
		functionID := callFrame.function.Decl.ID
		if callFrame.pc >= len(callFrame.function.Instructions) {
			if len(callFrame.stack) != 0 {
				return taskYield{}, nil, Error{
					Generation:  callFrame.revisionGeneration(),
					ProgramHash: callFrame.revisionHash(),
					ModulePath:  callFrame.module.modulePath(),
					FunctionID:  functionID,
					Err:         fmt.Errorf("function completed with %d stack values", len(callFrame.stack)),
				}
			}
			var values []vmValue
			var err error
			if callFrame.hasResultLocals() {
				values, err = callFrame.currentResultValues()
			} else {
				values, err = callFrame.normalizeReturnValues(nil)
			}
			if err != nil {
				return taskYield{}, nil, Error{
					Generation:  callFrame.revisionGeneration(),
					ProgramHash: callFrame.revisionHash(),
					ModulePath:  callFrame.module.modulePath(),
					FunctionID:  functionID,
					Err:         err,
				}
			}
			current.completion = &frameCompletion{returnValues: values}
			continue
		}
		pc := callFrame.pc
		inst := &callFrame.function.Instructions[pc]
		if task.execution != nil && task.execution.cancelRequested.Load() {
			return taskYield{}, nil, context.Canceled
		}
		if machine.vm.interruptRequested.Load() {
			return taskYield{}, nil, context.Canceled
		}
		if machine.pollBudget > 0 && machine.pollAttempts >= machine.pollBudget {
			return taskYield{kind: taskYieldPoll}, nil, nil
		}
		if task.quantumSteps >= taskInstructionQuantum && machine.runnableCount() != 0 && !task.inNoSwitchRegion() {
			return taskYield{kind: taskYieldCooperate}, nil, nil
		}
		machine.pollAttempts++
		if task.quantumSteps < taskInstructionQuantum {
			task.quantumSteps++
		}
		debugging := machine.vm.breakpointsActive.Load() || machine.vm.debugStep.Active || machine.vm.hostPauseRequested.Load()
		if debugging {
			machine.syncCallStack(task)
			if _, ok := machine.vm.checkDebugPause(callFrame, functionID, pc); ok {
				if machine.vm.paused == nil {
					machine.vm.paused = &debugPauseState{frame: callFrame, functionID: functionID}
				}
				return taskYield{kind: taskYieldPause}, nil, nil
			}
		}
		callFrame.pc++
		if err := task.consumeStep(machine.vm.maxSteps); err != nil {
			return taskYield{}, nil, Error{
				Generation:         callFrame.revisionGeneration(),
				ProgramHash:        callFrame.revisionHash(),
				ModulePath:         callFrame.module.modulePath(),
				FunctionID:         functionID,
				PC:                 pc,
				Op:                 inst.opcodeText(),
				ExecutionContextID: callFrame.executionContextID,
				Loc:                runtimeLocation(callFrame, functionID, pc),
				Err:                err,
			}
		}
		machine.pollExecuted++
		if machine.vm.executedSteps < math.MaxInt64 {
			machine.vm.executedSteps++
		}
		if execution := task.execution; execution != nil && execution.profileEvery != 0 && task.budget.profilePhase&execution.profileMask == 0 {
			execution.recordGuestSample(callFrame, functionID, pc, inst)
		}
		if inst.control {
			yield, handled, err := machine.executeControl(task, current, pc, inst)
			if err != nil {
				var guestFault *guestPanic
				if errors.As(err, &guestFault) {
					machine.startPanic(task, current, pc, guestFault.value)
					continue
				}
				return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, err)
			}
			if !handled {
				return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, errors.New("prepared control opcode is not handled"))
			}
			if yield.kind != "" {
				return yield, nil, nil
			}
			continue
		}
		if err := machine.vm.executeInstruction(callFrame, inst); err != nil {
			var selectRequest *reflectSelectRequest
			if errors.As(err, &selectRequest) {
				index, selectErr := parkWaitSet(machine.vm, selectRequest.waitSet)
				if selectErr != nil {
					var blocked WaitBlockedError
					if !errors.As(selectErr, &blocked) {
						return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, selectErr)
					}
					machine.blockTask(task, current, pc, inst, &blockedOperation{
						kind: "waitset", waitSet: selectRequest.waitSet, selectDone: selectRequest.complete,
					})
					return taskYield{kind: taskYieldBlocked}, nil, nil
				}
				selected, _ := asInt64(index)
				values, selectErr := selectRequest.complete(int(selected))
				if selectErr != nil {
					machine.startPanic(task, current, pc, newVMValue("String", selectErr.Error()))
					continue
				}
				for _, value := range values {
					callFrame.push(value)
				}
				continue
			}
			var send *reflectSendRequest
			if errors.As(err, &send) {
				module := send.ctx.module
				resource, blocked, sendErr := waitableSendTaskValue(module, send.waitable, send.value, task.id)
				if sendErr != nil {
					machine.startPanic(task, current, pc, newVMValue("String", sendErr.Error()))
					continue
				}
				if blocked {
					machine.blockTask(task, current, pc, inst, &blockedOperation{
						kind: "send", resource: resource, waitable: send.waitable,
						sendDone: func() []vmValue { return []vmValue{newVMValue("String", ""), newVMValue("Bool", true)} },
					})
					return taskYield{kind: taskYieldBlocked}, nil, nil
				}
				callFrame.push(newVMValue("String", ""))
				callFrame.push(newVMValue("Bool", true))
				continue
			}
			var recv *reflectRecvRequest
			if errors.As(err, &recv) {
				module := recv.ctx.module
				value, ok, closed, recvErr := waitableTryRecvValue(module, recv.waitable)
				if recvErr != nil {
					return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, recvErr)
				}
				if !ok && !closed {
					resource, resourceErr := waitableValueData(module, recv.waitable)
					if resourceErr != nil {
						return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, resourceErr)
					}
					token := machine.vm.newWaitTokenValue()
					if waitErr := waitableWaitRecvValue(module, recv.waitable, token); waitErr != nil {
						return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, waitErr)
					}
					machine.blockTask(task, current, pc, inst, &blockedOperation{
						kind: "recv", resource: resource, waitable: recv.waitable, recvToken: token, recvDone: recv.complete,
					})
					return taskYield{kind: taskYieldBlocked}, nil, nil
				}
				values, recvErr := recv.complete(value, ok)
				if recvErr != nil {
					return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, recvErr)
				}
				if len(values) != inst.callIntrinsic.ResultCount {
					return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, fmt.Errorf("reflect receive resumed with %d results, expected %d", len(values), inst.callIntrinsic.ResultCount))
				}
				for _, value := range values {
					callFrame.push(value)
				}
				continue
			}
			var request *artifactCallbackRequest
			if errors.As(err, &request) {
				callee, frameErr := machine.vm.newExecutionFrame(request.module, request.functionID, request.args, request.upvalues, task.id, request.resultCount, false)
				if frameErr != nil {
					return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, frameErr)
				}
				callee.resume = func(task *executionTask, caller *executionFrame, results []vmValue) error {
					values, resumeErr := request.resume(results)
					if resumeErr == nil && len(values) != request.resumeResults {
						resumeErr = fmt.Errorf("artifact callback %s resumed with %d results, expected %d", request.functionID, len(values), request.resumeResults)
					}
					if resumeErr != nil {
						machine.startPanic(task, caller, caller.frame.pc-1, newVMValue("String", resumeErr.Error()))
						return nil
					}
					for _, value := range values {
						caller.frame.push(value)
					}
					return nil
				}
				task.frames = append(task.frames, callee)
				continue
			}
			var guestFault *guestPanic
			if errors.As(err, &guestFault) {
				machine.startPanic(task, current, pc, guestFault.value)
				continue
			}
			return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, err)
		}
		if len(callFrame.stack) != 0 && (inst.op == preparedBinary || inst.op == preparedConvert || inst.op == preparedCallIntrinsic) {
			if err := machine.vm.validateRuntimeValue(callFrame.stack[len(callFrame.stack)-1]); err != nil {
				return taskYield{}, nil, machine.vm.runtimeInstructionError(callFrame, functionID, pc, inst, err)
			}
		}
	}
}
