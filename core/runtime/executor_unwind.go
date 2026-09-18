package runtime

import (
	"errors"
	"fmt"
)

func callBoundaryDeferOwner(session *StackContext) *Stack {
	if session == nil || session.Stack == nil {
		return nil
	}
	return session.Stack.CurrentDeferOwner()
}

func scheduleCallBoundaryDefers(session *StackContext, task Task, data *CallBoundaryData, resume *UnwindMode) bool {
	owner := callBoundaryDeferOwner(session)
	if data == nil || data.DefersDrained || owner == nil || len(owner.DeferStack) == 0 {
		return false
	}

	// Keep deferred calls running inside the callee activation. This preserves
	// both Go-style function-scoped defer lifetime and recover() semantics
	// before the call boundary restores the caller stack/frame.
	data.DefersDrained = true
	session.TaskStack = append(session.TaskStack, task)
	if resume != nil {
		session.TaskStack = append(session.TaskStack, Task{Op: OpResumeUnwind, Data: *resume})
	}
	owner.RunDefers()
	return true
}

func setUnwindMode(session *StackContext, mode UnwindMode) {
	if session == nil {
		return
	}
	session.UnwindMode = mode
	if mode == UnwindNone {
		session.skippedScopeEnters = 0
	}
}

// Unwind State Machine
func (e *Executor) handleUnwind(session *StackContext, task *Task) (bool, error) {
	// When unwinding, future scope-enter tasks are skipped. Their matching
	// exits must be skipped as well; otherwise unwinding can pop scopes that
	// were never entered in this execution path.
	if session.UnwindMode != UnwindNone {
		switch task.Op {
		case OpScopeEnter, OpForScopeEnter, OpRangeScopeEnter, OpCatchScopeEnter:
			session.skippedScopeEnters++
			return true, nil
		case OpScopeExit, OpForScopeExit:
			if session.skippedScopeEnters > 0 {
				session.skippedScopeEnters--
				return true, nil
			}
		}
	}

	if task.Op == OpScopeExit || task.Op == OpForScopeExit || task.Op == OpFinally {
		prevMode := session.UnwindMode
		setUnwindMode(session, UnwindNone)
		session.TaskStack = append(session.TaskStack, Task{Op: OpResumeUnwind, Data: prevMode})
		session.TaskStack = append(session.TaskStack, *task)
		return true, nil
	}

	if task.Op == OpRunDefers {
		if owner := callBoundaryDeferOwner(session); owner != nil && len(owner.DeferStack) > 0 {
			prevMode := session.UnwindMode
			setUnwindMode(session, UnwindNone)
			session.TaskStack = append(session.TaskStack, Task{Op: OpResumeUnwind, Data: prevMode})
			owner.RunDefers()
			return true, nil
		}
		return false, nil
	}

	if task.Op == OpCatchBoundary && session.UnwindMode == UnwindPanic {
		setUnwindMode(session, UnwindNone)
		session.TaskStack = append(session.TaskStack, Task{Op: OpScopeExit})
		if data, ok := task.Data.(*CatchData); ok {
			session.TaskStack = append(session.TaskStack, data.Body...)
			session.TaskStack = append(session.TaskStack, Task{
				Op:   OpCatchScopeEnter,
				Data: &CatchScopeData{Catch: data, Panic: session.PanicVar},
			})
		} else {
			return false, errors.New("OpCatchBoundary missing CatchData")
		}
		session.PanicVar = nil
		return true, nil // Resume normal execution within Catch
	}

	if task.Op == OpLoopContinue {
		if session.UnwindMode == UnwindContinue {
			setUnwindMode(session, UnwindNone)
			return true, nil
		}
	}

	if task.Op == OpRangeIter {
		if session.UnwindMode == UnwindContinue {
			pruneRangeContinueResidualTasks(session)
			setUnwindMode(session, UnwindNone)
			session.TaskStack = append(session.TaskStack, *task)
			return true, nil
		}
	}

	if task.Op == OpImportDone {
		// Even on panic, we must restore the parent session
		data := task.Data.(*ImportData)
		session.Executor = data.OldExecutor
		session.Stack = data.OldStack
		if session.Shared != nil {
			session.Shared.SetModuleLoading(data.Path, false)
		}
		// Return true to indicate we handled this task, but keep UnwindMode as is to continue unwinding in parent
		return true, nil
	}

	if task.Op == OpCallBoundary {
		data, ok := task.Data.(*CallBoundaryData)
		if !ok || data == nil {
			return false, fmt.Errorf("OpCallBoundary data is not *CallBoundaryData: %T", task.Data)
		}
		if session.UnwindMode == UnwindPanic || session.UnwindMode == UnwindReturn {
			prevMode := session.UnwindMode
			setUnwindMode(session, UnwindNone)
			if scheduleCallBoundaryDefers(session, *task, data, &prevMode) {
				return true, nil
			}
			setUnwindMode(session, prevMode)
		}
		oldStack := data.OldStack
		hasReturn := data.HasReturn

		if session.UnwindMode == UnwindReturn {
			setUnwindMode(session, UnwindNone)
			if hasReturn {
				res, _ := session.LoadReturn()
				session.ValueStack.Push(res)
			}
			session.Stack = oldStack
			if data.OldExec != nil {
				session.Executor = data.OldExec
			}
			if data.OldShared != nil {
				session.Shared = data.OldShared
			}
			return true, nil
		}

		// If it's a panic, still restore the stack and continue unwinding
		session.Stack = oldStack
		if data.OldExec != nil {
			session.Executor = data.OldExec
		}
		if data.OldShared != nil {
			session.Shared = data.OldShared
		}
		return false, nil
	}

	if task.Op == OpLoopBoundary {
		if session.UnwindMode == UnwindBreak {
			setUnwindMode(session, UnwindNone)
			return true, nil
		}
		if session.UnwindMode == UnwindContinue {
			if task.Data == nil {
				return false, nil
			}
			// Switch does NOT catch continue, it should propagate to the outer loop
			if _, ok := task.Data.(*SwitchData); ok {
				return false, nil
			}

			setUnwindMode(session, UnwindNone)
			if err := e.dispatch(session, *task); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, nil // Continue unwinding if it's a panic/return
	}

	return false, nil
}
