package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"gopkg.d7z.net/go-mini/core/debugger"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type Executor struct {
	metadata         *runtimeMetadataRegistry
	consts           map[string]string
	globals          map[string]*RuntimeGlobal
	functions        map[string]*RuntimeFunction
	mainTasks        []Task
	globalInitOrder  []string
	globalInitGroups []*RuntimeGlobalInit
	importAliases    map[string]string

	routes               map[string]FFIRoute
	packageValues        map[string]*BoundPackageValue
	ffiPackages          map[string]*BoundFFIPackage
	externalRequirements []ExternalRequirement

	ModulePlanLoader func(path string) (*PreparedProgram, error)

	StepLimit int64

	interfaceCache map[TypeSpec]*RuntimeInterfaceSpec
	mu             sync.RWMutex
	runMu          sync.Mutex
	shared         *SharedState
	scheduler      *ExecutionContextScheduler
}

type runStop uint8

const (
	runStopDone runStop = iota
	runStopYield
	runStopSuspend
)

const executionContextInstructionQuantum = 256

var (
	errExecutionContextYield   = errors.New("VM execution context yielded")
	errExecutionContextSuspend = errors.New("VM execution context suspended")
)

var ErrModuleNotFound = errors.New("module not found")

type RuntimeGlobal struct {
	Name     string
	Kind     RuntimeType
	HasInit  bool
	InitPlan []Task
}

type RuntimeGlobalInit struct {
	Names    []string
	InitPlan []Task
}

type RuntimeFunction struct {
	Name        string
	FunctionSig *RuntimeFuncSig
	BodyTasks   []Task
}

func (e *Executor) SharedStateSnapshot() *SharedStateSnapshot {
	if e == nil || e.shared == nil {
		return nil
	}
	return e.shared.Snapshot()
}

func normalizeMethodReceiverType(typeName string) string {
	if inner, ok := ffigo.RefElementType(typeName); ok {
		typeName = inner
	}
	typeName = strings.TrimPrefix(typeName, "*")
	return typeName
}

func methodRouteName(typeName, method string) string {
	return normalizeMethodReceiverType(typeName) + "." + method
}

func (e *Executor) resolveMethodRoute(typeName, method string) (string, bool) {
	methodName := methodRouteName(typeName, method)
	if _, ok := e.routes[methodName]; ok {
		return methodName, true
	}
	if _, ok := e.lookupFunction(methodName); ok {
		return methodName, true
	}
	return "", false
}

func NewExecutorFromPrepared(prepared *PreparedProgram) (*Executor, error) {
	if prepared == nil {
		return nil, errors.New("missing prepared program")
	}
	if err := ValidatePreparedProgram(prepared); err != nil {
		return nil, err
	}
	result := &Executor{
		globalInitOrder:      append([]string(nil), prepared.GlobalInitOrder...),
		globalInitGroups:     cloneRuntimeGlobalInitGroupsFromPrepared(prepared.GlobalInitGroups),
		importAliases:        make(map[string]string, len(prepared.ImportAliases)),
		metadata:             newRuntimeMetadataRegistry(),
		globals:              make(map[string]*RuntimeGlobal),
		functions:            make(map[string]*RuntimeFunction),
		consts:               make(map[string]string),
		routes:               make(map[string]FFIRoute),
		packageValues:        make(map[string]*BoundPackageValue),
		ffiPackages:          make(map[string]*BoundFFIPackage),
		externalRequirements: append([]ExternalRequirement(nil), prepared.ExternalRequirements...),
		interfaceCache:       make(map[TypeSpec]*RuntimeInterfaceSpec),
		shared:               NewSharedState(),
		scheduler:            NewExecutionContextScheduler(),
	}
	result.applyPreparedProgram(prepared)
	return result, nil
}

func (e *Executor) applyPreparedProgram(prepared *PreparedProgram) {
	prepared = clonePreparedProgram(prepared)
	if prepared == nil {
		return
	}
	e.globalInitOrder = append([]string(nil), prepared.GlobalInitOrder...)
	e.globalInitGroups = cloneRuntimeGlobalInitGroupsFromPrepared(prepared.GlobalInitGroups)
	e.importAliases = cloneStringMap(prepared.ImportAliases)
	e.consts = cloneStringMap(prepared.Constants)
	e.externalRequirements = append([]ExternalRequirement(nil), prepared.ExternalRequirements...)
	for name, typeInfo := range prepared.NamedTypes {
		e.metadata.registerNamedType(name, typeInfo)
	}
	for name, spec := range prepared.StructSchemas {
		e.metadata.registerStructSchema(name, CloneRuntimeStructSpec(spec))
	}
	for name, spec := range prepared.InterfaceSchemas {
		e.metadata.registerInterfaceSpec(name, CloneRuntimeInterfaceSpec(spec))
	}
	for name, global := range prepared.Globals {
		rg, ok := e.globals[name]
		if !ok || rg == nil {
			rg = &RuntimeGlobal{Name: name}
		}
		if global != nil {
			rg.Kind = global.Kind
			rg.HasInit = global.HasInit
			rg.InitPlan = cloneTasks(global.InitPlan)
		}
		e.globals[name] = rg
	}
	for name, fn := range prepared.Functions {
		rf, ok := e.functions[name]
		if !ok || rf == nil {
			rf = &RuntimeFunction{Name: name}
		}
		if fn != nil {
			rf.FunctionSig = CloneRuntimeFuncSig(fn.FunctionSig)
			rf.BodyTasks = cloneTasks(fn.BodyTasks)
		}
		e.functions[name] = rf
	}
	e.mainTasks = cloneTasks(prepared.MainTasks)
}

func (e *Executor) resolveNamedType(typ TypeSpec) (RuntimeType, bool) {
	return e.metadata.resolveNamedType(typ)
}

func (e *Executor) resolveNamedTypeChain(typ TypeSpec) (RuntimeType, bool, error) {
	current := typ
	seen := map[TypeSpec]struct{}{}
	for {
		if _, dup := seen[current]; dup {
			return RuntimeType{}, false, fmt.Errorf("cyclic named type resolution: %s", typ)
		}
		seen[current] = struct{}{}
		next, ok := e.resolveNamedType(current)
		if !ok {
			if current == typ {
				return RuntimeType{}, false, nil
			}
			resolved, err := ParseRuntimeType(current)
			if err != nil {
				return RuntimeType{}, false, err
			}
			return resolved, true, nil
		}
		if next.Raw == current {
			return next, true, nil
		}
		current = next.Raw
	}
}

func (e *Executor) resolveInterfaceSpec(typ TypeSpec) (*RuntimeInterfaceSpec, bool) {
	if typ.IsInterface() {
		return e.cachedInterfaceSpec(typ)
	}
	return e.metadata.resolveInterfaceSpec(typ)
}

func (e *Executor) cachedInterfaceSpec(typ TypeSpec) (*RuntimeInterfaceSpec, bool) {
	e.mu.RLock()
	spec, ok := e.interfaceCache[typ]
	e.mu.RUnlock()
	if ok && spec != nil {
		return spec, true
	}
	parsedSpec, err := ParseRuntimeInterfaceSpec(typ)
	if err != nil || parsedSpec == nil {
		return nil, false
	}
	e.mu.Lock()
	e.interfaceCache[typ] = parsedSpec
	e.mu.Unlock()
	return parsedSpec, true
}

func (e *Executor) resolveStructSchema(typ TypeSpec) (*RuntimeStructSpec, bool) {
	return e.metadata.resolveStructSchema(typ)
}

func (e *Executor) SetGlobalInitOrder(order []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.globalInitOrder = append([]string(nil), order...)
	e.globalInitGroups = nil
}

func cloneTasks(tasks []Task) []Task {
	if len(tasks) == 0 {
		return nil
	}
	return append([]Task(nil), tasks...)
}

func cloneRuntimeGlobalInitGroupsFromPrepared(groups []*PreparedGlobalInit) []*RuntimeGlobalInit {
	if len(groups) == 0 {
		return nil
	}
	out := make([]*RuntimeGlobalInit, 0, len(groups))
	for _, group := range groups {
		if group == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &RuntimeGlobalInit{
			Names:    append([]string(nil), group.Names...),
			InitPlan: cloneTasks(group.InitPlan),
		})
	}
	return out
}

func (e *Executor) lookupFunction(name string) (*RuntimeFunction, bool) {
	fn, ok := e.functions[name]
	return fn, ok
}

func (e *Executor) lookupGlobal(name string) (*RuntimeGlobal, bool) {
	global, ok := e.globals[name]
	return global, ok
}

func (e *Executor) runExprPlan(ctx *StackContext, plan []Task) (*Var, error) {
	oldTasks := ctx.TaskStack
	oldValues := ctx.ValueStack
	oldLHS := ctx.LHSStack

	ctx.TaskStack = cloneTasks(plan)
	ctx.ValueStack = &ValueStack{}
	ctx.LHSStack = &LHSStack{}

	var err error
	if e.scheduler != nil && e.scheduler.Current() == nil {
		e.runMu.Lock()
		root, resetErr := e.scheduler.Reset(ctx, e)
		if resetErr != nil {
			e.runMu.Unlock()
			err = resetErr
		} else {
			err = e.runExecutionContexts(ctx.Context, root)
			e.scheduler.Stop()
			e.runMu.Unlock()
		}
	} else {
		err = e.Run(ctx)
	}
	if err != nil {
		ctx.TaskStack = oldTasks
		ctx.ValueStack = oldValues
		ctx.LHSStack = oldLHS
		return nil, err
	}

	res := ctx.ValueStack.Pop()
	ctx.TaskStack = oldTasks
	ctx.ValueStack = oldValues
	ctx.LHSStack = oldLHS
	return res, nil
}

func (e *Executor) NewSession(ctx context.Context, scope string) *StackContext {
	session := &StackContext{
		Context:      ctx,
		Executor:     e,
		Shared:       e.shared,
		ImportChain:  make(map[string]bool),
		Stack:        &Stack{MemoryPtr: make(map[string]*Slot), Symbols: make(map[string]SymbolRef), Frame: &SlotFrame{}, Scope: scope, Depth: 1},
		Debugger:     debugger.GetDebugger(ctx),
		TaskStack:    make([]Task, 0, 128),
		ValueStack:   &ValueStack{},
		LHSStack:     &LHSStack{},
		UnwindMode:   UnwindNone,
		resumeSignal: make(chan struct{}, 1),
	}
	session.Stack.DeferOwner = session.Stack

	return session
}

func (e *Executor) EnsureSharedStateInitialized(ctx context.Context, env map[string]*Var) error {
	if e.scheduler == nil {
		e.scheduler = NewExecutionContextScheduler()
	}
	if e.scheduler.Current() != nil {
		return errors.New("shared state initialization cannot start inside an active VM execution context")
	}
	session := e.NewSession(ctx, "global")
	session.StepLimit = e.StepLimit
	if err := e.scheduleSharedInitialization(session, env); err != nil {
		return err
	}
	if len(session.TaskStack) == 0 {
		return nil
	}
	e.runMu.Lock()
	root, err := e.scheduler.Reset(session, e)
	if err != nil {
		e.runMu.Unlock()
		return err
	}
	err = e.runExecutionContexts(ctx, root)
	e.scheduler.Stop()
	e.runMu.Unlock()
	return err
}

func (e *Executor) CleanupSession(session *StackContext) {
	if session == nil {
		return
	}
	// Run all defers and clean up handles in all scopes
	curr := session.Stack
	for curr != nil {
		if curr.DeferStack != nil {
			curr.RunDefers()
		}
		curr = curr.Parent
	}
}

func (e *Executor) CheckSatisfaction(val *Var, interfaceType string) (*Var, error) {
	if val == nil {
		return nil, errors.New("cannot assign nil to interface")
	}
	interfaceSpec := TypeSpec(interfaceType)

	// 1. Exact match (handles named types and primitives directly)
	if val.RuntimeType().Raw.Equals(interfaceSpec) {
		return cloneVarForAssign(val), nil
	}

	// 2. Any penetration
	inner := e.unwrapValue(val)
	if inner == nil {
		inner = val
	}
	if inner.RuntimeType().Raw.Equals(interfaceSpec) {
		return cloneVarForAssign(inner), nil
	}

	actualInterfaceType := interfaceSpec
	if !interfaceSpec.IsInterface() {
		// 3. Resolve named type ONLY if it could be an interface or struct
		if actual, ok := e.resolveNamedType(interfaceSpec); ok {
			if actual.Kind == RuntimeTypeInterface {
				return e.CheckSatisfaction(val, actual.Raw.String())
			}
		}

		// 4. Resolve named interface
		if spec, ok := e.resolveInterfaceSpec(interfaceSpec); ok {
			actualInterfaceType = spec.Spec
		} else {
			// If it wasn't an exact match and isn't an interface, it fails (like Go)
			return nil, fmt.Errorf("interface conversion: interface is %s, not %s", inner.RawType(), interfaceSpec)
		}
	}

	spec, ok := e.cachedInterfaceSpec(actualInterfaceType)
	if !ok {
		return nil, fmt.Errorf("invalid interface type: %s", actualInterfaceType)
	}

	target := cloneVarForAssign(inner)
	vtable := make([]*Var, len(spec.Methods))
	for _, method := range spec.Methods {
		if method.Spec == nil {
			return nil, fmt.Errorf("type %v does not implement %s: missing method schema %s", inner.VType, interfaceSpec, method.Name)
		}
		callable, ok := e.resolveMethodValue(target, method.Name)
		if !ok || !e.isCallableCompatible(callable, method.Spec) {
			return nil, fmt.Errorf("type %v does not implement %s: missing or incompatible method %s", inner.VType, interfaceSpec, method.Name)
		}
		vtable[method.Index] = callable
	}

	v := &Var{
		VType: TypeInterface,
		Ref: &VMInterface{
			Target: target,
			Spec:   spec,
			VTable: vtable,
		},
	}
	v.SetRawType(interfaceType)
	return v, nil
}

func (e *Executor) resolveMethodValue(val *Var, name string) (*Var, bool) {
	val = e.unwrapValue(val)
	if val == nil {
		return nil, false
	}

	switch val.VType {
	case TypeError:
		if name == "Error" {
			return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: val, Method: "Error"}}, true
		}
	case TypeHostRef:
		if methodName, ok := e.resolveMethodRoute(string(val.RawType()), name); ok {
			return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: val, Method: methodName}}, true
		}
	case TypeMap:
		if m, ok := val.Ref.(*VMMap); ok {
			if v, ok := m.Load(name); ok {
				return v, true
			}
		}
	case TypeStruct:
		if st, ok := val.Ref.(*VMStruct); ok {
			if field, ok := st.Field(name); ok && field != nil {
				return field.Value, true
			}
		}
		tName := string(val.RawType())
		if tName != "" && tName != "Any" {
			if methodName, ok := e.resolveMethodRoute(tName, name); ok {
				return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: val, Method: methodName}}, true
			}
		}
	case TypeModule:
		if mod, ok := val.Ref.(*VMModule); ok {
			if mod.Context != nil && mod.Context.Shared != nil {
				if v, ok := mod.Context.Shared.LoadGlobal(name); ok && v != nil {
					return v, true
				}
			}
			if v, ok := mod.Load(name); ok && v != nil {
				return v, true
			}
			if mod.Context != nil {
				if v, err := mod.Context.Load(name); err == nil && v != nil {
					return v, true
				}
			}
			routeKey := fmt.Sprintf("%s.%s", mod.Name, name)
			if route, ok := e.routes[routeKey]; ok {
				return &Var{VType: TypeAny, Ref: route}, true
			}
		}
	case TypeInterface:
		if inter, ok := val.Ref.(*VMInterface); ok && inter.Spec != nil {
			if idx, ok := inter.Spec.MethodIndex[name]; ok && idx < len(inter.VTable) && inter.VTable[idx] != nil {
				return inter.VTable[idx], true
			}
		}
	}
	return nil, false
}

func (e *Executor) isCallableCompatible(v *Var, expectedSig *RuntimeFuncSig) bool {
	v = e.unwrapValue(v)
	if v == nil {
		return false
	}
	if v.VType == TypeClosure {
		if cl, ok := v.Ref.(*VMClosure); ok {
			if cl.FunctionSig == nil {
				return false
			}
			return e.isSignatureCompatible(cl.FunctionSig, expectedSig)
		}
	}
	if route, ok := v.Ref.(FFIRoute); ok {
		if route.FuncSig != nil {
			return e.isSignatureCompatible(route.FuncSig, expectedSig)
		}
	}
	return true // 默认放行，由运行期进一步处理
}

func (e *Executor) isSignatureCompatible(actual, expected *RuntimeFuncSig) bool {
	if actual == nil || expected == nil {
		return actual == expected
	}
	// 如果 expected 是 interface{Method} 这种没有详细签名的（默认 Return: Any），直接放行
	if expected.ReturnType.Raw == SpecAny && len(expected.ParamTypes) == 0 && !expected.Variadic {
		return true
	}

	// 参数数量校验
	if !actual.Variadic && expected.Variadic {
		return false
	}
	if !actual.Variadic && len(actual.ParamTypes) != len(expected.ParamTypes) {
		return false
	}

	// 参数类型校验
	for i := range expected.ParamTypes {
		actType := MustParseRuntimeType(SpecAny)
		if i < len(actual.ParamTypes) {
			actType = actual.ParamTypes[i]
		} else if actual.Variadic {
			actType = actual.ParamTypes[len(actual.ParamTypes)-1]
		}

		if !expected.ParamTypes[i].IsAssignableTo(actType) {
			return false
		}
	}

	// 返回值兼容性
	if actual.ReturnType.Raw == SpecVoid && expected.ReturnType.Raw == SpecAny {
		return true
	}
	return actual.ReturnType.IsAssignableTo(expected.ReturnType)
}

func (e *Executor) Run(session *StackContext) error {
	_, err := e.runSession(session, 0)
	return err
}

func (e *Executor) runExecutionContexts(ctx context.Context, root *VMExecutionContext) error {
	if e.scheduler == nil || root == nil {
		return errors.New("invalid VM execution context scheduler")
	}
	for {
		execCtx, err := e.scheduler.Next(ctx)
		if err != nil {
			return e.abortRun(err)
		}
		if execCtx == nil {
			return nil
		}
		frame := execCtx.CurrentFrame()
		if frame == nil {
			e.scheduler.FinishCurrent()
			continue
		}
		stop, err := frame.Executor.runSession(frame.Session, executionContextInstructionQuantum)
		if err != nil {
			return e.abortRun(err)
		}
		switch stop {
		case runStopDone:
			doneFrame := execCtx.PopFrame()
			if doneFrame == nil {
				e.scheduler.FinishCurrent()
				continue
			}
			if doneFrame.OnDone != nil {
				if err := doneFrame.OnDone(doneFrame); err != nil {
					if doneFrame.Cleanup {
						doneFrame.Executor.CleanupSession(doneFrame.Session)
					}
					return e.abortRun(err)
				}
			}
			if doneFrame.Cleanup {
				doneFrame.Executor.CleanupSession(doneFrame.Session)
			}
			if len(execCtx.Frames) == 0 {
				if execCtx.ID != root.ID {
					e.scheduler.FinishCurrent()
					continue
				}
				e.scheduler.Stop()
				return nil
			}
			e.scheduler.EnqueueExecutionContext(execCtx)
			e.scheduler.FinishCurrent()
		case runStopYield, runStopSuspend:
			// The scheduler method already parked the current VM execution context.
		}
	}
}

func (e *Executor) abortRun(cause error) error {
	if cause == nil {
		return nil
	}
	if e.scheduler == nil {
		return cause
	}
	execCtxs := e.scheduler.AbortAll()
	execCtxs = e.cancelModuleLoadsForExecutionContexts(execCtxs)
	result := e.unwindExecutionContextErrors(execCtxs, cause)
	if result == nil {
		return cause
	}
	return result
}

func (e *Executor) cancelModuleLoadsForExecutionContexts(execCtxs []*VMExecutionContext) []*VMExecutionContext {
	seen := make(map[*SharedState]bool)
	for i := 0; i < len(execCtxs); i++ {
		execCtx := execCtxs[i]
		if execCtx == nil {
			continue
		}
		for _, frame := range execCtx.Frames {
			if frame == nil || frame.Session == nil || frame.Session.Shared == nil {
				continue
			}
			shared := frame.Session.Shared
			if seen[shared] {
				continue
			}
			seen[shared] = true
			for _, waiter := range shared.cancelModuleLoads() {
				if waiter.ExecutionContext != nil {
					execCtxs = append(execCtxs, waiter.ExecutionContext)
				}
			}
		}
	}
	return execCtxs
}

func (e *Executor) unwindExecutionContextErrors(execCtxs []*VMExecutionContext, cause error) error {
	result := cause
	replaced := false
	seen := make(map[*VMExecutionContext]bool)
	for _, execCtx := range execCtxs {
		if execCtx == nil || seen[execCtx] {
			continue
		}
		seen[execCtx] = true
		if err := e.unwindExecutionContextError(execCtx, cause); err != nil && !replaced {
			result = err
			replaced = true
		}
	}
	return result
}

func (e *Executor) unwindExecutionContextError(execCtx *VMExecutionContext, cause error) error {
	err := cause
	for {
		frame := execCtx.PopFrame()
		if frame == nil {
			break
		}
		finishSessionSharedInitialization(frame.Session, err)
		if frame.OnError != nil {
			if next := frame.OnError(frame, err); next != nil {
				err = next
			}
		}
		if frame.Cleanup {
			frame.Executor.CleanupSession(frame.Session)
		}
	}
	return err
}

func (e *Executor) runSession(session *StackContext, budget int) (runStop, error) {
	executed := 0
	for len(session.TaskStack) > 0 {
		// Pause/Resume Logic (Fake Context)
		if session.IsPaused() {
			select {
			case <-session.Done():
				return runStopDone, session.Err()
			case <-session.resumeSignal:
				// Continue execution
			}
		}
		if session.Context != nil {
			if err := session.Context.Err(); err != nil {
				return runStopDone, err
			}
		}

		task := session.TaskStack[len(session.TaskStack)-1]
		session.TaskStack = session.TaskStack[:len(session.TaskStack)-1]

		session.StepCount++
		if session.StepLimit > 0 {
			if session.StepCount > session.StepLimit {
				return runStopDone, fmt.Errorf("instruction limit exceeded (%d)", session.StepLimit)
			}
		}
		if session.Aborted() {
			return runStopDone, session.Err()
		}

		if task.Op == OpLineStep {
			if session.Debugger != nil && task.Source != nil {
				if session.Debugger.ShouldTrigger(task.Source.Line) {
					session.Debugger.SetStepping(false)
					var execCtxID uint32
					if e.scheduler != nil {
						if execCtx := e.scheduler.Current(); execCtx != nil {
							execCtxID = execCtx.ID
						}
					}
					event := &debugger.Event{
						ExecutionContextID: execCtxID,
						Loc: &debugger.Position{
							F: task.Source.File,
							L: task.Source.Line,
							C: task.Source.Col,
						},
						Variables: session.Stack.DumpVariables(),
					}
					// Debugger pause is currently all-stop: once any VM execution context triggers a pause,
					// the single-threaded VM waits here for a global debugger command.
					cmd := session.Debugger.Pause(event)
					if cmd == debugger.CmdStepInto {
						session.Debugger.SetStepping(true)
					}
				}
			}
			continue
		}

		if session.UnwindMode != UnwindNone {
			if _, err := e.handleUnwind(session, &task); err != nil {
				return runStopDone, err
			}
			continue
		}

		if err := e.dispatch(session, task); err != nil {
			if errors.Is(err, errExecutionContextYield) {
				return runStopYield, nil
			}
			if errors.Is(err, errExecutionContextSuspend) {
				return runStopSuspend, nil
			}
			frames := session.GenerateStackTrace(&task)
			var vme *VMError
			if errors.As(err, &vme) {
				if len(vme.Frames) == 0 {
					vme.Frames = frames
				}
				if vme.IsPanic {
					panicVal := vme.Value
					if panicVal == nil && vme.Message != "" {
						panicVal = NewString(vme.Message)
					}
					session.PanicVar = panicVal
					session.PanicMessage = vme.Message
					session.PanicTrace = vme.Frames
					session.UnwindMode = UnwindPanic
				} else {
					return runStopDone, vme
				}
			} else {
				// Wrap unexpected errors into VMError
				return runStopDone, &VMError{
					Message: err.Error(),
					Frames:  frames,
					Cause:   err,
				}
			}
		}
		executed++
		if budget > 0 && executed >= budget && len(session.TaskStack) > 0 {
			if e.scheduler != nil && e.scheduler.Current() != nil {
				if err := e.scheduler.YieldCurrent(); err != nil {
					return runStopDone, err
				}
				return runStopYield, nil
			}
			executed = 0
		}
	}
	if session.UnwindMode == UnwindPanic {
		frames := session.PanicTrace
		if len(frames) == 0 {
			frames = session.GenerateStackTrace(nil)
		}
		message := session.PanicMessage
		if message == "" {
			message = "unhandled panic"
		}
		if session.PanicVar != nil {
			if s, err := session.PanicVar.ToError(); err == nil {
				message = s
			}
		}
		return runStopDone, &VMError{
			Message: message,
			Value:   session.PanicVar,
			Frames:  frames,
			IsPanic: true,
		}
	}
	return runStopDone, nil
}

func (e *Executor) ExecuteTasks(session *StackContext, tasks []Task) error {
	oldTasks := session.TaskStack
	oldValues := session.ValueStack
	oldLHS := session.LHSStack
	oldUnwind := session.UnwindMode

	session.TaskStack = []Task{}
	session.ValueStack = &ValueStack{}
	session.LHSStack = &LHSStack{}
	setUnwindMode(session, UnwindNone)

	session.TaskStack = append(session.TaskStack, cloneTasks(tasks)...)

	var err error
	if e.scheduler != nil && e.scheduler.Current() == nil {
		e.runMu.Lock()
		root, resetErr := e.scheduler.Reset(session, e)
		if resetErr != nil {
			e.runMu.Unlock()
			err = resetErr
		} else {
			err = e.runExecutionContexts(session.Context, root)
			e.scheduler.Stop()
			e.runMu.Unlock()
		}
	} else {
		err = e.Run(session)
	}

	session.TaskStack = oldTasks
	session.ValueStack = oldValues
	session.LHSStack = oldLHS
	session.UnwindMode = oldUnwind
	return err
}
