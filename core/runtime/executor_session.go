package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
)

func (e *Executor) Execute(ctx context.Context) (err error) {
	return e.ExecuteWithEnv(ctx, nil)
}

func (e *Executor) Start(ctx context.Context) (*RunHandle, error) {
	return e.StartWithEnv(ctx, nil)
}

func (e *Executor) StartWithEnv(ctx context.Context, env map[string]*Var) (*RunHandle, error) {
	session := e.NewSession(ctx, "global")
	session.StepLimit = e.stepLimit
	if err := e.prepareSession(session, env, true); err != nil {
		e.CleanupSession(session)
		return nil, err
	}
	run, err := e.startRun(ctx, session, true)
	if err != nil {
		e.CleanupSession(session)
		return nil, err
	}
	return run, nil
}

func (e *Executor) ExecuteWithEnv(ctx context.Context, env map[string]*Var) (err error) {
	run, err := e.StartWithEnv(ctx, env)
	if err != nil {
		return err
	}
	return run.Wait()
}

func (e *Executor) InitializeSession(session *StackContext, env map[string]*Var, invokeMain bool) (err error) {
	if err := e.prepareSession(session, env, invokeMain); err != nil {
		return err
	}
	if e.scheduler != nil && e.scheduler.Current() == nil {
		run, startErr := e.startRun(session.Context, session, false)
		if startErr != nil {
			return startErr
		}
		return run.Wait()
	}
	return e.Run(session)
}

func (e *Executor) prepareSession(session *StackContext, env map[string]*Var, invokeMain bool) (err error) {
	if session == nil {
		return errors.New("invalid session")
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Executor panic", "error", r, "stack", string(debug.Stack()))
			if err == nil {
				if errRec, ok := r.(error); ok {
					err = errRec
				} else {
					err = fmt.Errorf("panic: %v", r)
				}
			}
		}
	}()

	if invokeMain {
		if fn, ok := e.lookupFunction("main"); ok {
			session.TaskStack = append(session.TaskStack, Task{
				Op: OpCallBoundary,
				Data: &CallBoundaryData{
					Name:      "main",
					OldStack:  session.Stack,
					OldShared: session.Shared,
					HasReturn: false,
					ValueBase: session.ValueStack.Len(),
					LHSBase:   session.LHSStack.Len(),
				},
			})
			session.TaskStack = append(session.TaskStack, Task{Op: OpDoCall, Data: &DoCallData{
				Name:        fn.Name,
				FunctionSig: CloneRuntimeFuncSig(fn.FunctionSig),
				BodyTasks:   cloneTasks(fn.BodyTasks),
			}})
		}
	}

	// Main block sits on top of the stack, so it executes before main().
	session.TaskStack = append(session.TaskStack, cloneTasks(e.mainTasks)...)
	return e.scheduleSharedInitialization(session, env)
}

func (e *Executor) scheduleSharedInitialization(session *StackContext, env map[string]*Var) error {
	if e.shared == nil {
		e.shared = NewSharedState()
	}
	session.Shared = e.shared
	if !e.shared.BeginInitialization() {
		e.shared.ApplyEnv(env)
		return nil
	}
	session.OwnsSharedInit = true
	session.TaskStack = append(session.TaskStack, Task{Op: OpFinishSharedInit, Data: &FinishSharedInitData{Env: env}})
	if len(e.globalInitGroups) > 0 {
		for i := len(e.globalInitGroups) - 1; i >= 0; i-- {
			group := e.globalInitGroups[i]
			if group == nil {
				continue
			}
			session.TaskStack = append(session.TaskStack, cloneTasks(group.InitPlan)...)
		}
		return nil
	}
	for i := len(e.globalInitOrder) - 1; i >= 0; i-- {
		name := e.globalInitOrder[i]
		global, ok := e.lookupGlobal(name)
		if !ok || global == nil {
			continue
		}
		session.TaskStack = append(session.TaskStack, Task{
			Op: OpInitGlobal,
			Data: &InitGlobalData{
				Name:    name,
				Kind:    global.Kind,
				HasInit: global.HasInit,
				Plan:    cloneTasks(global.InitPlan),
			},
		})
	}
	return nil
}

func finishSessionSharedInitialization(session *StackContext, err error) {
	if session == nil || !session.OwnsSharedInit {
		return
	}
	session.OwnsSharedInit = false
	if session.Shared != nil {
		session.Shared.FinishInitialization(err)
	}
}
