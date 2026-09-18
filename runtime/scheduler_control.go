package runtime

import (
	"errors"
	"fmt"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func (machine *executionMachine) executeControl(task *executionTask, current *executionFrame, pc int, inst *preparedInstruction) (taskYield, bool, error) {
	callFrame := current.frame
	defer callFrame.releasePopValues()
	switch inst.op {
	case preparedAddressOf:
		payload := inst.address
		module, err := machine.module(payload.ModulePath)
		if err != nil {
			return taskYield{}, true, err
		}
		ready := func(_ *executionTask, caller *executionFrame) error {
			export, ok := module.executable.Exports[payload.Export]
			if !ok || export.Kind != "global" {
				return fmt.Errorf("address target %s.%s is not an exported variable", payload.ModulePath, payload.Export)
			}
			if err := machine.vm.chargeAllocation(); err != nil {
				return err
			}
			value, err := caller.frame.slotAddress(module.state.globals[export.ID], *payload)
			if err != nil {
				return err
			}
			caller.frame.push(value)
			return nil
		}
		if module.state.initState == moduleInitializing {
			return taskYield{}, true, ready(task, current)
		}
		return taskYield{}, true, machine.initializeModule(task, current, module, ready)
	case preparedLoadExport:
		payload := inst.export
		module, err := machine.module(payload.ModulePath)
		if err != nil {
			return taskYield{}, true, err
		}
		ready := func(_ *executionTask, caller *executionFrame) error {
			value, err := loadInitializedExport(module, payload.Export)
			if err != nil {
				return err
			}
			caller.frame.push(value)
			return nil
		}
		if module.state.initState == moduleInitializing {
			return taskYield{}, true, ready(task, current)
		}
		return taskYield{}, true, machine.initializeModule(task, current, module, ready)
	case preparedInitModule:
		payload := inst.initModule
		module, err := machine.module(payload.ModulePath)
		if err != nil {
			return taskYield{}, true, err
		}
		return taskYield{}, true, machine.initializeModule(task, current, module, nil)
	case preparedCallDirect:
		payload := inst.call
		target, function, err := machine.directCallTarget(callFrame, inst)
		if err != nil {
			return taskYield{}, true, err
		}
		if target.state != callFrame.module.state && target.state.initState != moduleReady && target.state.initState != moduleInitializing {
			return taskYield{}, true, machine.initializeModule(task, current, target, func(task *executionTask, caller *executionFrame) error {
				args, err := caller.frame.popN(payload.ArgCount)
				if err != nil {
					return err
				}
				defer caller.frame.releasePopValues()
				qualify := target.modulePath() != caller.frame.module.modulePath()
				if qualify {
					args = caller.frame.module.qualifyValuesForArgumentBoundary(args)
				}
				callee, err := machine.vm.newPreparedExecutionFrame(target, function, args, nil, task.id, payload.ResultCount, qualify)
				if err != nil {
					return err
				}
				task.frames = append(task.frames, callee)
				return nil
			})
		}
		args, err := callFrame.popN(payload.ArgCount)
		if err != nil {
			return taskYield{}, true, err
		}
		qualify := target.modulePath() != callFrame.module.modulePath()
		if qualify {
			args = callFrame.module.qualifyValuesForArgumentBoundary(args)
		}
		callee, err := machine.vm.newPreparedExecutionFrame(target, function, args, nil, task.id, payload.ResultCount, qualify)
		if err != nil {
			return taskYield{}, true, err
		}
		task.frames = append(task.frames, callee)
		return taskYield{}, true, nil
	case preparedTailCallDirect:
		payload := inst.call
		target, function, err := machine.directCallTarget(callFrame, inst)
		if err != nil {
			return taskYield{}, true, err
		}
		if target.state != callFrame.module.state && target.state.initState != moduleReady && target.state.initState != moduleInitializing {
			err = machine.initializeModule(task, current, target, func(task *executionTask, caller *executionFrame) error {
				return machine.enterDirectTailCall(task, caller, target, function, payload)
			})
			return taskYield{}, true, err
		}
		return taskYield{}, true, machine.enterDirectTailCall(task, current, target, function, payload)
	case preparedCallValue:
		payload := inst.call
		values, err := callFrame.popN(payload.ArgCount + 1)
		if err != nil {
			return taskYield{}, true, err
		}
		if target, ok := values[0].Data.(reflectMakeFuncTarget); ok {
			args := values[1:]
			if target.handlerModule != callFrame.module {
				args = callFrame.module.qualifyValuesForArgumentBoundary(args)
			}
			request, err := target.callbackRequest(machine.vm, args)
			if err != nil {
				return taskYield{}, true, err
			}
			callee, err := machine.vm.newExecutionFrame(request.module, request.functionID, request.args, request.upvalues, task.id, request.resultCount, false)
			if err != nil {
				return taskYield{}, true, err
			}
			callee.resume = func(task *executionTask, caller *executionFrame, results []vmValue) error {
				out, err := request.resume(results)
				if err == nil && len(out) != payload.ResultCount {
					err = fmt.Errorf("reflect MakeFunc returned %d values, expected %d", len(out), payload.ResultCount)
				}
				if err != nil {
					machine.startPanic(task, caller, caller.frame.pc-1, newVMValue("String", err.Error()))
					return nil
				}
				for _, value := range out {
					caller.frame.push(value)
				}
				return nil
			}
			task.frames = append(task.frames, callee)
			return taskYield{}, true, nil
		}
		target, ref, err := machine.vm.resolveFunctionValue(callFrame.module, values[0])
		if err != nil {
			if errors.Is(err, errNilFunctionCall) {
				machine.startPanic(task, current, pc, newVMValue("String", errNilFunctionCall.Error()))
				return taskYield{}, true, nil
			}
			return taskYield{}, true, err
		}
		args := values[1:]
		qualify := target.modulePath() != callFrame.module.modulePath()
		if qualify {
			args = callFrame.module.qualifyValuesForArgumentBoundary(args)
		}
		callee, err := machine.vm.newExecutionFrame(target, ref.FunctionID, args, ref.upvalues, task.id, payload.ResultCount, qualify)
		if err != nil {
			return taskYield{}, true, err
		}
		task.frames = append(task.frames, callee)
		return taskYield{}, true, nil
	case preparedCallInterface:
		payload := inst.interfaceCall
		values, err := callFrame.popN(payload.ArgCount + 1)
		if err != nil {
			return taskYield{}, true, err
		}
		receiver, target, functionID, err := callFrame.module.resolveInterfaceMethod(values[0], callFrame.module.formatType(payload.InterfaceType), payload.Method)
		if err != nil {
			return taskYield{}, true, err
		}
		target, err = machine.module(target.modulePath())
		if err != nil {
			return taskYield{}, true, err
		}
		args := append([]vmValue{receiver}, values[1:]...)
		qualify := target.modulePath() != callFrame.module.modulePath()
		if qualify {
			args = callFrame.module.qualifyValuesForArgumentBoundary(args)
		}
		callee, err := machine.vm.newExecutionFrame(target, functionID, args, nil, task.id, payload.ResultCount, qualify)
		if err != nil {
			return taskYield{}, true, err
		}
		task.frames = append(task.frames, callee)
		return taskYield{}, true, nil
	case preparedSpawn:
		payload := inst.call
		values, err := callFrame.popN(payload.ArgCount + 1)
		if err != nil {
			return taskYield{}, true, err
		}
		target, ref, err := machine.vm.resolveFunctionValue(callFrame.module, values[0])
		if err != nil {
			return taskYield{}, true, err
		}
		args := values[1:]
		if target.modulePath() != callFrame.module.modulePath() {
			args = callFrame.module.qualifyValuesForArgumentBoundary(args)
		}
		childID := machine.vm.nextSpawnExecutionContextID()
		childFrame, err := machine.vm.newExecutionFrame(target, ref.FunctionID, args, ref.upvalues, childID, payload.ResultCount, false)
		if err != nil {
			return taskYield{}, true, err
		}
		if err := machine.vm.chargeAllocation(); err != nil {
			return taskYield{}, true, err
		}
		child := &executionTask{id: childID, scope: task.scope, execution: task.execution, budget: task.budget, frames: []*executionFrame{childFrame}, debugParents: machine.debugParentSnapshot(task)}
		return taskYield{kind: taskYieldSpawn, child: child}, true, nil
	case preparedWaitableSend:
		waitable, value, err := callFrame.pop2()
		if err != nil {
			return taskYield{}, true, err
		}
		resource, blocked, err := waitableSendTaskValue(callFrame.module, waitable, value, task.id)
		if err != nil {
			return taskYield{}, true, err
		}
		if blocked {
			machine.blockTask(task, current, pc, inst, &blockedOperation{kind: "send", resource: resource})
			return taskYield{kind: taskYieldBlocked}, true, nil
		}
		return taskYield{}, true, nil
	case preparedWaitableRecv, preparedWaitableRecvOK:
		waitable, err := callFrame.pop()
		if err != nil {
			return taskYield{}, true, err
		}
		value, ok, closed, err := waitableTryRecvValue(callFrame.module, waitable)
		if err != nil {
			return taskYield{}, true, err
		}
		if !ok && !closed {
			resource, err := waitableValueData(callFrame.module, waitable)
			if err != nil {
				return taskYield{}, true, err
			}
			token := machine.vm.newWaitTokenValue()
			if err := waitableWaitRecvValue(callFrame.module, waitable, token); err != nil {
				return taskYield{}, true, err
			}
			machine.blockTask(task, current, pc, inst, &blockedOperation{
				kind: "recv", resource: resource, waitable: waitable, recvToken: token, withOK: inst.op == preparedWaitableRecvOK,
			})
			return taskYield{kind: taskYieldBlocked}, true, nil
		}
		callFrame.push(value)
		if inst.op == preparedWaitableRecvOK {
			callFrame.push(newVMValue("Bool", ok))
		}
		return taskYield{}, true, nil
	case preparedWaitSetPark:
		waitSet, err := callFrame.pop()
		if err != nil {
			return taskYield{}, true, err
		}
		index, err := parkWaitSet(machine.vm, waitSet)
		if err == nil {
			callFrame.push(index)
			return taskYield{}, true, nil
		}
		var blocked WaitBlockedError
		if !errors.As(err, &blocked) {
			return taskYield{}, true, err
		}
		machine.blockTask(task, current, pc, inst, &blockedOperation{kind: "waitset", waitSet: waitSet})
		return taskYield{kind: taskYieldBlocked}, true, nil
	case preparedCallFFI:
		payload := inst.callFFI
		args, err := callFrame.popN(payload.ArgCount)
		if err != nil {
			return taskYield{}, true, err
		}
		route, ok := args[0].Data.(string)
		if !ok {
			return taskYield{}, true, fmt.Errorf("FFI route must be a string, got %s", args[0].Type)
		}
		bytes, err := ffiPayloadBytes(callFrame.module, args[1])
		if err != nil {
			return taskYield{}, true, err
		}
		if machine.vm.ffiSession == nil {
			callFrame.push(newByteSliceValue("Slice<Uint8>", ""))
			callFrame.push(newVMValue("String", "FFI route unavailable"))
			callFrame.push(newVMValue("Int", int64(1)))
			return taskYield{}, true, nil
		}
		pending, err := machine.vm.startFFICall(route, bytes)
		if err != nil {
			return taskYield{}, true, err
		}
		machine.blockTask(task, current, pc, inst, &blockedOperation{kind: "ffi", ffi: pending})
		return taskYield{kind: taskYieldBlocked}, true, nil
	case preparedDeferPush:
		value, err := callFrame.pop()
		if err != nil {
			return taskYield{}, true, err
		}
		ownerDepth := 0
		if inst.deferValue != nil {
			ownerDepth = inst.deferValue.OwnerDepth
		}
		if ownerDepth < 0 || ownerDepth >= len(task.frames) {
			return taskYield{}, true, fmt.Errorf("defer owner depth %d exceeds frame stack", ownerDepth)
		}
		_, ref, err := machine.vm.resolveFunctionValue(callFrame.module, value)
		if err != nil {
			return taskYield{}, true, err
		}
		owner := task.frames[len(task.frames)-1-ownerDepth]
		owner.frame.defers = append(owner.frame.defers, deferredCall{caller: callFrame.module, ref: ref})
		return taskYield{}, true, nil
	case preparedRecover:
		callFrame.push(machine.recoverValue(task))
		return taskYield{}, true, nil
	case preparedPanic:
		value, err := callFrame.pop()
		if err != nil {
			return taskYield{}, true, err
		}
		machine.startPanic(task, current, pc, value)
		return taskYield{}, true, nil
	case preparedReturn:
		payload := inst.returnValue
		values, err := callFrame.popN(payload.ResultCount)
		if err != nil {
			return taskYield{}, true, err
		}
		values, err = callFrame.normalizeReturnValues(values)
		if err != nil {
			return taskYield{}, true, err
		}
		values = callFrame.retainReturnValues(values)
		current.completion = &frameCompletion{returnValues: values}
		return taskYield{}, true, nil
	default:
		return taskYield{}, false, nil
	}
}

func (machine *executionMachine) enterDirectTailCall(task *executionTask, current *executionFrame, target *moduleInstance, function loadedFunction, payload *ir.CallPayload) error {
	callFrame := current.frame
	if payload == nil || payload.ArgCount < 0 || len(callFrame.stack) < payload.ArgCount {
		argCount := 0
		if payload != nil {
			argCount = payload.ArgCount
		}
		return fmt.Errorf("tail call stack underflow: need %d values, have %d", argCount, len(callFrame.stack))
	}
	start := len(callFrame.stack) - payload.ArgCount
	args := append([]vmValue(nil), callFrame.stack[start:]...)
	qualify := target.modulePath() != callFrame.module.modulePath()
	if qualify {
		args = callFrame.module.qualifyValuesForArgumentBoundary(args)
	}
	debugging := machine.vm.breakpointsActive.Load() || machine.vm.debugStep.Active || machine.vm.hostPauseRequested.Load()
	if !debugging && len(callFrame.defers) == 0 && !qualify {
		callee, err := machine.vm.newPreparedExecutionFrame(target, function, args, nil, task.id, current.expectedResults, current.qualifyResults)
		if err != nil {
			return err
		}
		callee.resume = current.resume
		callee.abort = current.abort
		callee.deferred = current.deferred
		callee.recoveredPanic = current.recoveredPanic
		current.resume = nil
		current.abort = nil
		task.frames[len(task.frames)-1] = callee
		callFrame.recycle()
		return nil
	}
	callee, err := machine.vm.newPreparedExecutionFrame(target, function, args, nil, task.id, payload.ResultCount, qualify)
	if err != nil {
		return err
	}
	callee.resume = func(_ *executionTask, caller *executionFrame, values []vmValue) error {
		normalized, err := caller.frame.normalizeReturnValues(values)
		if err != nil {
			return err
		}
		// The callee is recycled immediately after resume returns. Preserve
		// values that still occupy the callee stack backing array.
		caller.completion = &frameCompletion{returnValues: append([]vmValue(nil), normalized...)}
		return nil
	}
	clear(callFrame.stack[start:])
	callFrame.stack = callFrame.stack[:start]
	task.frames = append(task.frames, callee)
	return nil
}
