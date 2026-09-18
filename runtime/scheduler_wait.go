package runtime

import (
	"errors"
	"fmt"
	"sort"
)

const maxBlockedContexts = 64

func (machine *executionMachine) blockTask(task *executionTask, current *executionFrame, pc int, inst *preparedInstruction, operation *blockedOperation) {
	blocked := WaitBlockedError{Message: "execution blocked"}
	switch operation.kind {
	case "send":
		blocked.Message = "waitable send blocked"
	case "recv":
		blocked.Message = "waitable receive blocked"
	case "waitset":
		blocked.Message = "waitset has no ready token"
	case "ffi":
		blocked.Message = "FFI call is pending"
	}
	operation.error = machine.vm.runtimeInstructionError(current.frame, current.frame.function.Decl.ID, pc, inst, blocked)
	task.blocked = operation
}

func (machine *executionMachine) allBlockedError() Error {
	total, contexts := machine.blockedContextSnapshot()
	blocked := AllBlockedError{Total: total, Contexts: contexts}
	out := Error{Err: blocked}
	if len(contexts) != 0 {
		first := contexts[0]
		out.Generation, out.ProgramHash = first.Revision.Generation, first.Revision.Hash
		out.ModulePath, out.FunctionID = first.ModulePath, first.FunctionID
		out.PC, out.Op, out.ExecutionContextID, out.Loc = first.PC, first.Op, first.ExecutionContextID, first.Loc
	}
	return out
}

func (machine *executionMachine) blockedContextSnapshot() (int, []BlockedContext) {
	contexts := make([]BlockedContext, 0, min(len(machine.blocked), maxBlockedContexts))
	total := 0
	for _, task := range machine.blocked {
		if task == nil || task.blocked == nil {
			continue
		}
		total++
		runtimeErr := task.blocked.error
		reason := "execution blocked"
		var blocked WaitBlockedError
		if errors.As(runtimeErr.Err, &blocked) && blocked.Message != "" {
			reason = blocked.Message
		}
		context := BlockedContext{
			ExecutionContextID: runtimeErr.ExecutionContextID,
			Revision:           RevisionInfo{Generation: runtimeErr.Generation, Hash: runtimeErr.ProgramHash},
			ModulePath:         runtimeErr.ModulePath, FunctionID: runtimeErr.FunctionID,
			PC: runtimeErr.PC, Op: runtimeErr.Op, Loc: runtimeErr.Loc, Reason: reason,
		}
		if task.scope != nil {
			context.ScopeID = task.scope.id
		}
		index := sort.Search(len(contexts), func(i int) bool {
			if contexts[i].ExecutionContextID != context.ExecutionContextID {
				return contexts[i].ExecutionContextID > context.ExecutionContextID
			}
			return contexts[i].ScopeID > context.ScopeID
		})
		if index == maxBlockedContexts {
			continue
		}
		if len(contexts) < maxBlockedContexts {
			contexts = append(contexts, BlockedContext{})
		}
		copy(contexts[index+1:], contexts[index:len(contexts)-1])
		contexts[index] = context
	}
	return total, contexts
}

func (machine *executionMachine) wakeBlocked() error {
	if err := machine.vm.drainReadyTimers(); err != nil {
		return err
	}
	for {
		progress := false
		for i := 0; i < len(machine.blocked); {
			task := machine.blocked[i]
			op := task.blocked
			ready, err := machine.resumeBlocked(task, op)
			if err != nil {
				return err
			}
			if !ready {
				i++
				continue
			}
			task.blocked = nil
			copy(machine.blocked[i:], machine.blocked[i+1:])
			machine.blocked[len(machine.blocked)-1] = nil
			machine.blocked = machine.blocked[:len(machine.blocked)-1]
			machine.pushRunnable(task)
			progress = true
		}
		if !progress {
			return nil
		}
	}
}

func (machine *executionMachine) resumeBlocked(task *executionTask, op *blockedOperation) (bool, error) {
	if op == nil || len(task.frames) == 0 {
		return false, errors.New("blocked task lost continuation")
	}
	callFrame := task.frames[len(task.frames)-1].frame
	switch op.kind {
	case "send":
		if op.resource == nil {
			return false, nil
		}
		if op.resource.takeCompletedSend(task.id) {
			if op.sendDone != nil {
				for _, value := range op.sendDone() {
					callFrame.push(value)
				}
			}
			return true, nil
		}
		if op.resource.Closed {
			current := task.frames[len(task.frames)-1]
			machine.startPanic(task, current, current.frame.pc-1, newVMValue("String", "send on closed waitable"))
			return true, nil
		}
		return false, nil
	case "recv":
		value, ok, closed, err := waitableTryRecvValue(callFrame.module, op.waitable)
		if err != nil {
			return false, err
		}
		if !ok && !closed {
			return false, nil
		}
		if op.recvToken.Data != nil {
			if err := cancelWaitToken(op.recvToken); err != nil {
				return false, err
			}
		}
		if op.recvDone != nil {
			values, err := op.recvDone(value, ok)
			if err != nil {
				return false, err
			}
			for _, value := range values {
				callFrame.push(value)
			}
		} else {
			callFrame.push(value)
			if op.withOK {
				callFrame.push(newVMValue("Bool", ok))
			}
		}
		return true, nil
	case "ffi":
		values, ready := op.ffi.take()
		if !ready {
			return false, nil
		}
		for _, value := range values {
			callFrame.push(value)
		}
		return true, nil
	case "waitset":
		index, err := parkWaitSet(machine.vm, op.waitSet)
		if err != nil {
			var blocked WaitBlockedError
			if errors.As(err, &blocked) {
				return false, nil
			}
			return false, err
		}
		if op.selectDone != nil {
			selected, _ := asInt64(index)
			values, err := op.selectDone(int(selected))
			if err != nil {
				current := task.frames[len(task.frames)-1]
				machine.startPanic(task, current, current.frame.pc-1, newVMValue("String", err.Error()))
				return true, nil
			}
			for _, value := range values {
				callFrame.push(value)
			}
		} else {
			callFrame.push(index)
		}
		return true, nil
	default:
		return false, fmt.Errorf("unknown blocked operation %q", op.kind)
	}
}
