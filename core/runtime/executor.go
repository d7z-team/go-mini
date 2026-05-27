package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type Executor struct {
	packageName      string
	metadata         *runtimeMetadataRegistry
	consts           map[string]string
	constTypes       map[string]RuntimeType
	globals          map[string]*RuntimeGlobal
	functions        map[string]*RuntimeFunction
	methodFunctions  map[string]map[string]string
	exports          map[string]PreparedExport
	mainTasks        []Task
	globalInitOrder  []string
	globalInitGroups []*RuntimeGlobalInit
	importAliases    map[string]string

	routes               map[string]FFIRoute
	packageValues        map[string]*BoundPackageValue
	ffiPackages          map[string]*BoundFFIPackage
	ffiChannels          ffigo.ChannelRegistry
	externalRequirements []ExternalRequirement
	moduleHashes         map[string]string

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
	Receiver    TypeSpec
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

func methodNameFromFunctionName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 && idx < len(name)-1 {
		return name[idx+1:]
	}
	return name
}

func receiverNameFromFunctionName(name string) string {
	if idx := strings.LastIndex(name, "."); idx > 0 {
		return name[:idx]
	}
	return ""
}

func methodFunctionReceiverKeys(pkg string, receiver TypeSpec) []string {
	normalized := normalizeMethodReceiverType(receiver.String())
	if normalized == "" {
		return nil
	}
	keys := []string{normalized}
	if pkg != "" && !strings.Contains(normalized, ".") {
		keys = append(keys, pkg+"."+normalized)
	}
	return keys
}

func (e *Executor) resolveVMMethodRoute(typeName, method string, staticTypes ...RuntimeType) (string, bool) {
	for _, name := range e.vmMethodCandidates(typeName, method, staticTypes...) {
		if _, ok := e.lookupFunction(name); ok {
			return name, true
		}
	}
	return "", false
}

func (e *Executor) resolveHostMethodRoute(typeName, method string, staticTypes ...RuntimeType) (string, bool) {
	for _, name := range hostMethodCandidates(typeName, method, staticTypes...) {
		if _, ok := e.routes[name]; ok {
			return name, true
		}
	}
	return "", false
}

func (e *Executor) vmMethodCandidates(typeName, method string, staticTypes ...RuntimeType) []string {
	seen := make(map[string]struct{})
	var candidates []string
	addCandidate := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		candidates = append(candidates, name)
	}
	addName := func(name string) {
		normalized := normalizeMethodReceiverType(name)
		if normalized == "" || normalized == "Any" {
			return
		}
		if methods := e.methodFunctions[normalized]; methods != nil {
			if fnName := methods[method]; fnName != "" {
				addCandidate(fnName)
			}
		}
		if e.packageName != "" && !strings.Contains(normalized, ".") {
			addCandidate(e.packageName + "." + normalized + "." + method)
		}
	}
	addName(typeName)
	for _, typ := range staticTypes {
		if typ.IsEmpty() {
			continue
		}
		addName(typ.Raw.String())
	}
	return candidates
}

func hostMethodCandidates(typeName, method string, staticTypes ...RuntimeType) []string {
	seen := make(map[string]struct{})
	var candidates []string
	addName := func(name string) {
		normalized := normalizeMethodReceiverType(name)
		if normalized == "" || normalized == "Any" {
			return
		}
		candidate := normalized + "." + method
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	addName(typeName)
	for _, typ := range staticTypes {
		if typ.IsEmpty() {
			continue
		}
		addName(typ.Raw.String())
	}
	return candidates
}

func NewExecutorFromPrepared(prepared *PreparedProgram) (*Executor, error) {
	if prepared == nil {
		return nil, errors.New("missing prepared program")
	}
	if err := ValidatePreparedProgram(prepared); err != nil {
		return nil, err
	}
	result := &Executor{
		packageName:          prepared.Package,
		globalInitOrder:      append([]string(nil), prepared.GlobalInitOrder...),
		globalInitGroups:     cloneRuntimeGlobalInitGroupsFromPrepared(prepared.GlobalInitGroups),
		importAliases:        make(map[string]string, len(prepared.ImportAliases)),
		metadata:             newRuntimeMetadataRegistry(),
		globals:              make(map[string]*RuntimeGlobal),
		functions:            make(map[string]*RuntimeFunction),
		methodFunctions:      make(map[string]map[string]string),
		exports:              clonePreparedExportMap(prepared.Exports),
		consts:               make(map[string]string),
		constTypes:           make(map[string]RuntimeType),
		routes:               make(map[string]FFIRoute),
		packageValues:        make(map[string]*BoundPackageValue),
		ffiPackages:          make(map[string]*BoundFFIPackage),
		ffiChannels:          ffigo.NewChannelRegistry(),
		externalRequirements: append([]ExternalRequirement(nil), prepared.ExternalRequirements...),
		moduleHashes:         make(map[string]string),
		interfaceCache:       make(map[TypeSpec]*RuntimeInterfaceSpec),
		shared:               NewSharedState(),
		scheduler:            NewExecutionContextScheduler(),
	}
	result.applyPreparedProgram(prepared)
	return result, nil
}

func (e *Executor) channelRegistry() ffigo.ChannelRegistry {
	if e == nil {
		return nil
	}
	if e.ffiChannels == nil {
		e.ffiChannels = ffigo.NewChannelRegistry()
	}
	return e.ffiChannels
}

func (e *Executor) applyPreparedProgram(prepared *PreparedProgram) {
	prepared = clonePreparedProgram(prepared)
	if prepared == nil {
		return
	}
	e.globalInitOrder = append([]string(nil), prepared.GlobalInitOrder...)
	e.packageName = prepared.Package
	e.globalInitGroups = cloneRuntimeGlobalInitGroupsFromPrepared(prepared.GlobalInitGroups)
	e.importAliases = cloneStringMap(prepared.ImportAliases)
	e.consts = cloneStringMap(prepared.Constants)
	e.constTypes = cloneRuntimeTypeMap(prepared.ConstantTypes)
	if e.constTypes == nil {
		e.constTypes = make(map[string]RuntimeType)
	}
	e.exports = clonePreparedExportMap(prepared.Exports)
	e.externalRequirements = append([]ExternalRequirement(nil), prepared.ExternalRequirements...)
	e.methodFunctions = make(map[string]map[string]string)
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
			rf.Receiver = fn.Receiver
			rf.FunctionSig = CloneRuntimeFuncSig(fn.FunctionSig)
			rf.BodyTasks = cloneTasks(fn.BodyTasks)
		}
		e.functions[name] = rf
		e.registerMethodFunction(prepared.Package, name, rf)
	}
	e.mainTasks = cloneTasks(prepared.MainTasks)
}

func (e *Executor) registerMethodFunction(pkg, name string, fn *RuntimeFunction) {
	if e == nil || fn == nil || fn.Receiver.IsEmpty() || fn.FunctionSig == nil {
		return
	}
	methodName := methodNameFromFunctionName(name)
	for _, receiverName := range methodFunctionReceiverKeys(pkg, fn.Receiver) {
		if e.methodFunctions == nil {
			e.methodFunctions = make(map[string]map[string]string)
		}
		methods := e.methodFunctions[receiverName]
		if methods == nil {
			methods = make(map[string]string)
			e.methodFunctions[receiverName] = methods
		}
		methods[methodName] = name
	}
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

func (e *Executor) runTemporaryTasks(ctx *StackContext, plan []Task) (*Var, error) {
	oldTasks := ctx.TaskStack
	oldValues := ctx.ValueStack
	oldLHS := ctx.LHSStack
	oldUnwind := ctx.UnwindMode

	ctx.TaskStack = cloneTasks(plan)
	ctx.ValueStack = &ValueStack{}
	ctx.LHSStack = &LHSStack{}
	setUnwindMode(ctx, UnwindNone)
	defer func() {
		ctx.TaskStack = oldTasks
		ctx.ValueStack = oldValues
		ctx.LHSStack = oldLHS
		ctx.UnwindMode = oldUnwind
	}()

	if err := e.runStackContext(ctx.Context, ctx, false); err != nil {
		return nil, err
	}
	return ctx.ValueStack.Pop(), nil
}

func (e *Executor) EvalPreparedFunction(ctx context.Context, fn *PreparedFunction, env map[string]interface{}) (*Var, error) {
	if e == nil {
		return nil, errors.New("missing executor")
	}
	if fn == nil {
		return nil, errors.New("missing prepared function")
	}
	name := fn.Name
	if name == "" {
		name = "__eval__"
	}
	if fn.FunctionSig == nil {
		return nil, fmt.Errorf("prepared function %s missing signature", name)
	}
	if !fn.Receiver.IsEmpty() {
		return nil, fmt.Errorf("eval prepared function %s does not accept method receiver", name)
	}
	if err := validateRuntimeFuncSig("prepared function "+name, fn.FunctionSig); err != nil {
		return nil, err
	}
	if err := validatePreparedFunctionReceiver(name, fn); err != nil {
		return nil, err
	}
	if err := validatePreparedTaskPlan("prepared function "+name, fn.BodyTasks, 0); err != nil {
		return nil, err
	}
	if err := e.EnsureSharedStateInitialized(ctx, nil); err != nil {
		return nil, err
	}

	session := e.NewSession(ctx, "eval")
	session.StepLimit = e.StepLimit
	for k, v := range env {
		_ = session.AddVariable(k, session.Executor.ToVar(session, v, nil))
	}

	call := &DoCallData{
		Name:        name,
		FunctionSig: CloneRuntimeFuncSig(fn.FunctionSig),
		BodyTasks:   cloneTasks(fn.BodyTasks),
	}
	if err := e.setupFuncCall(session, name, call, nil, nil); err != nil {
		e.CleanupSession(session)
		return nil, err
	}

	if err := e.runStackContext(ctx, session, true); err != nil {
		return nil, err
	}
	if fn.FunctionSig.ReturnType.IsVoid() || session.ValueStack.Len() == 0 {
		return nil, nil
	}
	return session.ValueStack.Pop(), nil
}

func (e *Executor) NewSession(ctx context.Context, scope string) *StackContext {
	controller := RunControllerFromContext(ctx)
	session := &StackContext{
		Context:     ctx,
		Executor:    e,
		Controller:  controller,
		Shared:      e.shared,
		ImportChain: make(map[string]bool),
		Stack:       &Stack{MemoryPtr: make(map[string]*Slot), Symbols: make(map[string]SymbolRef), Frame: &SlotFrame{}, Scope: scope, Depth: 1},
		Debugger:    DebuggerFromContext(ctx),
		TaskStack:   make([]Task, 0, 128),
		ValueStack:  &ValueStack{},
		LHSStack:    &LHSStack{},
		UnwindMode:  UnwindNone,
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
		e.CleanupSession(session)
		return err
	}
	if len(session.TaskStack) == 0 {
		e.CleanupSession(session)
		return nil
	}
	return e.runStackContext(ctx, session, true)
}

func (e *Executor) runStackContext(ctx context.Context, session *StackContext, cleanupSession bool) error {
	if e.scheduler != nil && e.scheduler.Current() != nil {
		err := e.Run(session)
		if cleanupSession {
			e.CleanupSession(session)
		}
		return err
	}
	run, err := e.startRun(ctx, session, cleanupSession)
	if err != nil {
		if cleanupSession {
			e.CleanupSession(session)
		}
		return err
	}
	return run.Wait()
}

func (e *Executor) startRun(ctx context.Context, session *StackContext, cleanupSession bool) (*RunHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	e.runMu.Lock()
	if e.scheduler == nil {
		e.scheduler = NewExecutionContextScheduler()
	}
	baseCtx := session.Context
	baseController := session.Controller
	session.Controller = NewRunController(nil)
	session.Context = ContextWithRunController(ctx, session.Controller)
	activeController := session.Controller
	root, err := e.scheduler.Reset(session, e)
	if err != nil {
		session.Context = baseCtx
		session.Controller = baseController
		activeController.Stop(err)
		e.runMu.Unlock()
		return nil, err
	}
	done := make(chan struct{})
	run := NewRunHandle(session.Controller, session.Debugger, done)
	go func() {
		var runErr error
		defer func() {
			if session.Debugger != nil {
				session.Debugger.ClearStep(activeController.ID())
			}
			session.Context = baseCtx
			session.Controller = baseController
			if cleanupSession {
				e.CleanupSession(session)
			}
			activeController.Stop(runErr)
			run.setResult(runErr)
			e.scheduler.Stop()
			close(done)
			e.runMu.Unlock()
		}()
		runErr = e.runExecutionContexts(session.Context, root)
	}()
	return run, nil
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
	return e.resolveMethodValueWithStaticType(val, name, RuntimeType{})
}

func (e *Executor) resolveMethodValueWithStaticType(val *Var, name string, staticType RuntimeType) (*Var, bool) {
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
		if methodName, ok := e.resolveHostMethodRoute(string(val.RawType()), name, staticType); ok {
			return e.methodClosure(val, methodName), true
		}
	case TypeMap:
		if m, ok := val.Ref.(*VMMap); ok {
			if v, ok := m.Load(name); ok {
				return v, true
			}
		}
	case TypePointer:
		tName := string(val.RawType())
		if tName == "" || tName == "Any" {
			if slot, ok := e.slotPointerSlot(val); ok && slot != nil && !slot.Decl.IsEmpty() {
				tName = PtrType(slot.Decl.Raw).String()
			}
		}
		if tName != "" && tName != "Any" {
			if methodName, ok := e.resolveVMMethodRoute(tName, name, staticType); ok {
				return e.methodClosure(val, methodName), true
			}
		}
		if target, ok := e.slotPointerTarget(val); ok {
			return e.resolveMethodValueWithStaticType(target, name, staticType)
		}
	case TypeStruct:
		if st, ok := val.Ref.(*VMStruct); ok {
			if field, ok := st.Field(name); ok && field != nil {
				return field.Value, true
			}
		}
		tName := string(val.RawType())
		if tName != "" && tName != "Any" {
			if methodName, ok := e.resolveVMMethodRoute(tName, name, staticType); ok {
				return e.methodClosure(val, methodName), true
			}
		}
	case TypeModule:
		if mod, ok := val.Ref.(*VMModule); ok {
			if v, ok := mod.Load(name); ok && v != nil {
				return v, true
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

func (e *Executor) methodClosure(receiver *Var, methodName string) *Var {
	method := &VMMethodValue{Receiver: receiver, Method: methodName}
	if fn, ok := e.lookupFunction(methodName); ok && fn != nil {
		method.FuncSig = CloneRuntimeFuncSig(fn.FunctionSig)
		method.BodyTasks = cloneTasks(fn.BodyTasks)
	}
	return &Var{VType: TypeClosure, Ref: method}
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
	if ctx == nil {
		ctx = context.Background()
	}
	controller := RunControllerFromContext(ctx)
	if controller == nil {
		if frame := root.CurrentFrame(); frame != nil && frame.Session != nil {
			controller = frame.Session.Controller
		}
	}
	for {
		if controller != nil {
			if err := controller.Checkpoint(ctx); err != nil {
				return e.abortRun(err)
			}
		}
		snapshot := e.scheduler.Snapshot()
		switch snapshot.State {
		case SchedulerStateDone:
			return nil
		case SchedulerStateIdleExternal:
			wake := e.scheduler.WakeChan()
			var control <-chan struct{}
			if controller != nil {
				control = controller.Signal()
			}
			select {
			case <-wake:
			case <-control:
			case <-ctx.Done():
				return e.abortRun(ctx.Err())
			}
			continue
		case SchedulerStateAllBlocked:
			if controller != nil && controller.HasPauseRequest() {
				if err := controller.Checkpoint(ctx); err != nil {
					return e.abortRun(err)
				}
				continue
			}
			return e.abortRun(&VMAllBlockedError{Waits: snapshot.Blocked})
		case SchedulerStateReady:
		default:
			return e.abortRun(errors.New("invalid VM scheduler state"))
		}
		execCtx := snapshot.ExecCtx
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
		if session.Controller != nil {
			if err := session.Controller.Checkpoint(session.Context); err != nil {
				return runStopDone, err
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
		if task.Op == OpLineStep {
			if session.Debugger != nil && task.Source != nil {
				var runID uint64
				if session.Controller != nil {
					runID = session.Controller.ID()
				}
				if session.Debugger.ShouldTrigger(runID, task.Source.Line) {
					session.Debugger.ClearStep(runID)
					var execCtxID uint32
					if e.scheduler != nil {
						if execCtx := e.scheduler.Current(); execCtx != nil {
							execCtxID = execCtx.ID
						}
					}
					event := &DebugEvent{
						RunID:              runID,
						ExecutionContextID: execCtxID,
						Loc: &DebugPosition{
							F: task.Source.File,
							L: task.Source.Line,
							C: task.Source.Col,
						},
						Variables: session.Stack.DumpVariables(),
					}
					if session.Controller != nil {
						if !session.Controller.RequestPause(PauseReason{Kind: "debugger", Meta: task.Source.File}) {
							if err := session.Controller.Checkpoint(session.Context); err != nil {
								return runStopDone, err
							}
							continue
						}
						if !session.Controller.EnterPaused() {
							if err := session.Controller.Checkpoint(session.Context); err != nil {
								return runStopDone, err
							}
							continue
						}
					}
					session.Debugger.Publish(event)
					if session.Controller != nil {
						if err := session.Controller.WaitPaused(session.Context); err != nil {
							return runStopDone, err
						}
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

		session.CurrentTask = &task
		err := e.dispatch(session, task)
		session.CurrentTask = nil
		if err != nil {
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
					if goErr := goErrorFromVar(e.unwrapValue(panicVal)); goErr != nil {
						panicVal = newErrorVar(wrapErrorWithStack(goErr, vme.Frames))
					} else {
						message := vme.Message
						if panicVal != nil {
							if text, err := panicVal.ToError(); err == nil && text != "" {
								message = text
							}
						}
						if message != "" {
							panicVal = newErrorVar(wrapErrorWithStack(errors.New(message), vme.Frames))
						}
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
