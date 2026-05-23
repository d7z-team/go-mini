package runtime

import "fmt"

func (e *Executor) buildImportedModuleValue(path string, modExec *Executor, modSession *StackContext) *Var {
	exports := make(map[string]*Var)
	for name := range modExec.globals {
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			if modSession != nil {
				if modSession.Shared != nil {
					if v, ok := modSession.Shared.LoadGlobal(name); ok {
						exports[name] = v
						continue
					}
				}
				if v, err := modSession.Load(name); err == nil {
					exports[name] = v
				}
			}
		}
	}
	for name, fn := range modExec.functions {
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			exports[name] = &Var{
				VType: TypeClosure,
				Ref: &VMClosure{
					FunctionSig:  CloneRuntimeFuncSig(fn.FunctionSig),
					BodyTasks:    cloneTasks(fn.BodyTasks),
					UpvalueSlots: nil,
					UpvalueNames: nil,
					Context:      &LexicalContext{Executor: modSession.Executor, Shared: modSession.Shared, Stack: modSession.Stack},
				},
			}
		}
	}
	for name, val := range modExec.consts {
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			exports[name] = NewString(val)
		}
	}
	for name, s := range modExec.metadata.structsByName {
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			exports[name] = &Var{
				VType: TypeAny,
				Ref:   CloneRuntimeStructSpec(s),
			}
		}
	}

	return &Var{
		VType: TypeModule,
		Ref: &VMModule{
			Name:    path,
			Data:    exports,
			Context: &LexicalContext{Executor: modSession.Executor, Shared: modSession.Shared, Stack: modSession.Stack},
		},
	}
}

func (e *Executor) startImportedProgram(parent *StackContext, path string, prepared *PreparedProgram) error {
	if e.scheduler == nil || e.scheduler.Current() == nil {
		return fmt.Errorf("module %s requires an active VM scheduler", path)
	}
	modExecutor, err := NewExecutorFromPrepared(prepared)
	if err != nil {
		return err
	}
	modExecutor.ModulePlanLoader = e.ModulePlanLoader
	modExecutor.StepLimit = e.StepLimit
	modExecutor.routes = e.routes
	modExecutor.packageValues = e.packageValues
	modExecutor.ffiPackages = e.ffiPackages
	for name, value := range e.consts {
		if _, exists := modExecutor.consts[name]; !exists {
			modExecutor.consts[name] = value
		}
	}
	for name, spec := range e.metadata.structsByName {
		if _, exists := modExecutor.metadata.structsByName[name]; !exists {
			modExecutor.metadata.registerStructSchema(name, CloneRuntimeStructSpec(spec))
		}
	}
	for name, spec := range e.metadata.interfacesByName {
		if _, exists := modExecutor.metadata.interfacesByName[name]; !exists {
			modExecutor.metadata.registerInterfaceSpec(name, CloneRuntimeInterfaceSpec(spec))
		}
	}
	if err := modExecutor.ValidateExternalRequirements(); err != nil {
		return err
	}
	modExecutor.scheduler = e.scheduler

	modSession := modExecutor.NewSession(parent.Context, "global")
	modSession.StepLimit = parent.StepLimit
	modSession.StepCount = parent.StepCount
	modSession.Debugger = parent.Debugger
	modSession.Shared = modExecutor.shared
	modSession.ImportChain = make(map[string]bool, len(parent.ImportChain)+1)
	for k, v := range parent.ImportChain {
		modSession.ImportChain[k] = v
	}
	modSession.ImportChain[path] = true

	if err := modExecutor.prepareSession(modSession, nil, false); err != nil {
		parent.StepCount = modSession.StepCount
		return err
	}
	frame := &ExecutionContextFrame{
		Executor: modExecutor,
		Session:  modSession,
		Kind:     FrameModuleInit,
	}
	frame.OnDone = func(done *ExecutionContextFrame) error {
		parent.StepCount = done.Session.StepCount
		res := e.buildImportedModuleValue(path, modExecutor, done.Session)
		waiters := parent.Shared.finishModuleLoad(path, res)
		parent.ValueStack.Push(res)
		e.scheduleModuleWaiters(waiters, res, nil)
		return nil
	}
	frame.OnError = func(done *ExecutionContextFrame, loadErr error) error {
		parent.StepCount = done.Session.StepCount
		waiters := parent.Shared.finishModuleLoad(path, nil)
		e.scheduleModuleWaiters(waiters, nil, loadErr)
		return loadErr
	}
	if err := e.scheduler.PushFrame(frame); err != nil {
		return err
	}
	if err := e.scheduler.YieldCurrent(); err != nil {
		return err
	}
	return errExecutionContextYield
}

func (e *Executor) scheduleModuleWaiters(waiters []moduleWaiter, value *Var, err error) {
	if len(waiters) == 0 || e.scheduler == nil {
		return
	}
	for _, waiter := range waiters {
		if waiter.ExecutionContext == nil || waiter.Frame == nil {
			continue
		}
		data, _ := waiter.Resume.Data.(*ResumeModuleData)
		if data == nil {
			data = &ResumeModuleData{}
			waiter.Resume.Data = data
		}
		data.Value = value
		data.Err = err
		waiter.Frame.Session.TaskStack = append(waiter.Frame.Session.TaskStack, waiter.Resume)
		e.scheduler.EnqueueExecutionContext(waiter.ExecutionContext)
	}
}

// ImportModulePath imports a prepared module or FFI module without exposing AST nodes.
func (e *Executor) ImportModulePath(ctx *StackContext, path string) (*Var, error) {
	oldTasks := ctx.TaskStack
	oldValues := ctx.ValueStack
	oldLHS := ctx.LHSStack
	oldUnwind := ctx.UnwindMode

	ctx.TaskStack = []Task{{Op: OpImportInit, Data: &ImportInitData{Path: path}}}
	ctx.ValueStack = &ValueStack{}
	ctx.LHSStack = &LHSStack{}
	setUnwindMode(ctx, UnwindNone)

	var err error
	if e.scheduler == nil {
		e.scheduler = NewExecutionContextScheduler()
	}
	if e.scheduler.Current() == nil {
		e.runMu.Lock()
		root, resetErr := e.scheduler.Reset(ctx, e)
		if resetErr != nil {
			err = resetErr
		} else {
			err = e.runExecutionContexts(ctx.Context, root)
			e.scheduler.Stop()
		}
		e.runMu.Unlock()
	} else {
		err = e.Run(ctx)
	}
	var res *Var
	if err == nil {
		res = ctx.ValueStack.Pop()
	}

	ctx.TaskStack = oldTasks
	ctx.ValueStack = oldValues
	ctx.LHSStack = oldLHS
	ctx.UnwindMode = oldUnwind
	return res, err
}
