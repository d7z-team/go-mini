package runtime

import "fmt"

func (e *Executor) buildImportedModuleValue(path string, modExec *Executor, modSession *StackContext) *Var {
	exports := make(map[string]*Var)
	if modExec != nil {
		for name, export := range modExec.exports {
			target := export.TargetName
			if target == "" {
				target = export.Name
			}
			if target == "" {
				target = name
			}
			switch export.Kind {
			case PreparedExportGlobal:
				if modSession != nil && modSession.Shared != nil {
					exports[name] = &Var{
						VType:    TypeAny,
						TypeInfo: export.Type,
						Ref: &vmModuleGlobalRef{
							Shared: modSession.Shared,
							Name:   target,
						},
					}
				}
			case PreparedExportFunc:
				fn := modExec.functions[target]
				if fn == nil || modSession == nil {
					continue
				}
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
			case PreparedExportConst:
				if val, ok := modExec.consts[target]; ok {
					exports[name] = modExec.evalLiteralToVarWithType(val, modExec.constTypes[target])
				}
			case PreparedExportType:
				if typ, ok := modExec.metadata.namedTypesByName[target]; ok {
					exports[name] = &Var{VType: TypeString, TypeInfo: MustParseRuntimeType(SpecString), Str: typ.Raw.String()}
				}
			case PreparedExportStruct:
				if spec := modExec.metadata.structsByName[target]; spec != nil {
					exports[name] = &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType(SpecAny), Ref: CloneRuntimeStructSpec(spec)}
				}
			case PreparedExportInterface:
				if spec := modExec.metadata.interfacesByName[target]; spec != nil {
					exports[name] = &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType(SpecAny), Ref: CloneRuntimeInterfaceSpec(spec)}
				}
			}
		}
	}

	var moduleContext *LexicalContext
	if modSession != nil {
		moduleContext = &LexicalContext{Executor: modSession.Executor, Shared: modSession.Shared, Stack: modSession.Stack}
	}
	return &Var{
		VType:    TypeModule,
		TypeInfo: MustParseRuntimeType(SpecModule),
		Ref: &VMModule{
			Name:    path,
			Data:    exports,
			Context: moduleContext,
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
	modExecutor.ffiChannels = e.channelRegistry()
	modExecutor.moduleHashes = cloneStringMap(e.moduleHashes)
	for name, value := range e.consts {
		if _, exists := modExecutor.consts[name]; !exists {
			modExecutor.consts[name] = value
		}
	}
	if modExecutor.constTypes == nil {
		modExecutor.constTypes = make(map[string]RuntimeType)
	}
	for name, typ := range e.constTypes {
		if _, exists := modExecutor.constTypes[name]; !exists {
			modExecutor.constTypes[name] = typ
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
	return e.runTemporaryTasks(ctx, []Task{{Op: OpImportInit, Data: &ImportInitData{Path: path}}})
}
