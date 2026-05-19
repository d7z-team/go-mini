package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/debugger"
)

type Executor struct {
	metadata        *runtimeMetadataRegistry
	consts          map[string]string
	globals         map[string]*RuntimeGlobal
	functions       map[string]*RuntimeFunction
	mainTasks       []Task
	globalInitOrder []string
	importAliases   map[string]string

	routes map[string]FFIRoute

	ModulePlanLoader func(path string) (*PreparedProgram, error)

	StepLimit int64

	interfaceCache map[TypeSpec]*RuntimeInterfaceSpec
	mu             sync.RWMutex
	runMu          sync.Mutex
	shared         *SharedState
	scheduler      *FiberScheduler
}

type runStop uint8

const (
	runStopDone runStop = iota
	runStopYield
	runStopSuspend
)

const fiberInstructionQuantum = 256

var (
	errFiberYield   = errors.New("fiber yielded")
	errFiberSuspend = errors.New("fiber suspended")
)

var ErrModuleNotFound = errors.New("module not found")

type RuntimeGlobal struct {
	Name     string
	Kind     RuntimeType
	HasInit  bool
	InitPlan []Task
}

type RuntimeFunction struct {
	Name        string
	FunctionSig *RuntimeFuncSig
	BodyTasks   []Task
}

type runtimeMetadataRegistry struct {
	namedTypesByName   map[string]RuntimeType
	namedTypesByTypeID map[string]RuntimeType
	structsByName      map[string]*RuntimeStructSpec
	structsByTypeID    map[string]*RuntimeStructSpec
	interfacesByName   map[string]*RuntimeInterfaceSpec
	interfacesByTypeID map[string]*RuntimeInterfaceSpec
}

func newRuntimeMetadataRegistry() *runtimeMetadataRegistry {
	return &runtimeMetadataRegistry{
		namedTypesByName:   make(map[string]RuntimeType),
		namedTypesByTypeID: make(map[string]RuntimeType),
		structsByName:      make(map[string]*RuntimeStructSpec),
		structsByTypeID:    make(map[string]*RuntimeStructSpec),
		interfacesByName:   make(map[string]*RuntimeInterfaceSpec),
		interfacesByTypeID: make(map[string]*RuntimeInterfaceSpec),
	}
}

func (r *runtimeMetadataRegistry) registerNamedType(name string, typeInfo RuntimeType) {
	r.namedTypesByName[name] = typeInfo
	switch typeInfo.Kind {
	case RuntimeTypeNamed, RuntimeTypeStruct, RuntimeTypeInterface:
	default:
		return
	}
	if typeInfo.TypeID != "" {
		r.namedTypesByTypeID[typeInfo.TypeID] = typeInfo
	}
}

func (r *runtimeMetadataRegistry) registerInterfaceSpec(name string, spec *RuntimeInterfaceSpec) {
	if spec == nil {
		if existing, ok := r.interfacesByName[name]; ok && existing != nil && existing.TypeID != "" {
			delete(r.interfacesByTypeID, existing.TypeID)
		}
		delete(r.interfacesByName, name)
		return
	}
	r.interfacesByName[name] = spec
	if spec.TypeID != "" {
		r.interfacesByTypeID[spec.TypeID] = spec
	}
}

func (r *runtimeMetadataRegistry) registerStructSchema(name string, spec *RuntimeStructSpec) {
	if spec == nil {
		if existing, ok := r.structsByName[name]; ok && existing != nil && existing.TypeID != "" {
			delete(r.structsByTypeID, existing.TypeID)
		}
		delete(r.structsByName, name)
		return
	}
	r.structsByName[name] = spec
	if spec.TypeID != "" {
		r.structsByTypeID[spec.TypeID] = spec
	}
}

func (r *runtimeMetadataRegistry) resolveNamedType(typ TypeSpec) (RuntimeType, bool) {
	if typeInfo, ok := r.namedTypesByName[string(typ)]; ok {
		return typeInfo, true
	}
	typeInfo, ok := r.namedTypesByTypeID[CanonicalTypeID(string(typ))]
	return typeInfo, ok
}

func (r *runtimeMetadataRegistry) resolveInterfaceSpec(typ TypeSpec) (*RuntimeInterfaceSpec, bool) {
	if spec, ok := r.interfacesByName[string(typ)]; ok {
		return spec, true
	}
	spec, ok := r.interfacesByTypeID[CanonicalTypeID(string(typ))]
	return spec, ok
}

func (r *runtimeMetadataRegistry) resolveStructSchema(typ TypeSpec) (*RuntimeStructSpec, bool) {
	if schema, ok := r.structsByName[string(typ)]; ok {
		return schema, true
	}
	schema, ok := r.structsByTypeID[CanonicalTypeID(string(typ))]
	return schema, ok
}

func (e *Executor) SharedStateSnapshot() *SharedStateSnapshot {
	if e == nil || e.shared == nil {
		return nil
	}
	return e.shared.Snapshot()
}

func normalizeMethodReceiverType(typeName string) string {
	typeName = strings.TrimPrefix(typeName, "Ptr<")
	typeName = strings.TrimPrefix(typeName, "*")
	typeName = strings.TrimSuffix(typeName, ">")
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
	result := &Executor{
		globalInitOrder: append([]string(nil), prepared.GlobalInitOrder...),
		importAliases:   make(map[string]string, len(prepared.ImportAliases)),
		metadata:        newRuntimeMetadataRegistry(),
		globals:         make(map[string]*RuntimeGlobal),
		functions:       make(map[string]*RuntimeFunction),
		consts:          make(map[string]string),
		routes:          make(map[string]FFIRoute),
		interfaceCache:  make(map[TypeSpec]*RuntimeInterfaceSpec),
		shared:          NewSharedState(),
		scheduler:       NewFiberScheduler(),
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
	e.importAliases = cloneStringMap(prepared.ImportAliases)
	e.consts = cloneStringMap(prepared.Constants)
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

func runtimeStructSpecFromStmt(stmt *ast.StructStmt) *RuntimeStructSpec {
	if stmt == nil {
		return nil
	}
	fields := make([]RuntimeStructField, 0, len(stmt.FieldNames))
	byName := make(map[string]RuntimeStructField, len(stmt.Fields))
	for _, fieldName := range stmt.FieldNames {
		fieldType := stmt.Fields[fieldName]
		typeInfo, err := ParseRuntimeType(fieldType)
		if err != nil {
			return nil
		}
		field := RuntimeStructField{
			Name:     string(fieldName),
			Type:     TypeSpec(fieldType),
			TypeInfo: typeInfo,
		}
		fields = append(fields, field)
		byName[field.Name] = field
	}
	typeInfo := RuntimeType{
		Kind:   RuntimeTypeStruct,
		Raw:    TypeSpec(stmt.Name),
		TypeID: CanonicalTypeID(string(stmt.Name)),
		Fields: fields,
	}
	return &RuntimeStructSpec{
		Name:     string(stmt.Name),
		TypeID:   CanonicalTypeID(string(stmt.Name)),
		Spec:     TypeSpec(stmt.Name),
		TypeInfo: typeInfo,
		Layout:   buildStructLayout(fields),
		Fields:   fields,
		ByName:   byName,
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
}

func cloneTasks(tasks []Task) []Task {
	if len(tasks) == 0 {
		return nil
	}
	return append([]Task(nil), tasks...)
}

func (e *Executor) buildStmtPlanWithScope(stmts []ast.Stmt, scope *loweringScope) []Task {
	if len(stmts) == 0 {
		return nil
	}
	plan := make([]Task, 0)
	for i := len(stmts) - 1; i >= 0; i-- {
		plan = append(plan, e.tasksForStmtInScope(stmts[i], nil, scope)...)
	}
	return plan
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
			err = e.runFibers(ctx.Context, root)
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

func (e *Executor) RegisterRoute(name string, route FFIRoute) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if existing, ok := e.routes[name]; ok {
		ensureCompatibleRuntimeRoute(name, existing, route)
	}
	e.routes[name] = route
}

func (e *Executor) RegisterStructSchema(name string, spec *RuntimeStructSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if spec == nil {
		delete(e.metadata.structsByName, name)
		return
	}
	if existing, ok := e.metadata.structsByName[name]; ok {
		merged, ok := mergeRuntimeStructSchema(existing, spec)
		if !ok {
			panic(fmt.Sprintf("ffi struct schema conflict for %s: existing=%s new=%s", name, existing.Spec, spec.Spec))
		}
		spec = merged
	}
	e.metadata.registerStructSchema(name, spec)
}

func (e *Executor) RegisterInterfaceSchema(name string, spec *RuntimeInterfaceSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if spec == nil {
		e.metadata.registerInterfaceSpec(name, nil)
		return
	}
	if existing, ok := e.metadata.interfacesByName[name]; ok && existing != nil && existing.Spec != spec.Spec {
		panic(fmt.Sprintf("ffi interface schema conflict for %s: existing=%s new=%s", name, existing.Spec, spec.Spec))
	}
	e.metadata.registerInterfaceSpec(name, CloneRuntimeInterfaceSpec(spec))
}

func ensureCompatibleRuntimeRoute(name string, existing, next FFIRoute) {
	if existing.Name != next.Name ||
		existing.MethodID != next.MethodID ||
		existing.Doc != next.Doc ||
		!sameRuntimeFuncSchema(existing.FuncSig, next.FuncSig) ||
		!sameRuntimeBridge(existing.Bridge, next.Bridge) {
		panic(fmt.Sprintf(
			"ffi route conflict for %s: existing(method=%d sig=%s bridge=%s) new(method=%d sig=%s bridge=%s)",
			name,
			existing.MethodID,
			runtimeRouteSignature(existing),
			runtimeBridgeIdentity(existing.Bridge),
			next.MethodID,
			runtimeRouteSignature(next),
			runtimeBridgeIdentity(next.Bridge),
		))
	}
}

func runtimeRouteSignature(route FFIRoute) string {
	if route.FuncSig != nil {
		return string(route.FuncSig.Spec)
	}
	return ""
}

func sameRuntimeFuncSchema(a, b *RuntimeFuncSig) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	default:
		if a.Spec != b.Spec || len(a.ParamModes) != len(b.ParamModes) {
			return false
		}
		for i := range a.ParamModes {
			if a.ParamModes[i] != b.ParamModes[i] {
				return false
			}
		}
		return true
	}
}

func sameRuntimeStructSchema(a, b *RuntimeStructSpec) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	default:
		return a.TypeID == b.TypeID && a.Spec == b.Spec && a.Name == b.Name
	}
}

func mergeRuntimeStructSchema(existing, next *RuntimeStructSpec) (*RuntimeStructSpec, bool) {
	switch {
	case existing == nil || next == nil:
		return next, existing == next
	case sameRuntimeStructSchema(existing, next):
		return existing, true
	case existing.TypeID != next.TypeID || existing.Name != next.Name:
		return nil, false
	}

	existingFields := make(map[string]RuntimeStructField, len(existing.Fields))
	for _, field := range existing.Fields {
		existingFields[field.Name] = field
	}
	nextFields := make(map[string]RuntimeStructField, len(next.Fields))
	for _, field := range next.Fields {
		nextFields[field.Name] = field
	}

	for name, field := range existingFields {
		if other, ok := nextFields[name]; ok {
			if field.TypeInfo.Raw != other.TypeInfo.Raw {
				return nil, false
			}
			continue
		}
		if field.TypeInfo.Kind != RuntimeTypeFunction {
			return nil, false
		}
	}
	for name, field := range nextFields {
		if _, ok := existingFields[name]; ok {
			continue
		}
		if field.TypeInfo.Kind != RuntimeTypeFunction {
			return nil, false
		}
	}

	if len(next.Fields) >= len(existing.Fields) {
		return next, true
	}
	return existing, true
}

func sameRuntimeBridge(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	return reflect.TypeOf(a) == reflect.TypeOf(b)
}

func runtimeBridgeIdentity(bridge any) string {
	if bridge == nil {
		return "<nil>"
	}
	v := reflect.ValueOf(bridge)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return fmt.Sprintf("%T@0x%x", bridge, v.Pointer())
	default:
		return fmt.Sprintf("%T:%v", bridge, bridge)
	}
}

func (e *Executor) RegisterConstant(name, val string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consts[name] = val
}

func (e *Executor) NewSession(ctx context.Context, scope string) *StackContext {
	session := &StackContext{
		Context:      ctx,
		Executor:     e,
		Shared:       e.shared,
		ImportChain:  make(map[string]bool),
		Stack:        &Stack{MemoryPtr: make(map[string]*Var), Frame: &SlotFrame{}, Scope: scope, Depth: 1},
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
		e.scheduler = NewFiberScheduler()
	}
	if e.scheduler.Current() != nil {
		return errors.New("shared state initialization cannot start inside an active fiber")
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
	err = e.runFibers(ctx, root)
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
		return val.Copy(), nil
	}

	// 2. Any penetration
	inner := e.unwrapValue(val)
	if inner == nil {
		inner = val
	}
	if inner.RuntimeType().Raw.Equals(interfaceSpec) {
		return inner.Copy(), nil
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

	vtable := make([]*Var, len(spec.Methods))
	for _, method := range spec.Methods {
		if method.Spec == nil {
			return nil, fmt.Errorf("type %v does not implement %s: missing method schema %s", inner.VType, interfaceSpec, method.Name)
		}
		callable, ok := e.resolveMethodValue(inner, method.Name)
		expected := method.Spec.FunctionType()
		if !ok || !e.isCallableCompatible(callable, &expected) {
			return nil, fmt.Errorf("type %v does not implement %s: missing or incompatible method %s", inner.VType, interfaceSpec, method.Name)
		}
		vtable[method.Index] = callable
	}

	v := &Var{
		VType: TypeInterface,
		Ref: &VMInterface{
			Target: inner.Copy(),
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
	case TypeHandle:
		if methodName, ok := e.resolveMethodRoute(string(val.RawType()), name); ok {
			return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: val, Method: methodName}}, true
		}
	case TypeMap:
		if m, ok := val.Ref.(*VMMap); ok {
			if v, ok := m.Load(name); ok {
				return v, true
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
			if mod.Context != nil {
				if v, err := mod.Context.Load(name); err == nil && v != nil {
					return v, true
				}
			}
			if v, ok := mod.Load(name); ok && v != nil {
				return v, true
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

func (e *Executor) isCallableCompatible(v *Var, expectedSig *ast.FunctionType) bool {
	v = e.unwrapValue(v)
	if v == nil {
		return false
	}
	if v.VType == TypeClosure {
		if cl, ok := v.Ref.(*VMClosure); ok {
			if cl.FunctionSig == nil {
				return false
			}
			actual := cl.FunctionSig.FunctionType()
			return e.isSignatureCompatible(&actual, expectedSig)
		}
	}
	if route, ok := v.Ref.(FFIRoute); ok {
		if route.FuncSig != nil {
			actual := route.FuncSig.FunctionType()
			return e.isSignatureCompatible(&actual, expectedSig)
		}
	}
	return true // 默认放行，由运行期进一步处理
}

func (e *Executor) isSignatureCompatible(actual, expected *ast.FunctionType) bool {
	// 如果 expected 是 interface{Method} 这种没有详细签名的（默认 Return: Any），直接放行
	if expected.Return == "Any" && len(expected.Params) == 0 && !expected.Variadic {
		return true
	}

	// 参数数量校验
	if !actual.Variadic && expected.Variadic {
		return false
	}
	if !actual.Variadic && len(actual.Params) != len(expected.Params) {
		return false
	}

	// 参数类型校验
	for i := range expected.Params {
		var actType ast.GoMiniType = "Any"
		if i < len(actual.Params) {
			actType = actual.Params[i].Type
		} else if actual.Variadic {
			actType = actual.Params[len(actual.Params)-1].Type
		}

		if !expected.Params[i].Type.IsAssignableTo(actType) {
			return false
		}
	}

	// 返回值兼容性
	if actual.Return == "Void" && expected.Return == "Any" {
		return true
	}
	return actual.Return.IsAssignableTo(expected.Return)
}

func (e *Executor) Execute(ctx context.Context) (err error) {
	return e.ExecuteWithEnv(ctx, nil)
}

func (e *Executor) ExecuteWithEnv(ctx context.Context, env map[string]*Var) (err error) {
	e.runMu.Lock()
	defer e.runMu.Unlock()

	session := e.NewSession(ctx, "global")
	session.StepLimit = e.StepLimit
	defer e.CleanupSession(session)
	if err := e.prepareSession(session, env, true); err != nil {
		return err
	}
	root, err := e.scheduler.Reset(session, e)
	if err != nil {
		return err
	}
	defer e.scheduler.Stop()
	return e.runFibers(ctx, root)
}

func (e *Executor) InitializeSession(session *StackContext, env map[string]*Var, invokeMain bool) (err error) {
	if err := e.prepareSession(session, env, invokeMain); err != nil {
		return err
	}
	if e.scheduler != nil && e.scheduler.Current() == nil {
		e.runMu.Lock()
		root, resetErr := e.scheduler.Reset(session, e)
		if resetErr != nil {
			e.runMu.Unlock()
			return resetErr
		}
		err = e.runFibers(session.Context, root)
		e.scheduler.Stop()
		e.runMu.Unlock()
		return err
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

	// Main 块位于栈顶，先于 main() 调用执行。
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

// Unwind State Machine
func (e *Executor) handleUnwind(session *StackContext, task *Task) (bool, error) {
	// During UnwindContinue and UnwindBreak, OpScopeEnter tasks that were
	// in the loop body after the control-flow instruction are being skipped.
	// Their matching OpScopeExit tasks must also be skipped to avoid
	// corrupting the scope chain and losing access to variables declared
	// in outer scopes.
	if session.UnwindMode == UnwindContinue || session.UnwindMode == UnwindBreak {
		switch task.Op {
		case OpScopeEnter, OpForScopeEnter, OpRangeScopeEnter, OpCatchScopeEnter:
			session.continueSkipScope++
			return true, nil
		case OpScopeExit, OpForScopeExit:
			if session.continueSkipScope > 0 {
				session.continueSkipScope--
				return true, nil
			}
		case OpFinally:
			return false, nil
		}
	}

	if task.Op == OpScopeExit || task.Op == OpForScopeExit || task.Op == OpFinally {
		prevMode := session.UnwindMode
		session.UnwindMode = UnwindNone
		session.TaskStack = append(session.TaskStack, Task{Op: OpResumeUnwind, Data: prevMode})
		session.TaskStack = append(session.TaskStack, *task)
		return true, nil
	}

	if task.Op == OpRunDefers {
		if owner := callBoundaryDeferOwner(session); owner != nil && len(owner.DeferStack) > 0 {
			prevMode := session.UnwindMode
			session.UnwindMode = UnwindNone
			session.TaskStack = append(session.TaskStack, Task{Op: OpResumeUnwind, Data: prevMode})
			owner.RunDefers()
			return true, nil
		}
		return false, nil
	}

	if task.Op == OpCatchBoundary && session.UnwindMode == UnwindPanic {
		session.UnwindMode = UnwindNone
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
			session.UnwindMode = UnwindNone
			return true, nil
		}
	}

	if task.Op == OpRangeIter {
		if session.UnwindMode == UnwindContinue {
			pruneRangeContinueResidualTasks(session)
			session.UnwindMode = UnwindNone
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
			session.UnwindMode = UnwindNone
			if scheduleCallBoundaryDefers(session, *task, data, &prevMode) {
				return true, nil
			}
			session.UnwindMode = prevMode
		}
		oldStack := data.OldStack
		hasReturn := data.HasReturn

		if session.UnwindMode == UnwindReturn {
			session.UnwindMode = UnwindNone
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
			session.UnwindMode = UnwindNone
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

			session.UnwindMode = UnwindNone
			if err := e.dispatch(session, *task); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, nil // Continue unwinding if it's a panic/return
	}

	return false, nil
}

// 供解卷状态恢复使用
func (e *Executor) varToMapKey(v *Var) (string, error) {
	if v == nil {
		return "", errors.New("map key is nil")
	}
	switch v.VType {
	case TypeString:
		return v.Str, nil
	case TypeInt:
		return strconv.FormatInt(v.I64, 10), nil
	case TypeBool:
		return strconv.FormatBool(v.Bool), nil
	case TypeFloat:
		return strconv.FormatFloat(v.F64, 'f', -1, 64), nil
	}
	return "", fmt.Errorf("unsupported map key type: %v", v.VType)
}

func (e *Executor) mapKeyToVar(k string, keyType RuntimeType) *Var {
	if keyType.IsInt() {
		val, _ := strconv.ParseInt(k, 10, 64)
		return NewInt(val)
	}
	if keyType.IsBool() {
		val, _ := strconv.ParseBool(k)
		return NewBool(val)
	}
	if keyType.IsNumeric() && !keyType.IsInt() {
		val, _ := strconv.ParseFloat(k, 64)
		return NewFloat(val)
	}
	return NewString(k)
}

func pruneRangeContinueResidualTasks(session *StackContext) {
	for i := len(session.TaskStack) - 1; i >= 0; i-- {
		task := session.TaskStack[i]
		if task.Op == OpLoopBoundary && task.Data == nil {
			session.TaskStack = session.TaskStack[:i+1]
			return
		}
	}
}

func (e *Executor) dispatch(session *StackContext, task Task) error {
	switch task.Op {
	case OpLineStep:
		// Should be handled in the main loop before dispatch
		return nil
	case OpDeclareVar:
		data := task.Data.(*DeclareVarData)
		if data.Sym.Kind == SymbolLocal {
			return session.DeclareSymbol(data.Sym, data.Kind)
		}
		if session.Stack.Depth == 1 && session.Stack.Scope == "global" {
			if _, ok := session.Shared.LoadGlobal(data.Name); ok {
				return nil
			}
		}
		return session.NewVar(data.Name, data.Kind)
	case OpApplyBinary:
		op := task.Data.(string)
		r := session.ValueStack.Pop()
		l := session.ValueStack.Pop()
		res, err := e.evalBinaryExprDirect(op, l, r)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpApplyUnary:
		op := task.Data.(string)
		val := session.ValueStack.Pop()
		if op == "ToBool" {
			b, err := val.ToBool()
			if err != nil {
				return err
			}
			session.ValueStack.Push(NewBool(b))
			return nil
		}
		res, err := e.evalUnaryExprDirect(op, val)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpJumpIf:
		var op string
		var rightTasks []Task
		if data, ok := task.Data.(*JumpData); ok {
			op = data.Operator
			rightTasks = data.Right
		} else {
			return errors.New("OpJumpIf missing JumpData")
		}
		l := session.ValueStack.Peek()
		lb, err := l.ToBool()
		if err != nil {
			return err
		}
		if op == "&&" || op == "And" {
			if !lb {
				// Short circuit, pop the left value and push false
				session.ValueStack.Pop()
				session.ValueStack.Push(NewBool(false))
				return nil
			}
		} else { // || or Or
			if lb {
				// Short circuit, pop the left value and push true
				session.ValueStack.Pop()
				session.ValueStack.Push(NewBool(true))
				return nil
			}
		}
		session.ValueStack.Pop() // Discard Left
		// Push a task to evaluate Right and ensure it's converted to Bool
		session.TaskStack = append(session.TaskStack, Task{Op: OpApplyUnary, Data: "ToBool"}) // a pseudo unary to enforce bool
		session.TaskStack = append(session.TaskStack, rightTasks...)
		return nil
	case OpLoadVar:
		var (
			name string
			sym  SymbolRef
		)
		switch data := task.Data.(type) {
		case string:
			name = data
		case *LoadVarData:
			name = data.Name
			sym = data.Sym
		default:
			return errors.New("OpLoadVar missing LoadVarData")
		}
		var (
			v   *Var
			err error
		)
		if sym.Kind != SymbolUnknown {
			v, err = session.LoadSymbol(sym)
		} else {
			v, err = session.Load(name)
		}
		if err != nil {
			exec := session.Executor
			if exec != nil {
				if path, ok := exec.importAliases[name]; ok {
					if mod, ok := session.Shared.Module(path); ok {
						session.ValueStack.Push(mod)
						return nil
					}
				}
			}
			return err
		}
		session.ValueStack.Push(v)
		return nil
	case OpLoadLocal:
		sym, ok := task.Data.(SymbolRef)
		if !ok {
			return errors.New("OpLoadLocal missing SymbolRef")
		}
		v, err := session.LoadSymbol(sym)
		if err != nil {
			return err
		}
		session.ValueStack.Push(v)
		return nil
	case OpLoadUpvalue:
		sym, ok := task.Data.(SymbolRef)
		if !ok {
			return errors.New("OpLoadUpvalue missing SymbolRef")
		}
		v, err := session.LoadSymbol(sym)
		if err != nil {
			return err
		}
		session.ValueStack.Push(v)
		return nil
	case OpStoreLocal:
		sym, ok := task.Data.(SymbolRef)
		if !ok {
			return errors.New("OpStoreLocal missing SymbolRef")
		}
		return session.StoreSymbol(sym, session.ValueStack.Pop())
	case OpStoreUpvalue:
		sym, ok := task.Data.(SymbolRef)
		if !ok {
			return errors.New("OpStoreUpvalue missing SymbolRef")
		}
		return session.StoreSymbol(sym, session.ValueStack.Pop())
	case OpScopeEnter:
		scopeName := task.Data.(string)
		session.ScopeApply(scopeName)
		return nil
	case OpScopeExit:
		session.ScopeExit()
		return nil
	case OpAssign:
		if session.LHSStack == nil {
			session.LHSStack = &LHSStack{}
		}
		val := session.ValueStack.Pop()
		lhs := session.LHSStack.Pop()
		return e.assignAddress(session, lhs, val)
	case OpDoCall:
		call := task.Data.(*DoCallData)
		return e.setupFuncCall(session, call.Name, call, call.Args, nil)
	case OpMultiAssign:
		if session.LHSStack == nil {
			session.LHSStack = &LHSStack{}
		}
		lhsCount := task.Data.(int)
		val := session.ValueStack.Pop()
		descs := make([]LHSValue, lhsCount)
		for i := lhsCount - 1; i >= 0; i-- {
			descs[i] = session.LHSStack.Pop()
		}

		if val == nil {
			return errors.New("multi assignment: RHS evaluated to nil")
		}

		var elements []*Var
		switch val.VType {
		case TypeArray:
			rawElements := val.Ref.(*VMArray).Snapshot()
			// Snapshot to prevent issues with self-assignment like a, b = b, a
			elements = make([]*Var, len(rawElements))
			for i, v := range rawElements {
				if v != nil {
					elements[i] = v.Copy()
				} else {
					elements[i] = nil
				}
			}
		default:
			return &VMError{Message: fmt.Sprintf("cannot destructure type %v", val.VType), IsPanic: true}
		}

		if len(elements) < lhsCount {
			return &VMError{Message: fmt.Sprintf("multi assignment: not enough elements to destructure (need %d, got %d)", lhsCount, len(elements)), IsPanic: true}
		}

		for i := 0; i < lhsCount; i++ {
			if err := e.storeAddress(session, descs[i], elements[i]); err != nil {
				return err
			}
		}
		return nil
	case OpIncDec:
		if session.LHSStack == nil {
			session.LHSStack = &LHSStack{}
		}
		op := task.Data.(string)
		lhs := session.LHSStack.Pop()
		return e.updateAddress(session, lhs, op)
	case OpReturn:
		count := task.Data.(int)
		if count > 1 {
			// 多返回值，打包成 Tuple
			elements := make([]*Var, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = session.ValueStack.Pop()
			}
			res := &Var{VType: TypeArray, Ref: &VMArray{Data: elements}}
			_ = session.StoreReturn(res)
		} else if count == 1 {
			// 单返回值
			res := session.ValueStack.Pop()
			if res != nil {
				_ = session.StoreReturn(res)
			}
		}
		session.UnwindMode = UnwindReturn
		return nil
	case OpInterrupt:
		interruptType := task.Data.(string)
		switch interruptType {
		case "break":
			session.UnwindMode = UnwindBreak
		case "continue":
			session.UnwindMode = UnwindContinue
		}
		return nil
	case OpEvalLHS:
		if session.LHSStack == nil {
			session.LHSStack = &LHSStack{}
		}
		if task.Data == nil {
			session.LHSStack.Push(nil)
			return nil
		}
		if lhsData, ok := task.Data.(*LHSData); ok {
			switch lhsData.Kind {
			case LHSTypeEnv:
				session.LHSStack.Push(&LHSEnv{Name: lhsData.Name, Sym: lhsData.Sym})
				return nil
			case LHSTypeIndex:
				idx := e.unwrapAddressVar(session.ValueStack.Pop())
				obj := e.unwrapAddressVar(session.ValueStack.Pop())
				if idx != nil {
					idx = idx.Copy()
				}
				session.LHSStack.Push(&LHSIndex{Obj: obj, Index: idx})
				return nil
			case LHSTypeMember:
				obj := e.unwrapAddressVar(session.ValueStack.Pop())
				session.LHSStack.Push(&LHSMember{Obj: obj, Property: lhsData.Property})
				return nil
			case LHSTypeStar:
				obj := e.unwrapAddressVar(session.ValueStack.Pop())
				session.LHSStack.Push(&LHSDeref{Target: obj})
				return nil
			case LHSTypeSlice:
				var high, low *Var
				if lhsData.HasHigh {
					high = e.unwrapAddressVar(session.ValueStack.Pop())
					if high != nil {
						high = high.Copy()
					}
				}
				if lhsData.HasLow {
					low = e.unwrapAddressVar(session.ValueStack.Pop())
					if low != nil {
						low = low.Copy()
					}
				}
				obj := e.unwrapAddressVar(session.ValueStack.Pop())
				session.LHSStack.Push(&LHSSlice{Obj: obj, Low: low, High: high})
				return nil
			case LHSTypeNone:
				session.LHSStack.Push(nil)
				return nil
			}
		}

		return errors.New("OpEvalLHS missing LHSData")
	case OpIndex:
		idx := session.ValueStack.Pop()
		obj := session.ValueStack.Pop()
		data, ok := task.Data.(*IndexData)
		if !ok {
			return errors.New("OpIndex missing IndexData")
		}

		if data.Multi {
			obj = e.unwrapValue(obj)
			if obj == nil || isEmptyVar(obj) {
				return errors.New("index access on nil")
			}
			if idx == nil {
				return errors.New("index access with nil index")
			}
			if obj.VType == TypeMap {
				m := obj.Ref.(*VMMap)
				key, err := e.varToMapKey(idx)
				if err != nil {
					return err
				}
				tuple := make([]*Var, 2)
				if val, ok := m.Load(key); ok {
					tuple[0] = val
					tuple[1] = NewBool(true)
				} else {
					_, valType, _ := obj.RuntimeType().GetMapKeyValueTypes()
					tuple[0] = e.ToVar(session, valType.ZeroVar(), nil)
					tuple[1] = NewBool(false)
				}
				v := &Var{VType: TypeArray, Ref: &VMArray{Data: tuple}}
				v.SetRuntimeType(data.ResultType)
				session.ValueStack.Push(v)
				return nil
			}
			return fmt.Errorf("multi-index only supported for maps, got %v", obj.VType)
		}
		res, err := e.evalIndexExprDirect(session, obj, idx)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpMember:
		prop := task.Data.(string)
		obj := session.ValueStack.Pop()
		res, err := e.evalMemberExprDirect(session, obj, prop)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpPop:
		session.ValueStack.Pop()
		return nil
	case OpComposite:
		var (
			typ     RuntimeType
			entries []CompositeEntryData
		)
		if data, ok := task.Data.(*CompositeData); ok {
			typ = data.Type
			entries = data.Entries
		} else {
			return errors.New("OpComposite missing CompositeData")
		}
		isArray := typ.IsArray()
		isMap := typ.IsMap()

		if isArray {
			elemType, _ := typ.ReadArrayItemType()
			res := make([]*Var, len(entries))
			// ValueStack has [V1, V2, ..., VN]
			// So we must pop in reverse: V_N first, then V_N-1...
			for i := len(entries) - 1; i >= 0; i-- {
				val := e.normalizeTypedValue(session.ValueStack.Pop(), elemType)
				res[i] = val
			}
			v := &Var{VType: TypeArray, Ref: &VMArray{Data: res}}
			v.SetRuntimeType(typ)
			session.ValueStack.Push(v)
			return nil
		}

		res := make(map[string]*Var)
		var valType RuntimeType
		if isMap {
			_, valType, _ = typ.GetMapKeyValueTypes()
		}

		// Values are pushed as [..., K_i, V_i]
		// So we must pop in reverse order: V_i then K_i
		for i := len(entries) - 1; i >= 0; i-- {
			val := session.ValueStack.Pop()
			if isMap {
				val = e.normalizeTypedValue(val, valType)
			}

			keyName := ""
			if entries[i].IdentKey != "" {
				keyName = entries[i].IdentKey
			} else if entries[i].HasExprKey {
				keyVal := session.ValueStack.Pop()
				keyName = keyVal.Str
				if keyVal.VType == TypeInt {
					keyName = strconv.FormatInt(keyVal.I64, 10)
				}
			}

			res[keyName] = val
		}
		v := &Var{VType: TypeMap, Ref: &VMMap{Data: res}}
		v.SetRuntimeType(typ)
		session.ValueStack.Push(v)
		return nil
	case OpSlice:
		var high, low, obj *Var
		data := task.Data.(*SliceData)
		if data.HasHigh {
			high = session.ValueStack.Pop()
		}
		if data.HasLow {
			low = session.ValueStack.Pop()
		}
		obj = session.ValueStack.Pop()

		res, err := e.evalSliceExprDirect(session, obj, low, high)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpCall:
		var name string
		var receiver *Var
		var mod *VMModule
		var callable *Var
		data := task.Data.(*CallData)
		argCount := data.ArgCount

		// Arguments are on top of stack, then Func eval result (if any)
		// Let's pop arguments first!
		args := make([]*Var, argCount)
		for i := argCount - 1; i >= 0; i-- {
			args[i] = session.ValueStack.Pop()
		}
		var argLHS []LHSValue
		if data.CaptureArgLHS {
			argLHS = make([]LHSValue, argCount)
			for i := argCount - 1; i >= 0; i-- {
				argLHS[i] = session.LHSStack.Pop()
			}
		}

		// 处理变长参数展开 f(args...)
		ellipsis := data.Ellipsis
		if ellipsis && len(args) > 0 {
			last := e.unwrapValue(args[len(args)-1])
			if last != nil && last.VType == TypeArray {
				arr := last.Ref.(*VMArray)
				items := arr.Snapshot()
				newArgs := make([]*Var, len(args)-1+len(items))
				copy(newArgs, args[:len(args)-1])
				copy(newArgs[len(args)-1:], items)
				args = newArgs
			}
		}

		switch data.Mode {
		case CallByName:
			name = data.Name
		case CallByMember:
			obj := session.ValueStack.Pop()
			if obj == nil {
				return errors.New("calling method on nil object")
			}

			res, err := e.evalMemberExprDirect(session, obj, data.Name)
			if err != nil {
				return err
			}

			if res != nil && res.VType == TypeClosure {
				if mv, ok := res.Ref.(*VMMethodValue); ok {
					receiver = mv.Receiver
					name = mv.Method
				} else {
					callable = res
				}
			} else if res != nil && res.VType == TypeModule {
				mod = res.Ref.(*VMModule)
				name = data.Name
			} else if res != nil {
				callable = res
			} else {
				return fmt.Errorf("property %s is not a callable function on %v", data.Name, obj.VType)
			}
		case CallByValue:
			callable = session.ValueStack.Pop()
		}

		if name != "" && mod == nil && callable == nil {
			loadTarget := data.Sym
			if loadTarget.Kind == SymbolUnknown {
				loadTarget = SymbolRef{Name: name}
			}
			if v, err := session.LoadSymbol(loadTarget); err == nil && v != nil {
				callable = v
			}
		}

		totalArgs := len(args)
		offset := 0
		if receiver != nil {
			totalArgs++
			offset = 1
		}
		finalArgs := make([]*Var, totalArgs)
		var finalArgLHS []LHSValue
		if argLHS != nil {
			finalArgLHS = make([]LHSValue, totalArgs)
		}
		if receiver != nil {
			finalArgs[0] = receiver
			if finalArgLHS != nil {
				finalArgLHS[0] = nil
			}
		}
		copy(finalArgs[offset:], args)
		if finalArgLHS != nil {
			copy(finalArgLHS[offset:], argLHS)
		}

		return e.invokeCall(session, name, receiver, mod, callable, finalArgs, finalArgLHS)
	case OpGo:
		var name string
		var receiver *Var
		var mod *VMModule
		var callable *Var
		data := task.Data.(*CallData)
		argCount := data.ArgCount

		args := make([]*Var, argCount)
		for i := argCount - 1; i >= 0; i-- {
			args[i] = session.ValueStack.Pop()
		}

		if data.Ellipsis && len(args) > 0 {
			last := e.unwrapValue(args[len(args)-1])
			if last != nil && last.VType == TypeArray {
				arr := last.Ref.(*VMArray)
				items := arr.Snapshot()
				newArgs := make([]*Var, len(args)-1+len(items))
				copy(newArgs, args[:len(args)-1])
				copy(newArgs[len(args)-1:], items)
				args = newArgs
			}
		}

		switch data.Mode {
		case CallByName:
			name = data.Name
		case CallByMember:
			obj := session.ValueStack.Pop()
			if obj == nil {
				return errors.New("calling method on nil object")
			}

			res, err := e.evalMemberExprDirect(session, obj, data.Name)
			if err != nil {
				return err
			}

			if res != nil && res.VType == TypeClosure {
				if mv, ok := res.Ref.(*VMMethodValue); ok {
					receiver = mv.Receiver
					name = mv.Method
				} else {
					callable = res
				}
			} else if res != nil && res.VType == TypeModule {
				mod = res.Ref.(*VMModule)
				name = data.Name
			} else if res != nil {
				callable = res
			} else {
				return fmt.Errorf("property %s is not a callable function on %v", data.Name, obj.VType)
			}
		case CallByValue:
			callable = session.ValueStack.Pop()
		}

		if name != "" && mod == nil && callable == nil {
			loadTarget := data.Sym
			if loadTarget.Kind == SymbolUnknown {
				loadTarget = SymbolRef{Name: name}
			}
			if v, err := session.LoadSymbol(loadTarget); err == nil && v != nil {
				callable = v
			}
		}

		totalArgs := len(args)
		offset := 0
		if receiver != nil {
			totalArgs++
			offset = 1
		}
		finalArgs := make([]*Var, totalArgs)
		if receiver != nil {
			finalArgs[0] = receiver
		}
		copy(finalArgs[offset:], args)

		err := e.goCall(session, name, receiver, mod, callable, finalArgs)
		return err
	case OpInvokeDirect:
		data := task.Data.(*DirectCallData)
		args := append([]*Var(nil), data.Args...)
		return e.invokeCall(session, data.Name, data.Receiver, data.Module, data.Callable, args, nil)
	case OpResumeFFI:
		data, ok := task.Data.(*ResumeFFIData)
		if !ok || data == nil {
			return errors.New("OpResumeFFI missing ResumeFFIData")
		}
		res, err := e.finishFFI(session, data.Route, data.CopyBackTargets, data.Ret, data.Err)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpResumeModule:
		data, ok := task.Data.(*ResumeModuleData)
		if !ok || data == nil {
			return errors.New("OpResumeModule missing ResumeModuleData")
		}
		if data.Err != nil {
			return data.Err
		}
		session.ValueStack.Push(data.Value)
		return nil
	case OpCallBoundary:
		data, ok := task.Data.(*CallBoundaryData)
		if !ok || data == nil {
			return fmt.Errorf("OpCallBoundary data is not *CallBoundaryData: %T (%v)", task.Data, task.Data)
		}
		if scheduleCallBoundaryDefers(session, task, data, nil) {
			return nil
		}
		oldStack := data.OldStack
		hasReturn := data.HasReturn
		valueBase := data.ValueBase
		lhsBase := data.LHSBase

		// Restore executor if saved (cross-module calls)
		if data.OldExec != nil {
			session.Executor = data.OldExec
		}
		if data.OldShared != nil {
			session.Shared = data.OldShared
		}

		var retVal *Var
		if hasReturn {
			retVal, _ = session.LoadReturn()
		}

		session.Stack = oldStack
		if session.ValueStack != nil {
			session.ValueStack.Truncate(valueBase)
		}
		if session.LHSStack != nil {
			session.LHSStack.Truncate(lhsBase)
		}

		if hasReturn {
			session.ValueStack.Push(retVal)
		}

		if session.UnwindMode == UnwindReturn {
			session.UnwindMode = UnwindNone
		}
		return nil
	case OpAssert:
		val := session.ValueStack.Pop()
		var (
			targetType RuntimeType
			multi      bool
			resultType RuntimeType
		)
		if data, ok := task.Data.(*AssertData); ok {
			targetType = data.TargetType
			multi = data.Multi
			resultType = data.ResultType
		} else {
			return errors.New("OpAssert missing AssertData")
		}
		res, err := e.CheckSatisfaction(val, targetType.Raw.String())
		if multi {
			if err != nil {
				// 返回 (nil, false)
				tuple := make([]*Var, 2)
				tuple[0] = nil
				tuple[1] = NewBool(false)
				v := &Var{VType: TypeArray, Ref: &VMArray{Data: tuple}}
				v.SetRuntimeType(resultType)
				session.ValueStack.Push(v)
			} else {
				// 返回 (res, true)
				tuple := make([]*Var, 2)
				tuple[0] = res
				tuple[1] = NewBool(true)
				v := &Var{VType: TypeArray, Ref: &VMArray{Data: tuple}}
				v.SetRuntimeType(resultType)
				session.ValueStack.Push(v)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("interface conversion: %v", err)
		}
		session.ValueStack.Push(res)
		return nil
	case OpRunDefers:
		if owner := callBoundaryDeferOwner(session); owner != nil && len(owner.DeferStack) > 0 {
			owner.RunDefers()
		}
		return nil
	case OpScheduleDefer:
		data := task.Data.(*DeferData)
		session.Stack.AddDefer(func() {
			if data.PopResult {
				session.TaskStack = append(session.TaskStack, Task{Op: OpPop})
			}
			session.TaskStack = append(session.TaskStack, data.Tasks...)
		})
		return nil
	case OpLoopBoundary:
		if err := session.Context.Err(); err != nil {
			return err
		}
		if data, ok := task.Data.(*ForData); ok {
			if len(data.Cond) > 0 {
				session.TaskStack = append(session.TaskStack, Task{Op: OpForCond, Data: data})
				session.TaskStack = append(session.TaskStack, data.Cond...)
			} else {
				e.scheduleForBody(session, data)
			}
			return nil
		}
		if task.Data == nil {
			// Marker boundary (e.g. for Range)
			return nil
		}
		if _, ok := task.Data.(*SwitchData); ok {
			// Switch boundary, just a placeholder for break/continue
			return nil
		}
		return errors.New("OpLoopBoundary missing ForData")
	case OpForStart:
		data, ok := task.Data.(*ForData)
		if !ok || data == nil {
			return errors.New("OpForStart missing ForData")
		}
		if len(data.Cond) > 0 {
			session.TaskStack = append(session.TaskStack, Task{Op: OpForCond, Data: data})
			session.TaskStack = append(session.TaskStack, data.Cond...)
			return nil
		}
		e.scheduleForBody(session, data)
		return nil
	case OpForCond:
		condVal := session.ValueStack.Pop()
		b, err := condVal.ToBool()
		if err != nil {
			return err
		}
		if b {
			if data, ok := task.Data.(*ForData); ok {
				e.scheduleForBody(session, data)
			} else {
				return errors.New("OpForCond missing ForData")
			}
		}
		return nil
	case OpForScopeEnter:
		session.ScopeApplyLoopBody("for_body")
		return nil
	case OpForScopeExit:
		session.SyncLoopScope()
		session.ScopeExit()
		return nil
	case OpLoopContinue:
		return nil
	case OpRangeInit:
		obj := session.ValueStack.Pop()
		if obj == nil {
			return nil
		}
		data := task.Data.(*RangeData)
		rData := &RangeData{
			Key:     data.Key,
			Value:   data.Value,
			KeySym:  data.KeySym,
			ValSym:  data.ValSym,
			KeyType: data.KeyType,
			ValType: data.ValType,
			Define:  data.Define,
			Body:    data.Body,
			Obj:     obj,
		}
		switch obj.VType {
		case TypeArray:
			rData.Length = obj.Ref.(*VMArray).Len()
		case TypeMap:
			m := obj.Ref.(*VMMap)
			rData.Keys = m.Keys()
			rData.Length = len(rData.Keys)
		}
		session.TaskStack = append(session.TaskStack, Task{Op: OpLoopBoundary})
		session.TaskStack = append(session.TaskStack, Task{Op: OpRangeIter, Data: rData})
		return nil
	case OpRangeIter:
		rData := task.Data.(*RangeData)
		if err := session.Context.Err(); err != nil {
			return err
		}
		if rData.Index >= rData.Length {
			return nil
		}
		var key, val *Var
		if rData.Obj.VType == TypeArray {
			key = NewInt(int64(rData.Index))
			val, _ = rData.Obj.Ref.(*VMArray).Load(rData.Index)
		} else {
			k := rData.Keys[rData.Index]
			keyType, _, _ := rData.Obj.RuntimeType().GetMapKeyValueTypes()
			key = e.mapKeyToVar(k, keyType)
			val, _ = rData.Obj.Ref.(*VMMap).Load(k)
		}
		rData.Index++

		session.TaskStack = append(session.TaskStack, Task{Op: OpRangeIter, Data: rData})
		session.TaskStack = append(session.TaskStack, Task{Op: OpScopeExit})
		session.TaskStack = append(session.TaskStack, rData.Body...)
		session.TaskStack = append(session.TaskStack, Task{
			Op:   OpRangeScopeEnter,
			Data: &RangeScopeData{Range: rData, Key: key, Val: val},
		})
		return nil
	case OpRangeScopeEnter:
		data, ok := task.Data.(*RangeScopeData)
		if !ok || data == nil {
			return errors.New("OpRangeScopeEnter missing RangeScopeData")
		}
		rData := data.Range
		key := data.Key
		val := data.Val
		session.ScopeApply("for_range_body")
		if rData.Define {
			if rData.Key != "" && rData.Key != "_" {
				if rData.KeySym.Kind == SymbolLocal {
					_ = session.DeclareSymbol(rData.KeySym, rData.KeyType)
					_ = session.StoreSymbol(rData.KeySym, key)
				} else {
					_ = session.AddVariable(rData.Key, key)
				}
			}
			if rData.Value != "" && rData.Value != "_" && val != nil {
				if rData.ValSym.Kind == SymbolLocal {
					_ = session.DeclareSymbol(rData.ValSym, rData.ValType)
					_ = session.StoreSymbol(rData.ValSym, val)
				} else {
					_ = session.AddVariable(rData.Value, val)
				}
			}
		} else {
			if rData.Key != "" && rData.Key != "_" {
				if rData.KeySym.Kind != SymbolUnknown {
					_ = session.StoreSymbol(rData.KeySym, key)
				} else {
					_ = session.Store(rData.Key, key)
				}
			}
			if rData.Value != "" && rData.Value != "_" && val != nil {
				if rData.ValSym.Kind != SymbolUnknown {
					_ = session.StoreSymbol(rData.ValSym, val)
				} else {
					_ = session.Store(rData.Value, val)
				}
			}
		}
		return nil
	case OpSwitchTag:
		if plan, ok := task.Data.(*SwitchData); ok {
			tag := NewBool(true)
			if plan.HasTag {
				tag = session.ValueStack.Pop()
			}
			session.TaskStack = append(session.TaskStack, Task{
				Op:   OpSwitchNextCase,
				Data: &SwitchState{Plan: plan, Tag: tag},
			})
			return nil
		}
		return errors.New("OpSwitchTag missing SwitchData")
	case OpSwitchStart:
		if plan, ok := task.Data.(*SwitchData); ok {
			session.TaskStack = append(session.TaskStack, Task{Op: OpLoopBoundary, Data: plan})
			session.TaskStack = append(session.TaskStack, Task{Op: OpSwitchTag, Data: plan})
			return nil
		}
		return errors.New("OpSwitchStart missing SwitchData")
	case OpSwitchNextCase:
		if state, ok := task.Data.(*SwitchState); ok {
			if state.Index >= len(state.Plan.Cases) {
				if len(state.Plan.DefaultBody) > 0 {
					session.TaskStack = append(session.TaskStack, e.switchCaseTasks(state.Plan, state.Tag, state.Plan.DefaultBody, "switch_default")...)
				}
				return nil
			}

			caseData := state.Plan.Cases[state.Index]
			if state.Plan.IsType {
				if e.switchTypeCaseMatches(state.Tag, caseData.TypeNames) {
					session.TaskStack = append(session.TaskStack, e.switchCaseTasks(state.Plan, state.Tag, caseData.Body, "switch_matched")...)
					return nil
				}
				state.Index++
				state.ExprIx = 0
				session.TaskStack = append(session.TaskStack, task)
				return nil
			}
			if state.ExprIx < len(caseData.Exprs) {
				next := *state
				next.ExprIx++
				session.TaskStack = append(session.TaskStack, Task{Op: OpSwitchMatchCase, Data: &next})
				session.TaskStack = append(session.TaskStack, caseData.Exprs[state.ExprIx]...)
				return nil
			}

			state.Index++
			state.ExprIx = 0
			session.TaskStack = append(session.TaskStack, task)
			return nil
		}
		return errors.New("OpSwitchNextCase missing SwitchState")
	case OpSwitchMatchCase:
		if state, ok := task.Data.(*SwitchState); ok {
			val := session.ValueStack.Pop()
			res, _ := e.evalComparison("==", state.Tag, val)
			if res != nil && res.Bool {
				caseData := state.Plan.Cases[state.Index]
				session.TaskStack = append(session.TaskStack, caseData.Body...)
				return nil
			}
			session.TaskStack = append(session.TaskStack, Task{Op: OpSwitchNextCase, Data: state})
			return nil
		}
		return errors.New("OpSwitchMatchCase missing SwitchState")
	case OpCatchBoundary:
		return nil
	case OpFinally:
		if data, ok := task.Data.(*FinallyData); ok {
			session.TaskStack = append(session.TaskStack, data.Body...)
		} else {
			return errors.New("OpFinally missing FinallyData")
		}
		return nil
	case OpCatchScopeEnter:
		data, ok := task.Data.(*CatchScopeData)
		if !ok || data == nil || data.Catch == nil {
			return errors.New("OpCatchScopeEnter missing CatchScopeData")
		}
		varName := data.Catch.VarName
		varSym := data.Catch.Sym
		panicVar := data.Panic
		session.ScopeApply("catch")
		if varName != "" {
			if varSym.Kind == SymbolLocal {
				_ = session.DeclareSymbol(varSym, MustParseRuntimeType("Any"))
				_ = session.StoreSymbol(varSym, panicVar)
			} else {
				_ = session.NewVar(varName, MustParseRuntimeType("Any"))
				_ = session.Store(varName, panicVar)
			}
		}
		return nil
	case OpBranchIf:
		condVal := session.ValueStack.Pop()
		b, err := condVal.ToBool()
		if err != nil {
			return err
		}
		if data, ok := task.Data.(*BranchData); ok {
			if b {
				session.TaskStack = append(session.TaskStack, data.Then...)
			} else if len(data.Else) > 0 {
				session.TaskStack = append(session.TaskStack, data.Else...)
			}
			return nil
		}
		return errors.New("OpBranchIf missing BranchData")
	case OpInitVar:
		name := task.Data.(string)
		val := session.ValueStack.Pop()
		return session.AddVariable(name, val)
	case OpInitGlobal:
		data, ok := task.Data.(*InitGlobalData)
		if !ok || data == nil {
			return errors.New("OpInitGlobal missing InitGlobalData")
		}
		if data.HasInit {
			session.TaskStack = append(session.TaskStack, Task{Op: OpStoreGlobalInit, Data: data.Name})
			session.TaskStack = append(session.TaskStack, cloneTasks(data.Plan)...)
			return nil
		}
		kind := data.Kind
		if kind.IsEmpty() {
			kind = MustParseRuntimeType("Any")
		}
		session.Shared.StoreGlobal(data.Name, e.initializeType(session, kind, 0))
		return nil
	case OpStoreGlobalInit:
		name, ok := task.Data.(string)
		if !ok || name == "" {
			return errors.New("OpStoreGlobalInit missing global name")
		}
		session.Shared.StoreGlobal(name, session.ValueStack.Pop())
		return nil
	case OpFinishSharedInit:
		data, _ := task.Data.(*FinishSharedInitData)
		finishSessionSharedInitialization(session, nil)
		if data != nil {
			session.Shared.ApplyEnv(data.Env)
		}
		return nil
	case OpResumeUnwind:
		mode := task.Data.(UnwindMode)
		if session.UnwindMode == UnwindNone {
			// Keep panic unwinding alive as long as any panic state remains.
			// Some runtime panic sites historically populated message/trace
			// without a concrete panic value, and treating "nil PanicVar" as
			// "not a panic anymore" can accidentally downgrade the unwind into
			// a return and swallow the original failure.
			if mode == UnwindPanic && session.PanicVar == nil && session.PanicMessage == "" && len(session.PanicTrace) == 0 {
				session.UnwindMode = UnwindReturn
			} else {
				session.UnwindMode = mode
			}
		}
		return nil
	case OpImportInit:
		path := task.Data.(*ImportInitData).Path
		path = strings.Trim(path, " \t\n\r")
		if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
			return fmt.Errorf("invalid import path: %s", path)
		}

		if session.ImportChain[path] {
			return fmt.Errorf("circular dependency detected: %s", path)
		}
		if v, loadState := session.Shared.BeginModuleLoad(path); loadState == ModuleLoadReady {
			session.ValueStack.Push(v)
			return nil
		} else if loadState == ModuleLoadWait {
			if e.scheduler == nil || e.scheduler.Current() == nil {
				return fmt.Errorf("module %s is already loading and cannot be parked without an active scheduler", path)
			}
			fiber, frame, err := e.scheduler.ParkCurrent()
			if err != nil {
				return err
			}
			session.Shared.AddModuleWaiter(path, moduleWaiter{
				Fiber: fiber,
				Frame: frame,
				Resume: Task{
					Op:   OpResumeModule,
					Data: &ResumeModuleData{Path: path},
				},
			})
			return errFiberSuspend
		}
		session.ImportChain[path] = true
		defer delete(session.ImportChain, path)

		if e.ModulePlanLoader != nil {
			prepared, err := e.ModulePlanLoader(path)
			if err == nil {
				err := e.startImportedProgram(session, path, prepared)
				if err != nil && !errors.Is(err, errFiberYield) {
					waiters := session.Shared.FinishModuleLoad(path, nil)
					e.scheduleModuleWaiters(waiters, nil, err)
					return err
				}
				return err
			}
			if !errors.Is(err, ErrModuleNotFound) {
				waiters := session.Shared.FinishModuleLoad(path, nil)
				e.scheduleModuleWaiters(waiters, nil, err)
				return err
			}
		}

		// Fallback to FFI
		ffiMod := &VMModule{Name: path, Data: make(map[string]*Var)}
		found := false
		prefix1 := path + "."
		prefix2 := strings.ReplaceAll(path, "/", ".") + "."
		for name, route := range e.routes {
			if strings.HasPrefix(name, prefix1) || strings.HasPrefix(name, prefix2) {
				found = true
				var methodName string
				if strings.HasPrefix(name, prefix1) {
					methodName = strings.TrimPrefix(name, prefix1)
				} else {
					methodName = strings.TrimPrefix(name, prefix2)
				}
				ffiMod.Store(methodName, &Var{
					VType: TypeAny,
					Str:   name,
					Ref:   route,
				})
			}
		}

		for name, val := range e.consts {
			if strings.HasPrefix(name, prefix1) || strings.HasPrefix(name, prefix2) {
				found = true
				var constName string
				if strings.HasPrefix(name, prefix1) {
					constName = strings.TrimPrefix(name, prefix1)
				} else {
					constName = strings.TrimPrefix(name, prefix2)
				}
				ffiMod.Store(constName, e.evalLiteralToVar(val))
			}
		}

		if found {
			res := &Var{VType: TypeModule, Ref: ffiMod}
			waiters := session.Shared.FinishModuleLoad(path, res)
			session.ValueStack.Push(res)
			e.scheduleModuleWaiters(waiters, res, nil)
			return nil
		}
		err := fmt.Errorf("failed to load module %s", path)
		waiters := session.Shared.FinishModuleLoad(path, nil)
		e.scheduleModuleWaiters(waiters, nil, err)
		return err

	case OpImportDone:
		return errors.New("OpImportDone should not be reached in synchronous import mode")
	case OpPush:
		if v, ok := task.Data.(*Var); ok {
			session.ValueStack.Push(v)
		} else {
			session.ValueStack.Push(nil)
		}
		return nil
	case OpMakeClosure:
		data := task.Data.(*ClosureData)
		clCtx := &StackContext{
			Context:   session.Context,
			Executor:  session.Executor,
			Shared:    session.Shared,
			Stack:     session.Stack,
			StepLimit: session.StepLimit,
			Debugger:  session.Debugger,
		}
		closure := &VMClosure{
			FunctionSig:  CloneRuntimeFuncSig(data.FunctionSig),
			BodyTasks:    data.BodyTasks,
			UpvalueSlots: make([]*Var, len(data.CaptureRefs)),
			UpvalueNames: make([]string, len(data.CaptureRefs)),
			Context:      &LexicalContext{Executor: clCtx.Executor, Shared: clCtx.Shared, Stack: clCtx.Stack},
		}
		for i, capture := range data.CaptureRefs {
			cellVar, err := session.CaptureSymbol(capture)
			if err != nil {
				return fmt.Errorf("failed to capture variable %s: %w", capture.Name, err)
			}
			closure.UpvalueSlots[i] = cellVar
			closure.UpvalueNames[i] = capture.Name
		}
		v := NewVar(ast.TypeClosure, TypeClosure)
		v.Ref = closure
		session.ValueStack.Push(v)
		return nil
	default:
		return fmt.Errorf("unhandled opcode: %v", task.Op)
	}
}

func (e *Executor) scheduleForBody(session *StackContext, data *ForData) {
	session.TaskStack = append(session.TaskStack, Task{Op: OpLoopBoundary, Data: data})
	if len(data.Update) > 0 {
		session.TaskStack = append(session.TaskStack, data.Update...)
	}
	session.TaskStack = append(session.TaskStack, Task{Op: OpLoopContinue})
	session.TaskStack = append(session.TaskStack, Task{Op: OpForScopeExit})
	session.TaskStack = append(session.TaskStack, data.Body...)
	session.TaskStack = append(session.TaskStack, Task{Op: OpForScopeEnter})
}

func (e *Executor) switchCaseTasks(plan *SwitchData, tag *Var, body []Task, scopeName string) []Task {
	out := make([]Task, 0, len(body)+4)
	if plan.IsType {
		out = append(out, Task{Op: OpScopeExit})
	}
	out = append(out, body...)
	if plan.HasAssign {
		out = append(out, Task{Op: OpAssign})
		out = append(out, Task{Op: OpPush, Data: tag})
		out = append(out, plan.AssignLHS...)
	}
	if plan.IsType {
		out = append(out, Task{Op: OpScopeEnter, Data: scopeName})
	}
	return out
}

func (e *Executor) switchTypeCaseMatches(tag *Var, targets []RuntimeType) bool {
	tag = e.unwrapValue(tag)
	for _, targetType := range targets {
		if targetType.IsEmpty() {
			continue
		}
		if tag == nil || (tag.VType == TypeAny && tag.Ref == nil) {
			raw := targetType.Raw.Ast()
			if raw == "nil" || raw == ast.TypeAny {
				return true
			}
			continue
		}

		switch targetType.Raw {
		case "Int64":
			if tag.VType == TypeInt {
				return true
			}
		case "Float64":
			if tag.VType == TypeFloat {
				return true
			}
		case "String":
			if tag.VType == TypeString {
				return true
			}
		case "Bool":
			if tag.VType == TypeBool {
				return true
			}
		case "TypeBytes":
			if tag.VType == TypeBytes {
				return true
			}
		case "Any":
			if tag != nil {
				return true
			}
		case "Error":
			if tag.VType == TypeError {
				return true
			}
		}

		if targetType.IsInterface() {
			if _, err := e.CheckSatisfaction(tag, targetType.Raw.String()); err == nil {
				return true
			}
			continue
		}
		if _, ok := e.resolveInterfaceSpec(targetType.Raw); ok {
			if _, err := e.CheckSatisfaction(tag, targetType.Raw.String()); err == nil {
				return true
			}
		}
	}
	return false
}

func (e *Executor) unwrapValue(v *Var) *Var {
	for v != nil {
		switch v.VType {
		case TypeCell:
			v = v.Ref.(*Cell).Value
		case TypeAny:
			if inner, ok := v.Ref.(*Var); ok {
				v = inner
			} else if m, ok := v.Ref.(*VMMap); ok {
				out := &Var{VType: TypeMap, Ref: m}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else if arr, ok := v.Ref.(*VMArray); ok {
				out := &Var{VType: TypeArray, Ref: arr}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else if mod, ok := v.Ref.(*VMModule); ok {
				out := &Var{VType: TypeModule, Ref: mod}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else if inter, ok := v.Ref.(*VMInterface); ok {
				out := &Var{VType: TypeInterface, Ref: inter}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else if errObj, ok := v.Ref.(*VMError); ok {
				out := &Var{VType: TypeError, Ref: errObj}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else {
				return v
			}
		default:
			return v
		}
	}
	return nil
}

func (e *Executor) vmPointerTarget(v *Var) (*Var, bool) {
	if v == nil || v.VType != TypeHandle || v.Ref == nil || v.Bridge != nil {
		return nil, false
	}
	target, ok := v.Ref.(*Var)
	if !ok {
		return nil, false
	}
	return target, true
}

func (e *Executor) isVMPointer(v *Var) bool {
	_, ok := e.vmPointerTarget(v)
	return ok
}

func (e *Executor) isOpaqueHandle(v *Var) bool {
	if v == nil || v.VType != TypeHandle {
		return false
	}
	if e.isVMPointer(v) {
		return false
	}
	return v.Bridge != nil || v.Handle != 0
}

func (e *Executor) normalizeTypedValue(v *Var, targetType RuntimeType) *Var {
	v = e.unwrapValue(v)
	if v == nil {
		return nil
	}
	runtimeType := v.RuntimeType()
	if runtimeType.IsEmpty() || runtimeType.IsAny() {
		v.SetRuntimeType(targetType)
	}
	return v
}

func (e *Executor) unwrapAddressVar(v *Var) *Var {
	return e.unwrapValue(v)
}

func (e *Executor) dereferenceValue(v *Var) (*Var, error) {
	v = e.unwrapValue(v)
	if v == nil {
		return nil, errors.New("dereference of nil pointer")
	}
	target, ok := e.vmPointerTarget(v)
	if !ok {
		return nil, &VMError{Message: fmt.Sprintf("cannot dereference type %v", v.VType), IsPanic: true}
	}
	return e.unwrapValue(target), nil
}

type resolvedAddress struct {
	load  func() (*Var, error)
	store func(*Var) error
}

func (e *Executor) resolveAddress(session *StackContext, lhs LHSValue) (*resolvedAddress, error) {
	switch desc := lhs.(type) {
	case nil:
		return &resolvedAddress{
			load: func() (*Var, error) { return nil, nil },
			store: func(*Var) error {
				return nil
			},
		}, nil
	case *LHSEnv:
		if desc.Sym.Kind != SymbolUnknown {
			return &resolvedAddress{
				load: func() (*Var, error) {
					return session.LoadSymbol(desc.Sym)
				},
				store: func(val *Var) error {
					return session.StoreSymbol(desc.Sym, val)
				},
			}, nil
		}
		return &resolvedAddress{
			load: func() (*Var, error) {
				return session.Load(desc.Name)
			},
			store: func(val *Var) error {
				return session.Store(desc.Name, val)
			},
		}, nil
	case *LHSIndex:
		obj := e.unwrapAddressVar(desc.Obj)
		idx := e.unwrapAddressVar(desc.Index)
		if obj == nil || idx == nil {
			return nil, errors.New("index access on nil")
		}
		switch obj.VType {
		case TypeArray:
			arr := obj.Ref.(*VMArray)
			i := int(idx.I64)
			if _, ok := arr.Load(i); !ok {
				return nil, &VMError{Message: fmt.Sprintf("index out of range: %d", i), IsPanic: true}
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					v, _ := arr.Load(i)
					return e.unwrapAddressVar(v), nil
				},
				store: func(val *Var) error {
					arr.Store(i, val)
					return nil
				},
			}, nil
		case TypeMap:
			m := obj.Ref.(*VMMap)
			key, err := e.varToMapKey(idx)
			if err != nil {
				return nil, err
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					v, _ := m.Load(key)
					return e.unwrapAddressVar(v), nil
				},
				store: func(val *Var) error {
					m.Store(key, val)
					return nil
				},
			}, nil
		}
		return nil, fmt.Errorf("type %v does not support index access", obj.VType)
	case *LHSMember:
		obj := e.unwrapAddressVar(desc.Obj)
		if obj == nil {
			return nil, errors.New("member access on nil object")
		}
		switch obj.VType {
		case TypeMap:
			m := obj.Ref.(*VMMap)
			return &resolvedAddress{
				load: func() (*Var, error) {
					v, _ := m.Load(desc.Property)
					return e.unwrapAddressVar(v), nil
				},
				store: func(val *Var) error {
					m.Store(desc.Property, val)
					return nil
				},
			}, nil
		case TypeModule:
			mod := obj.Ref.(*VMModule)
			if mod.Context == nil {
				return nil, &VMError{Message: fmt.Sprintf("module %s is read-only", mod.Name), IsPanic: true}
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					return mod.Context.Load(desc.Property)
				},
				store: func(val *Var) error {
					return mod.Context.Store(desc.Property, val)
				},
			}, nil
		case TypeHandle:
			if obj.Ref == nil {
				return nil, errors.New("member access on nil pointer")
			}
			ref, ok := e.vmPointerTarget(obj)
			if !ok {
				return nil, errors.New("type Handle does not support member access")
			}
			return e.resolveAddress(session, &LHSMember{Obj: ref, Property: desc.Property})
		}
		return nil, fmt.Errorf("type %v does not support member access", obj.VType)
	case *LHSDeref:
		target := e.unwrapAddressVar(desc.Target)
		if target == nil {
			return nil, errors.New("dereference of nil pointer")
		}
		if !e.isVMPointer(target) {
			return nil, fmt.Errorf("type %v does not support dereference", target.VType)
		}
		return &resolvedAddress{
			load: func() (*Var, error) {
				return e.dereferenceValue(target)
			},
			store: func(val *Var) error {
				ref, _ := e.vmPointerTarget(target)
				copyVarData(ref, val)
				return nil
			},
		}, nil
	case *LHSSlice:
		obj := e.unwrapAddressVar(desc.Obj)
		if obj == nil {
			return nil, errors.New("slice access on nil object")
		}
		low, high, err := e.resolveSliceBoundsForAddress(obj, desc.Low, desc.High)
		if err != nil {
			return nil, err
		}
		switch obj.VType {
		case TypeBytes:
			return &resolvedAddress{
				load: func() (*Var, error) {
					return NewBytes(obj.B[low:high]), nil
				},
				store: func(val *Var) error {
					if val == nil || val.VType != TypeBytes {
						return fmt.Errorf("slice copy-back expects TypeBytes, got %v", valueTypeOf(val))
					}
					obj.B = spliceByteWindow(obj.B, low, high, val.B)
					return nil
				},
			}, nil
		case TypeArray:
			arr := obj.Ref.(*VMArray)
			return &resolvedAddress{
				load: func() (*Var, error) {
					v := &Var{VType: TypeArray, Ref: &VMArray{Data: arr.Slice(low, high)}}
					v.SetRuntimeType(obj.RuntimeType())
					return v, nil
				},
				store: func(val *Var) error {
					if val == nil || val.VType != TypeArray {
						return fmt.Errorf("slice copy-back expects Array, got %v", valueTypeOf(val))
					}
					items := val.Ref.(*VMArray).Snapshot()
					if !arr.ReplaceSlice(low, high, items) {
						return fmt.Errorf("slice bounds out of range [%d:%d]", low, high)
					}
					return nil
				},
			}, nil
		}
		return nil, fmt.Errorf("type %v does not support slice access", obj.VType)
	}
	return nil, &VMError{Message: fmt.Sprintf("unsupported LHS descriptor: %T", lhs), IsPanic: true}
}

func (e *Executor) resolveSliceBoundsForAddress(obj, lowVar, highVar *Var) (int, int, error) {
	low, high := 0, -1
	if lowVar != nil {
		if lowVar.VType != TypeInt {
			return 0, 0, fmt.Errorf("slice low index must be Int64, got %v", lowVar.VType)
		}
		low = int(lowVar.I64)
	}
	if highVar != nil {
		if highVar.VType != TypeInt {
			return 0, 0, fmt.Errorf("slice high index must be Int64, got %v", highVar.VType)
		}
		high = int(highVar.I64)
	}
	var length int
	switch obj.VType {
	case TypeBytes:
		length = len(obj.B)
	case TypeArray:
		length = obj.Ref.(*VMArray).Len()
	default:
		return 0, 0, fmt.Errorf("type %v does not support slice access", obj.VType)
	}
	if high == -1 {
		high = length
	}
	if low < 0 || high < low || high > length {
		return 0, 0, &VMError{Message: fmt.Sprintf("slice bounds out of range [%d:%d] with capacity %d", low, high, length), IsPanic: true}
	}
	return low, high, nil
}

func spliceByteWindow(base []byte, low, high int, replacement []byte) []byte {
	next := make([]byte, 0, low+len(replacement)+len(base)-high)
	next = append(next, base[:low]...)
	next = append(next, replacement...)
	next = append(next, base[high:]...)
	return next
}

func valueTypeOf(v *Var) string {
	if v == nil {
		return "<nil>"
	}
	return v.VType.String()
}

func (e *Executor) loadAddress(session *StackContext, lhs LHSValue) (*Var, error) {
	addr, err := e.resolveAddress(session, lhs)
	if err != nil {
		return nil, err
	}
	return addr.load()
}

func (e *Executor) storeAddress(session *StackContext, lhs LHSValue, val *Var) error {
	addr, err := e.resolveAddress(session, lhs)
	if err != nil {
		return err
	}
	return addr.store(val)
}

func (e *Executor) assignAddress(session *StackContext, lhs LHSValue, val *Var) error {
	return e.storeAddress(session, lhs, val)
}

func (e *Executor) updateAddress(session *StackContext, lhs LHSValue, op string) error {
	current, err := e.loadAddress(session, lhs)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	next := current.Copy()
	if op == "++" {
		next.I64++
	} else {
		next.I64--
	}
	return e.storeAddress(session, lhs, next)
}

func (e *Executor) Run(session *StackContext) error {
	_, err := e.runSession(session, 0)
	return err
}

func (e *Executor) runFibers(ctx context.Context, root *VMFiber) error {
	if e.scheduler == nil || root == nil {
		return errors.New("invalid fiber scheduler")
	}
	for {
		fiber, err := e.scheduler.Next(ctx)
		if err != nil {
			return e.abortRun(err)
		}
		if fiber == nil {
			return nil
		}
		frame := fiber.CurrentFrame()
		if frame == nil {
			e.scheduler.FinishCurrent()
			continue
		}
		stop, err := frame.Executor.runSession(frame.Session, fiberInstructionQuantum)
		if err != nil {
			return e.abortRun(err)
		}
		switch stop {
		case runStopDone:
			doneFrame := fiber.PopFrame()
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
			if len(fiber.Frames) == 0 {
				if fiber.ID != root.ID {
					e.scheduler.FinishCurrent()
					continue
				}
				e.scheduler.Stop()
				return nil
			}
			e.scheduler.EnqueueFiber(fiber)
			e.scheduler.FinishCurrent()
		case runStopYield, runStopSuspend:
			// The scheduler method already parked the current fiber.
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
	fibers := e.scheduler.AbortAll()
	fibers = e.cancelModuleLoadsForFibers(fibers)
	result := e.unwindFiberErrors(fibers, cause)
	if result == nil {
		return cause
	}
	return result
}

func (e *Executor) cancelModuleLoadsForFibers(fibers []*VMFiber) []*VMFiber {
	seen := make(map[*SharedState]bool)
	for i := 0; i < len(fibers); i++ {
		fiber := fibers[i]
		if fiber == nil {
			continue
		}
		for _, frame := range fiber.Frames {
			if frame == nil || frame.Session == nil || frame.Session.Shared == nil {
				continue
			}
			shared := frame.Session.Shared
			if seen[shared] {
				continue
			}
			seen[shared] = true
			for _, waiter := range shared.CancelModuleLoads() {
				if waiter.Fiber != nil {
					fibers = append(fibers, waiter.Fiber)
				}
			}
		}
	}
	return fibers
}

func (e *Executor) unwindFiberErrors(fibers []*VMFiber, cause error) error {
	result := cause
	replaced := false
	seen := make(map[*VMFiber]bool)
	for _, fiber := range fibers {
		if fiber == nil || seen[fiber] {
			continue
		}
		seen[fiber] = true
		if err := e.unwindFiberError(fiber, cause); err != nil && !replaced {
			result = err
			replaced = true
		}
	}
	return result
}

func (e *Executor) unwindFiberError(fiber *VMFiber, cause error) error {
	err := cause
	for {
		frame := fiber.PopFrame()
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
					var fiberID uint32
					if e.scheduler != nil {
						if fiber := e.scheduler.Current(); fiber != nil {
							fiberID = fiber.ID
						}
					}
					session.Debugger.EventChan <- &debugger.Event{
						FiberID: fiberID,
						Loc: &ast.Position{
							F: task.Source.File,
							L: task.Source.Line,
							C: task.Source.Col,
						},
						Variables: session.Stack.DumpVariables(),
					}
					cmd := <-session.Debugger.CommandChan
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
			if errors.Is(err, errFiberYield) {
				return runStopYield, nil
			}
			if errors.Is(err, errFiberSuspend) {
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

func (e *Executor) Disassemble() (res string) {
	defer func() {
		if r := recover(); r != nil {
			res = fmt.Sprintf("; Disassembly failed: %v\n", r)
		}
	}()

	var sb strings.Builder
	sb.WriteString("; Go-Mini VM Disassembly\n")
	fmt.Fprintf(&sb, "; Total Variables: %d\n", len(e.globals))
	fmt.Fprintf(&sb, "; Total Functions: %d\n\n", len(e.functions))

	sb.WriteString("section .data:\n")
	globalKeys := make([]string, 0, len(e.globals))
	for name := range e.globals {
		globalKeys = append(globalKeys, name)
	}
	sort.Strings(globalKeys)
	for _, key := range globalKeys {
		global := e.globals[key]
		fmt.Fprintf(&sb, "  global %s\n", key)
		fmt.Fprintf(&sb, "global.%s:\n", key)
		if global != nil {
			e.disassembleTasks(&sb, "  ", global.InitPlan)
		}
	}
	sb.WriteString("\n")

	sb.WriteString("section .text:\n")
	if len(e.mainTasks) > 0 {
		sb.WriteString("global _start\n")
		sb.WriteString("_start:\n")
		e.disassembleTasks(&sb, "  ", e.mainTasks)
		sb.WriteString("\n")
	}

	keys := make([]string, 0, len(e.functions))
	for k := range e.functions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		f := e.functions[k]
		sig := "function()"
		if f != nil && f.FunctionSig != nil {
			sig = string(f.FunctionSig.Spec)
		}
		fmt.Fprintf(&sb, "fn.%s: ; signature %s\n", k, sig)
		e.disassembleTasks(&sb, "  ", f.BodyTasks)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (e *Executor) disassembleTasks(sb *strings.Builder, indent string, tasks []Task) {
	defer func() {
		if r := recover(); r != nil {
			sb.WriteString(indent + "; Disassembly failed for this task plan: " + fmt.Sprintf("%v", r) + "\n")
		}
	}()

	if len(tasks) == 0 {
		return
	}

	queue := cloneTasks(tasks)

	for len(queue) > 0 {
		task := queue[len(queue)-1]
		queue = queue[:len(queue)-1]

		dataStr := ""
		if task.Data != nil {
			if v, ok := task.Data.(*Var); ok && v != nil {
				if v.VType == TypeString {
					dataStr = fmt.Sprintf("%q", v.Str)
				} else {
					dataStr = v.String()
				}
			} else if env, ok := task.Data.(*LHSEnv); ok {
				dataStr = env.Name
			} else if mem, ok := task.Data.(*LHSMember); ok {
				objStr := "nil"
				if mem.Obj != nil {
					objStr = mem.Obj.String()
				}
				dataStr = fmt.Sprintf("%v.%v", objStr, mem.Property)
			} else if _, ok := task.Data.(*LHSIndex); ok {
				dataStr = "[]"
			} else if ld, ok := task.Data.(*LHSData); ok {
				switch ld.Kind {
				case LHSTypeEnv:
					dataStr = ld.Name
				case LHSTypeMember:
					dataStr = ld.Property
				case LHSTypeIndex:
					dataStr = "[]"
				case LHSTypeStar:
					dataStr = "*"
				}
			} else if cd, ok := task.Data.(*CallData); ok {
				dataStr = cd.Name
			} else if ld, ok := task.Data.(*LoadVarData); ok {
				dataStr = ld.Name
			} else if sym, ok := task.Data.(SymbolRef); ok {
				dataStr = fmt.Sprintf("%s[%d]", sym.Name, sym.Slot)
			} else {
				dataStr = fmt.Sprintf("%v", task.Data)
			}
		}
		// 兜底：强制替换任何可能残留的真实换行符，防止破坏对齐
		dataStr = strings.ReplaceAll(dataStr, "\n", "\\n")
		dataStr = strings.ReplaceAll(dataStr, "\r", "\\r")

		addr := "[                ]"
		comment := ""
		if task.Source != nil {
			addr = fmt.Sprintf("[%16s]", task.Source.ID)
			comment = task.Source.Meta

			if task.Source.Line > 0 {
				comment = fmt.Sprintf("%s at L%d:%d", comment, task.Source.Line, task.Source.Col)
			}
		}

		// Provide more semantic context for common instructions based on Data
		switch task.Op {
		case OpCall:
			if cd, ok := task.Data.(*CallData); ok {
				comment = "Call " + cd.Name
			}
		case OpLoadLocal, OpLoadUpvalue:
			if sym, ok := task.Data.(SymbolRef); ok {
				comment = fmt.Sprintf("Load %s slot %d", sym.Name, sym.Slot)
			}
		case OpStoreLocal, OpStoreUpvalue:
			if sym, ok := task.Data.(SymbolRef); ok {
				comment = fmt.Sprintf("Store %s slot %d", sym.Name, sym.Slot)
			}
		case OpAssign:
			comment = "Assignment"
		case OpReturn:
			comment = fmt.Sprintf("Return %v values", task.Data)
		case OpJumpIf:
			if jd, ok := task.Data.(*JumpData); ok {
				comment = fmt.Sprintf("Short-circuit %v", jd.Operator)
			}
		case OpPush:
			comment = "Literal value"
		case OpLoadVar:
			switch data := task.Data.(type) {
			case *LoadVarData:
				comment = fmt.Sprintf("Load variable '%v'", data.Name)
			default:
				comment = fmt.Sprintf("Load variable '%v'", task.Data)
			}
		case OpPop:
			comment = "Pop stack"
		}

		// NASM 风格对齐: ADDRESS  INSTRUCTION  OPERANDS  ; COMMENT
		line := fmt.Sprintf("%s  %-18s %-22s", addr, task.Op.String(), dataStr)
		if comment != "" {
			line = fmt.Sprintf("%-65s ; %s", line, comment)
		}
		sb.WriteString(indent + line + "\n")
	}
}

func (e *Executor) buildImportedModuleValue(path string, modExec *Executor, modSession *StackContext) *Var {
	exports := make(map[string]*Var)
	for name := range modExec.globals {
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			v, err := modSession.Load(name)
			if err == nil {
				exports[name] = v
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
	frame := &FiberFrame{
		Executor: modExecutor,
		Session:  modSession,
		Kind:     FrameModuleInit,
	}
	frame.OnDone = func(done *FiberFrame) error {
		parent.StepCount = done.Session.StepCount
		res := e.buildImportedModuleValue(path, modExecutor, done.Session)
		waiters := parent.Shared.FinishModuleLoad(path, res)
		parent.ValueStack.Push(res)
		e.scheduleModuleWaiters(waiters, res, nil)
		return nil
	}
	frame.OnError = func(done *FiberFrame, loadErr error) error {
		parent.StepCount = done.Session.StepCount
		waiters := parent.Shared.FinishModuleLoad(path, nil)
		e.scheduleModuleWaiters(waiters, nil, loadErr)
		return loadErr
	}
	if err := e.scheduler.PushFrame(frame); err != nil {
		return err
	}
	if err := e.scheduler.YieldCurrent(); err != nil {
		return err
	}
	return errFiberYield
}

func (e *Executor) scheduleModuleWaiters(waiters []moduleWaiter, value *Var, err error) {
	if len(waiters) == 0 || e.scheduler == nil {
		return
	}
	for _, waiter := range waiters {
		if waiter.Fiber == nil || waiter.Frame == nil {
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
		e.scheduler.EnqueueFiber(waiter.Fiber)
	}
}

func (e *Executor) ExecuteStmts(session *StackContext, stmts []ast.Stmt) error {
	oldTasks := session.TaskStack
	oldValues := session.ValueStack
	oldLHS := session.LHSStack
	oldUnwind := session.UnwindMode

	session.TaskStack = []Task{}
	session.ValueStack = &ValueStack{}
	session.LHSStack = &LHSStack{}
	session.UnwindMode = UnwindNone

	for i := len(stmts) - 1; i >= 0; i-- {
		session.TaskStack = append(session.TaskStack, e.tasksForStmt(stmts[i], nil)...)
	}

	var err error
	if e.scheduler != nil && e.scheduler.Current() == nil {
		e.runMu.Lock()
		root, resetErr := e.scheduler.Reset(session, e)
		if resetErr != nil {
			e.runMu.Unlock()
			err = resetErr
		} else {
			err = e.runFibers(session.Context, root)
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

func (e *Executor) ImportModule(ctx *StackContext, n *ast.ImportExpr) (*Var, error) {
	oldTasks := ctx.TaskStack
	oldValues := ctx.ValueStack
	oldLHS := ctx.LHSStack
	oldUnwind := ctx.UnwindMode

	ctx.TaskStack = []Task{{Op: OpImportInit, Data: &ImportInitData{Path: n.Path}}}
	ctx.ValueStack = &ValueStack{}
	ctx.LHSStack = &LHSStack{}
	ctx.UnwindMode = UnwindNone

	var err error
	if e.scheduler == nil {
		e.scheduler = NewFiberScheduler()
	}
	if e.scheduler.Current() == nil {
		e.runMu.Lock()
		root, resetErr := e.scheduler.Reset(ctx, e)
		if resetErr != nil {
			err = resetErr
		} else {
			err = e.runFibers(ctx.Context, root)
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
