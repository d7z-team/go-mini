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
	globals         map[ast.Ident]*RuntimeGlobal
	functions       map[ast.Ident]*RuntimeFunction
	mainTasks       []Task
	program         *ast.ProgramStmt
	globalInitOrder []ast.Ident

	routes map[string]FFIRoute

	ModuleLoader     func(path string) (*ast.ProgramStmt, error)
	ModulePlanLoader func(path string) (*ast.ProgramStmt, *PreparedProgram, error)

	StepLimit int64

	interfaceCache map[ast.GoMiniType]*RuntimeInterfaceSpec
	mu             sync.RWMutex
	lastSession    *StackContext
}

type RuntimeGlobal struct {
	Name     ast.Ident
	HasInit  bool
	InitExpr ast.Expr
	InitPlan []Task
}

type RuntimeFunction struct {
	Name         ast.Ident
	FunctionType ast.FunctionType
	BodyTasks    []Task
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

func (r *runtimeMetadataRegistry) resolveNamedType(typ ast.GoMiniType) (RuntimeType, bool) {
	if typeInfo, ok := r.namedTypesByName[string(typ)]; ok {
		return typeInfo, true
	}
	typeInfo, ok := r.namedTypesByTypeID[CanonicalTypeID(string(typ))]
	return typeInfo, ok
}

func (r *runtimeMetadataRegistry) resolveInterfaceSpec(typ ast.GoMiniType) (*RuntimeInterfaceSpec, bool) {
	if spec, ok := r.interfacesByName[string(typ)]; ok {
		return spec, true
	}
	spec, ok := r.interfacesByTypeID[CanonicalTypeID(string(typ))]
	return spec, ok
}

func (r *runtimeMetadataRegistry) resolveStructSchema(typ ast.GoMiniType) (*RuntimeStructSpec, bool) {
	if schema, ok := r.structsByName[string(typ)]; ok {
		return schema, true
	}
	schema, ok := r.structsByTypeID[CanonicalTypeID(string(typ))]
	return schema, ok
}

func (e *Executor) LastSession() *StackContext {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastSession
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

func (e *Executor) SetLastSession(session *StackContext) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastSession = session
}

func NewExecutorFromPrepared(program *ast.ProgramStmt, prepared *PreparedProgram) (*Executor, error) {
	if program == nil {
		return nil, errors.New("invalid program")
	}
	if prepared == nil {
		return nil, errors.New("missing prepared program")
	}
	globalInitOrder, err := program.GlobalInitOrder()
	if err != nil {
		globalInitOrder = program.DeclaredGlobalOrder()
	}
	result := &Executor{
		program:         program,
		globalInitOrder: globalInitOrder,
		metadata:        newRuntimeMetadataRegistry(),
		globals:         make(map[ast.Ident]*RuntimeGlobal),
		functions:       make(map[ast.Ident]*RuntimeFunction),
		consts:          make(map[string]string),
		routes:          make(map[string]FFIRoute),
		interfaceCache:  make(map[ast.GoMiniType]*RuntimeInterfaceSpec),
	}
	if program.Structs != nil {
		for ident, stmt := range program.Structs {
			spec := runtimeStructSpecFromStmt(stmt)
			if spec != nil {
				result.metadata.registerStructSchema(string(ident), spec)
			}
		}
	}
	if program.Interfaces != nil {
		for ident, stmt := range program.Interfaces {
			spec, err := ParseRuntimeInterfaceSpec(stmt.Type)
			if err == nil && spec != nil {
				result.metadata.registerInterfaceSpec(string(ident), spec)
			}
		}
	}
	if program.Types != nil {
		for ident, t := range program.Types {
			typeInfo, err := ParseRuntimeType(t)
			if err == nil {
				result.metadata.registerNamedType(string(ident), typeInfo)
			}
		}
	}
	for s, s2 := range program.Constants {
		result.consts[s] = s2
	}
	if program.Variables != nil {
		for ident, expr := range program.Variables {
			result.globals[ident] = &RuntimeGlobal{
				Name:     ident,
				HasInit:  expr != nil,
				InitExpr: expr,
			}
		}
	}
	if program.Functions != nil {
		for ident, fn := range program.Functions {
			if fn == nil {
				continue
			}
			result.functions[ident] = &RuntimeFunction{
				Name:         ident,
				FunctionType: fn.FunctionType,
			}
		}
	}
	result.applyPreparedProgram(prepared)
	return result, nil
}

func (e *Executor) applyPreparedProgram(prepared *PreparedProgram) {
	prepared = clonePreparedProgram(prepared)
	if prepared == nil {
		return
	}
	e.globalInitOrder = append([]ast.Ident(nil), prepared.GlobalInitOrder...)
	for ident, global := range prepared.Globals {
		rg, ok := e.globals[ident]
		if !ok || rg == nil {
			rg = &RuntimeGlobal{Name: ident}
		}
		if global != nil {
			rg.HasInit = global.HasInit
			rg.InitPlan = cloneTasks(global.InitPlan)
		}
		e.globals[ident] = rg
	}
	for ident, fn := range prepared.Functions {
		rf, ok := e.functions[ident]
		if !ok || rf == nil {
			rf = &RuntimeFunction{Name: ident}
		}
		if fn != nil {
			rf.FunctionType = fn.FunctionType
			rf.BodyTasks = cloneTasks(fn.BodyTasks)
		}
		e.functions[ident] = rf
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
			Type:     fieldType,
			TypeInfo: typeInfo,
		}
		fields = append(fields, field)
		byName[field.Name] = field
	}
	typeInfo := RuntimeType{
		Kind:   RuntimeTypeStruct,
		Raw:    ast.GoMiniType(stmt.Name),
		TypeID: CanonicalTypeID(string(stmt.Name)),
		Fields: fields,
	}
	return &RuntimeStructSpec{
		Name:     string(stmt.Name),
		TypeID:   CanonicalTypeID(string(stmt.Name)),
		Spec:     ast.GoMiniType(stmt.Name),
		TypeInfo: typeInfo,
		Layout:   buildStructLayout(fields),
		Fields:   fields,
		ByName:   byName,
	}
}

func cloneRuntimeStructSpec(spec *RuntimeStructSpec) *RuntimeStructSpec {
	if spec == nil {
		return nil
	}
	fields := append([]RuntimeStructField(nil), spec.Fields...)
	byName := make(map[string]RuntimeStructField, len(spec.ByName))
	for k, v := range spec.ByName {
		byName[k] = v
	}
	typeInfo := spec.TypeInfo
	typeInfo.Fields = append([]RuntimeStructField(nil), spec.TypeInfo.Fields...)
	return &RuntimeStructSpec{
		Name:     spec.Name,
		TypeID:   spec.TypeID,
		Spec:     spec.Spec,
		TypeInfo: typeInfo,
		Layout:   spec.Layout,
		Fields:   fields,
		ByName:   byName,
	}
}

func cloneRuntimeInterfaceSpec(spec *RuntimeInterfaceSpec) *RuntimeInterfaceSpec {
	if spec == nil {
		return nil
	}
	methods := make([]RuntimeInterfaceMethod, len(spec.Methods))
	byName := make(map[string]*RuntimeFuncSig, len(spec.ByName))
	methodIndex := make(map[string]int, len(spec.MethodIndex))
	for i, method := range spec.Methods {
		methods[i] = RuntimeInterfaceMethod{
			Index: method.Index,
			Name:  method.Name,
			Spec:  cloneRuntimeFuncSig(method.Spec),
		}
	}
	for k, v := range spec.ByName {
		byName[k] = cloneRuntimeFuncSig(v)
	}
	for k, v := range spec.MethodIndex {
		methodIndex[k] = v
	}
	typeInfo := spec.TypeInfo
	typeInfo.Methods = append([]RuntimeInterfaceMethod(nil), spec.TypeInfo.Methods...)
	return &RuntimeInterfaceSpec{
		TypeID:      spec.TypeID,
		Spec:        spec.Spec,
		TypeInfo:    typeInfo,
		Methods:     methods,
		ByName:      byName,
		MethodIndex: methodIndex,
	}
}

func cloneRuntimeFuncSig(sig *RuntimeFuncSig) *RuntimeFuncSig {
	if sig == nil {
		return nil
	}
	res := *sig
	res.Function.Params = append([]ast.FunctionParam(nil), sig.Function.Params...)
	res.ParamTypes = append([]RuntimeType(nil), sig.ParamTypes...)
	res.ParamModes = append([]FFIParamMode(nil), sig.ParamModes...)
	return &res
}

func (e *Executor) resolveNamedType(typ ast.GoMiniType) (RuntimeType, bool) {
	return e.metadata.resolveNamedType(typ)
}

func (e *Executor) resolveNamedTypeChain(typ ast.GoMiniType) (RuntimeType, bool, error) {
	current := typ
	seen := map[ast.GoMiniType]struct{}{}
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

func (e *Executor) resolveInterfaceSpec(typ ast.GoMiniType) (*RuntimeInterfaceSpec, bool) {
	if typ.IsInterface() {
		return e.cachedInterfaceSpec(typ)
	}
	return e.metadata.resolveInterfaceSpec(typ)
}

func (e *Executor) cachedInterfaceSpec(typ ast.GoMiniType) (*RuntimeInterfaceSpec, bool) {
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

func (e *Executor) resolveStructSchema(typ ast.GoMiniType) (*RuntimeStructSpec, bool) {
	return e.metadata.resolveStructSchema(typ)
}

func (e *Executor) SetGlobalInitOrder(order []ast.Ident) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.globalInitOrder = append([]ast.Ident(nil), order...)
}

func cloneTasks(tasks []Task) []Task {
	if len(tasks) == 0 {
		return nil
	}
	return append([]Task(nil), tasks...)
}

func (e *Executor) buildStmtPlan(stmts []ast.Stmt) []Task {
	return e.buildStmtPlanWithScope(stmts, e.newRootLoweringScope())
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
	fn, ok := e.functions[ast.Ident(name)]
	return fn, ok
}

func (e *Executor) lookupGlobal(name ast.Ident) (*RuntimeGlobal, bool) {
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

	err := e.Run(ctx)
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
	e.metadata.registerInterfaceSpec(name, cloneRuntimeInterfaceSpec(spec))
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
		Context:        ctx,
		Executor:       e,
		Stack:          &Stack{MemoryPtr: make(map[string]*Var), Globals: make(map[string]*Var), Frame: &SlotFrame{}, Scope: scope, Depth: 1},
		ModuleCache:    make(map[string]*Var),
		LoadingModules: make(map[string]bool),
		Debugger:       debugger.GetDebugger(ctx),
		TaskStack:      make([]Task, 0, 128),
		ValueStack:     &ValueStack{},
		LHSStack:       &LHSStack{},
		UnwindMode:     UnwindNone,
		resumeSignal:   make(chan struct{}, 1),
	}

	// Setup Context Bridge (Abort logic)
	if ctx != nil && ctx.Done() != nil {
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				session.Abort()
			case <-done:
			}
		}()
		session.Stack.AddDefer(func() { close(done) })
	}

	return session
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

func (e *Executor) ExecExpr(ctx *StackContext, s ast.Expr) (*Var, error) {
	return e.runExprPlan(ctx, e.tasksForExpr(s))
}

func (e *Executor) CheckSatisfaction(val *Var, interfaceType ast.GoMiniType) (*Var, error) {
	if val == nil {
		return nil, errors.New("cannot assign nil to interface")
	}

	// 1. Exact match (handles named types and primitives directly)
	if val.Type.Equals(interfaceType) {
		return val.Copy(), nil
	}

	// 2. Any penetration
	inner := e.unwrapValue(val)
	if inner == nil {
		inner = val
	}
	if inner.Type.Equals(interfaceType) {
		return inner.Copy(), nil
	}

	actualInterfaceType := interfaceType
	if !interfaceType.IsInterface() {
		// 3. Resolve named type ONLY if it could be an interface or struct
		if actual, ok := e.resolveNamedType(interfaceType); ok {
			if actual.Kind == RuntimeTypeInterface {
				return e.CheckSatisfaction(val, actual.Raw)
			}
		}

		// 4. Resolve named interface
		if spec, ok := e.resolveInterfaceSpec(interfaceType); ok {
			actualInterfaceType = spec.Spec
		} else {
			// If it wasn't an exact match and isn't an interface, it fails (like Go)
			return nil, fmt.Errorf("interface conversion: interface is %s, not %s", inner.Type, interfaceType)
		}
	}

	spec, ok := e.cachedInterfaceSpec(actualInterfaceType)
	if !ok {
		return nil, fmt.Errorf("invalid interface type: %s", actualInterfaceType)
	}

	vtable := make([]*Var, len(spec.Methods))
	for _, method := range spec.Methods {
		if method.Spec == nil {
			return nil, fmt.Errorf("type %v does not implement %s: missing method schema %s", inner.VType, interfaceType, method.Name)
		}
		callable, ok := e.resolveMethodValue(inner, method.Name)
		if !ok || !e.isCallableCompatible(callable, &method.Spec.Function) {
			return nil, fmt.Errorf("type %v does not implement %s: missing or incompatible method %s", inner.VType, interfaceType, method.Name)
		}
		vtable[method.Index] = callable
	}

	return &Var{
		Type:  interfaceType,
		VType: TypeInterface,
		Ref: &VMInterface{
			Target: inner.Copy(),
			Spec:   spec,
			VTable: vtable,
		},
	}, nil
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
		if methodName, ok := e.resolveMethodRoute(string(val.Type), name); ok {
			return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: val, Method: methodName}}, true
		}
	case TypeMap:
		if m, ok := val.Ref.(*VMMap); ok {
			if v, ok := m.Data[name]; ok {
				return v, true
			}
		}
		tName := string(val.Type)
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
			if v := mod.Data[name]; v != nil {
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

func (e *Executor) hasMethodWithSignature(val *Var, name string, expectedSig *ast.FunctionType) bool {
	callable, ok := e.resolveMethodValue(val, name)
	return ok && e.isCallableCompatible(callable, expectedSig)
}

func (e *Executor) isCallableCompatible(v *Var, expectedSig *ast.FunctionType) bool {
	v = e.unwrapValue(v)
	if v == nil {
		return false
	}
	if v.VType == TypeClosure {
		if cl, ok := v.Ref.(*VMClosure); ok {
			return e.isSignatureCompatible(&cl.FunctionType, expectedSig)
		}
	}
	if route, ok := v.Ref.(FFIRoute); ok {
		if route.FuncSig != nil {
			return e.isSignatureCompatible(&route.FuncSig.Function, expectedSig)
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
	session := e.NewSession(ctx, "global")
	session.StepLimit = e.StepLimit

	e.mu.Lock()
	e.lastSession = session
	e.mu.Unlock()

	defer e.CleanupSession(session)

	return e.InitializeSession(session, env, true)
}

func (e *Executor) InitializeSession(session *StackContext, env map[string]*Var, invokeMain bool) (err error) {
	if session == nil {
		return errors.New("invalid session")
	}

	// 注入脚本定义的全局变量
	for _, name := range e.globalInitOrder {
		global, ok := e.lookupGlobal(name)
		if !ok {
			continue
		}
		var val *Var
		if global.HasInit {
			v, err := e.runExprPlan(session, global.InitPlan)
			if err != nil {
				return fmt.Errorf("failed to initialize global var %s: %w", name, err)
			}
			val = v
		} else {
			// 如果没有初值，则初始化为零值变量 (Any 类型)
			val = &Var{VType: TypeAny, Type: "Any"}
		}
		// 确保变量已存储到内存中 (直接操作内存字典以避开 AddVariable 的 Copy 逻辑，适合初始化)
		session.Stack.Globals[string(name)] = val
	}

	// 注入环境变量 (放到后面，允许覆盖脚本定义的同名全局变量)
	for k, v := range env {
		session.Stack.Globals[k] = v.Copy()
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

	// 压入执行入口任务: Main 块 (包初始化逻辑)
	session.TaskStack = append(session.TaskStack, cloneTasks(e.mainTasks)...)

	err = e.Run(session)
	if err != nil {
		return err
	}

	// 自动寻找并执行 main() 入口函数
	if invokeMain {
		if fn, ok := e.lookupFunction("main"); ok {
			session.TaskStack = append(session.TaskStack, Task{
				Op: OpCallBoundary,
				Data: &CallBoundaryData{
					Name:      "main",
					OldStack:  session.Stack,
					HasReturn: false,
					ValueBase: session.ValueStack.Len(),
					LHSBase:   session.LHSStack.Len(),
				},
			})
			session.TaskStack = append(session.TaskStack, Task{Op: OpDoCall, Data: &DoCallData{
				Name:         string(fn.Name),
				FunctionType: fn.FunctionType,
				BodyTasks:    cloneTasks(fn.BodyTasks),
			}})

			// Start run loop again for main func
			err = e.Run(session)
			session.Stack.RunDefers()
			if err != nil {
				return err
			}
		}
	}

	return err
}

// Unwind State Machine
func (e *Executor) handleUnwind(session *StackContext, task *Task) (bool, error) {
	if task.Op == OpScopeExit || task.Op == OpForScopeExit || task.Op == OpFinally {
		prevMode := session.UnwindMode
		session.UnwindMode = UnwindNone
		session.TaskStack = append(session.TaskStack, Task{Op: OpResumeUnwind, Data: prevMode})
		session.TaskStack = append(session.TaskStack, *task)
		return true, nil
	}

	if task.Op == OpRunDefers {
		if len(session.Stack.DeferStack) > 0 {
			prevMode := session.UnwindMode
			session.UnwindMode = UnwindNone
			session.TaskStack = append(session.TaskStack, Task{Op: OpResumeUnwind, Data: prevMode})

			defers := session.Stack.DeferStack
			session.Stack.DeferStack = nil
			for _, fn := range defers {
				fn()
			}
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
		delete(session.LoadingModules, data.Path)
		// Return true to indicate we handled this task, but keep UnwindMode as is to continue unwinding in parent
		return true, nil
	}

	if task.Op == OpCallBoundary {
		data, ok := task.Data.(*CallBoundaryData)
		if !ok || data == nil {
			return false, fmt.Errorf("OpCallBoundary data is not *CallBoundaryData: %T", task.Data)
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
			return true, nil
		}

		// If it's a panic, still restore the stack and continue unwinding
		session.Stack = oldStack
		if data.OldExec != nil {
			session.Executor = data.OldExec
		}
		return false, nil
	}

	if task.Op == OpLoopBoundary {
		if session.UnwindMode == UnwindBreak {
			session.UnwindMode = UnwindNone
			return true, nil
		}
		if session.UnwindMode == UnwindContinue {
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

func (e *Executor) mapKeyToVar(k string, keyType ast.GoMiniType) *Var {
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
			if _, ok := session.Stack.Globals[data.Name]; ok {
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
			if exec != nil && exec.program != nil {
				for _, imp := range exec.program.Imports {
					alias := imp.Alias
					if alias == "" {
						parts := strings.Split(imp.Path, "/")
						alias = parts[len(parts)-1]
					}
					if alias == name {
						if mod, ok := session.ModuleCache[imp.Path]; ok {
							session.ValueStack.Push(mod)
							return nil
						}
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
			rawElements := val.Ref.(*VMArray).Data
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
				if val, ok := m.Data[key]; ok {
					tuple[0] = val
					tuple[1] = NewBool(true)
				} else {
					_, valType, _ := obj.Type.GetMapKeyValueTypes()
					tuple[0] = e.ToVar(session, valType.ZeroVar(), nil)
					tuple[1] = NewBool(false)
				}
				session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: tuple}, Type: data.ResultType})
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
			typ     ast.GoMiniType
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
			session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: res}, Type: typ})
			return nil
		}

		res := make(map[string]*Var)
		var valType ast.GoMiniType
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
		session.ValueStack.Push(&Var{VType: TypeMap, Ref: &VMMap{Data: res}, Type: typ})
		return nil
	case OpSlice:
		var high, low, obj *Var
		hasLow := false
		hasHigh := false
		data := task.Data.(*SliceData)
		hasLow = data.HasLow
		hasHigh = data.HasHigh
		if hasHigh {
			high = session.ValueStack.Pop()
		}
		if hasLow {
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
				newArgs := make([]*Var, len(args)-1+len(arr.Data))
				copy(newArgs, args[:len(args)-1])
				copy(newArgs[len(args)-1:], arr.Data)
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
	case OpCallBoundary:
		data, ok := task.Data.(*CallBoundaryData)
		if !ok || data == nil {
			return fmt.Errorf("OpCallBoundary data is not *CallBoundaryData: %T (%v)", task.Data, task.Data)
		}
		oldStack := data.OldStack
		hasReturn := data.HasReturn
		valueBase := data.ValueBase
		lhsBase := data.LHSBase

		// Restore executor if saved (cross-module calls)
		if data.OldExec != nil {
			session.Executor = data.OldExec
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
			targetType ast.GoMiniType
			multi      bool
			resultType ast.GoMiniType
		)
		if data, ok := task.Data.(*AssertData); ok {
			targetType = data.TargetType
			multi = data.Multi
			resultType = data.ResultType
		} else {
			return errors.New("OpAssert missing AssertData")
		}
		res, err := e.CheckSatisfaction(val, targetType)
		if multi {
			if err != nil {
				// 返回 (nil, false)
				tuple := make([]*Var, 2)
				tuple[0] = nil
				tuple[1] = NewBool(false)
				session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: tuple}, Type: resultType})
			} else {
				// 返回 (res, true)
				tuple := make([]*Var, 2)
				tuple[0] = res
				tuple[1] = NewBool(true)
				session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: tuple}, Type: resultType})
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("interface conversion: %v", err)
		}
		session.ValueStack.Push(res)
		return nil
	case OpRunDefers:
		if len(session.Stack.DeferStack) > 0 {
			defers := session.Stack.DeferStack
			session.Stack.DeferStack = nil
			for _, fn := range defers {
				fn()
			}
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
			Key:    data.Key,
			Value:  data.Value,
			KeySym: data.KeySym,
			ValSym: data.ValSym,
			Define: data.Define,
			Body:   data.Body,
			Obj:    obj,
		}
		switch obj.VType {
		case TypeArray:
			rData.Length = len(obj.Ref.(*VMArray).Data)
		case TypeMap:
			m := obj.Ref.(*VMMap)
			rData.Keys = make([]string, 0, len(m.Data))
			for k := range m.Data {
				rData.Keys = append(rData.Keys, k)
			}
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
			val = rData.Obj.Ref.(*VMArray).Data[rData.Index]
		} else {
			k := rData.Keys[rData.Index]
			keyType, _, _ := rData.Obj.Type.GetMapKeyValueTypes()
			key = e.mapKeyToVar(k, keyType)
			val = rData.Obj.Ref.(*VMMap).Data[k]
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
					_ = session.DeclareSymbol(rData.KeySym, "Any")
					_ = session.StoreSymbol(rData.KeySym, key)
				} else {
					_ = session.AddVariable(rData.Key, key)
				}
			}
			if rData.Value != "" && rData.Value != "_" && val != nil {
				if rData.ValSym.Kind == SymbolLocal {
					_ = session.DeclareSymbol(rData.ValSym, "Any")
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
				_ = session.DeclareSymbol(varSym, "Any")
				_ = session.StoreSymbol(varSym, panicVar)
			} else {
				_ = session.NewVar(varName, "Any")
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
	case OpResumeUnwind:
		mode := task.Data.(UnwindMode)
		if session.UnwindMode == UnwindNone {
			if mode == UnwindPanic && session.PanicVar == nil {
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

		if v, ok := session.ModuleCache[path]; ok {
			session.ValueStack.Push(v)
			return nil
		}

		if session.LoadingModules[path] {
			return fmt.Errorf("circular dependency detected: %s", path)
		}

		if e.ModulePlanLoader != nil {
			prog, prepared, err := e.ModulePlanLoader(path)
			if err == nil {
				session.LoadingModules[path] = true
				res, err := e.executeImportedProgram(session, path, prog, prepared)
				delete(session.LoadingModules, path)
				if err != nil {
					return err
				}
				session.ModuleCache[path] = res
				session.ValueStack.Push(res)
				return nil
			}
		} else if e.ModuleLoader != nil {
			prog, err := e.ModuleLoader(path)
			if err == nil {
				session.LoadingModules[path] = true
				res, err := e.executeImportedProgram(session, path, prog, nil)
				delete(session.LoadingModules, path)
				if err != nil {
					return err
				}
				session.ModuleCache[path] = res
				session.ValueStack.Push(res)
				return nil
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
				ffiMod.Data[methodName] = &Var{
					VType: TypeAny,
					Str:   name,
					Ref:   route,
				}
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
				ffiMod.Data[constName] = e.evalLiteralToVar(val)
			}
		}

		if found {
			res := &Var{VType: TypeModule, Ref: ffiMod}
			session.ModuleCache[path] = res
			session.ValueStack.Push(res)
			return nil
		}
		return fmt.Errorf("failed to load module %s", path)

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
			Context:        session.Context,
			Executor:       session.Executor,
			Stack:          session.Stack,
			StepLimit:      session.StepLimit,
			ModuleCache:    session.ModuleCache,
			LoadingModules: session.LoadingModules,
			Debugger:       session.Debugger,
		}
		closure := &VMClosure{
			FunctionType: data.FunctionType,
			BodyTasks:    data.BodyTasks,
			UpvalueSlots: make([]*Var, len(data.CaptureRefs)),
			UpvalueNames: make([]string, len(data.CaptureRefs)),
			Context:      clCtx,
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

func (e *Executor) switchTypeCaseMatches(tag *Var, targets []ast.GoMiniType) bool {
	for _, targetType := range targets {
		if targetType == "" {
			continue
		}
		if tag == nil || (tag.VType == TypeAny && tag.Ref == nil) {
			if targetType == "nil" || targetType == "Any" || targetType == "interface{}" {
				return true
			}
			continue
		}

		switch targetType {
		case "Int64", "int", "int64":
			if tag.VType == TypeInt {
				return true
			}
		case "Float64", "float64":
			if tag.VType == TypeFloat {
				return true
			}
		case "String", "string":
			if tag.VType == TypeString {
				return true
			}
		case "Bool", "bool":
			if tag.VType == TypeBool {
				return true
			}
		case "TypeBytes", "bytes":
			if tag.VType == TypeBytes {
				return true
			}
		case "Any", "interface{}":
			if tag != nil {
				return true
			}
		case "Error", "error":
			if tag.VType == TypeError {
				return true
			}
		}

		if targetType.IsInterface() {
			if _, err := e.CheckSatisfaction(tag, targetType); err == nil {
				return true
			}
			continue
		}
		if _, ok := e.resolveInterfaceSpec(targetType); ok {
			if _, err := e.CheckSatisfaction(tag, targetType); err == nil {
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
				return &Var{VType: TypeMap, Ref: m, Type: v.Type}
			} else if arr, ok := v.Ref.(*VMArray); ok {
				return &Var{VType: TypeArray, Ref: arr, Type: v.Type}
			} else if mod, ok := v.Ref.(*VMModule); ok {
				return &Var{VType: TypeModule, Ref: mod, Type: v.Type}
			} else if inter, ok := v.Ref.(*VMInterface); ok {
				return &Var{VType: TypeInterface, Ref: inter, Type: v.Type}
			} else if errObj, ok := v.Ref.(*VMError); ok {
				return &Var{VType: TypeError, Ref: errObj, Type: v.Type}
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

func (e *Executor) normalizeTypedValue(v *Var, targetType ast.GoMiniType) *Var {
	v = e.unwrapValue(v)
	if v == nil {
		return nil
	}
	if v.Type == "" || v.Type == ast.TypeAny {
		v.Type = targetType
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
			if i < 0 || i >= len(arr.Data) {
				return nil, &VMError{Message: fmt.Sprintf("index out of range: %d", i), IsPanic: true}
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					return e.unwrapAddressVar(arr.Data[i]), nil
				},
				store: func(val *Var) error {
					arr.Data[i] = val
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
					return e.unwrapAddressVar(m.Data[key]), nil
				},
				store: func(val *Var) error {
					m.Data[key] = val
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
					return e.unwrapAddressVar(m.Data[desc.Property]), nil
				},
				store: func(val *Var) error {
					m.Data[desc.Property] = val
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
	}
	return nil, &VMError{Message: fmt.Sprintf("unsupported LHS descriptor: %T", lhs), IsPanic: true}
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
	for len(session.TaskStack) > 0 {
		// Pause/Resume Logic (Fake Context)
		if session.IsPaused() {
			select {
			case <-session.Done():
				return session.Err()
			case <-session.resumeSignal:
				// Continue execution
			}
		}

		task := session.TaskStack[len(session.TaskStack)-1]
		session.TaskStack = session.TaskStack[:len(session.TaskStack)-1]

		session.StepCount++
		if session.StepLimit > 0 {
			if session.StepCount > session.StepLimit {
				return fmt.Errorf("instruction limit exceeded (%d)", session.StepLimit)
			}
		}
		if session.Aborted() {
			return session.Err()
		}

		if task.Op == OpLineStep {
			if session.Debugger != nil && task.Source != nil {
				if session.Debugger.ShouldTrigger(task.Source.Line) {
					session.Debugger.SetStepping(false)
					session.Debugger.EventChan <- &debugger.Event{
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
				return err
			}
			continue
		}

		if err := e.dispatch(session, task); err != nil {
			frames := session.GenerateStackTrace(&task)
			var vme *VMError
			if errors.As(err, &vme) {
				if len(vme.Frames) == 0 {
					vme.Frames = frames
				}
				if vme.IsPanic {
					session.PanicVar = vme.Value
					session.PanicMessage = vme.Message
					session.PanicTrace = vme.Frames
					session.UnwindMode = UnwindPanic
				} else {
					return vme
				}
			} else {
				// Wrap unexpected errors into VMError
				return &VMError{
					Message: err.Error(),
					Frames:  frames,
					Cause:   err,
				}
			}
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
		return &VMError{
			Message: message,
			Value:   session.PanicVar,
			Frames:  frames,
			IsPanic: true,
		}
	}
	return nil
}

func (e *Executor) Disassemble() (res string) {
	defer func() {
		if r := recover(); r != nil {
			res = fmt.Sprintf("; Disassembly failed: %v\n", r)
		}
	}()

	if e.program == nil {
		return "; Error: no program loaded\n"
	}
	var sb strings.Builder
	sb.WriteString("; Go-Mini VM Disassembly\n")
	fmt.Fprintf(&sb, "; Total Variables: %d\n", len(e.program.Variables))
	fmt.Fprintf(&sb, "; Total Functions: %d\n\n", len(e.program.Functions))

	// 1. Export Global Initialization
	sb.WriteString("section .data:\n")
	for name, expr := range e.program.Variables {
		fmt.Fprintf(&sb, "  global %s\n", name)
		e.disassembleNode(&sb, "    ", expr, true)
	}
	sb.WriteString("\n")

	// 2. Export Main body / Functions
	sb.WriteString("section .text:\n")
	if len(e.program.Main) > 0 {
		sb.WriteString("main:\n")
		for _, stmt := range e.program.Main {
			e.disassembleNode(&sb, "  ", stmt, false)
		}
		sb.WriteString("\n")
	}

	// 3. Export Functions
	keys := make([]string, 0, len(e.program.Functions))
	for k := range e.program.Functions {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)

	for _, k := range keys {
		f := e.program.Functions[ast.Ident(k)]
		// Format signature nicely: (param1, param2) return
		params := []string{}
		for i, p := range f.Params {
			prefix := ""
			if f.Variadic && i == len(f.Params)-1 {
				prefix = "..."
			}
			params = append(params, prefix+string(p.Type))
		}
		sig := fmt.Sprintf("(%s) %s", strings.Join(params, ","), f.Return)
		fmt.Fprintf(&sb, "%s%s:\n", k, sig)
		e.disassembleNode(&sb, "  ", f.Body, false)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (e *Executor) disassembleNode(sb *strings.Builder, indent string, node ast.Node, isExpr bool) {
	defer func() {
		if r := recover(); r != nil {
			sb.WriteString(indent + "; Disassembly failed for this node: " + fmt.Sprintf("%v", r) + "\n")
		}
	}()

	if node == nil {
		return
	}

	var queue []Task
	if isExpr {
		queue = e.tasksForExpr(node.(ast.Expr))
	} else {
		queue = e.tasksForStmt(node.(ast.Stmt), nil)
	}

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

func (e *Executor) GetProgram() *ast.ProgramStmt {
	return e.program
}

func (e *Executor) buildImportedModuleValue(path string, modExec *Executor, modSession *StackContext) *Var {
	exports := make(map[string]*Var)
	for name := range modExec.globals {
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			v, err := modSession.Load(string(name))
			if err == nil {
				exports[string(name)] = v
			}
		}
	}
	for name, fn := range modExec.functions {
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			exports[string(name)] = &Var{
				VType: TypeClosure,
				Ref: &VMClosure{
					FunctionType: fn.FunctionType,
					BodyTasks:    cloneTasks(fn.BodyTasks),
					UpvalueSlots: nil,
					UpvalueNames: nil,
					Context:      modSession,
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
			exports[string(name)] = &Var{
				VType: TypeAny,
				Ref:   cloneRuntimeStructSpec(s),
			}
		}
	}

	return &Var{
		VType: TypeModule,
		Ref: &VMModule{
			Name:    path,
			Data:    exports,
			Context: modSession,
		},
	}
}

func (e *Executor) executeImportedProgram(parent *StackContext, path string, prog *ast.ProgramStmt, prepared *PreparedProgram) (*Var, error) {
	var err error
	if prepared == nil {
		prepared, err = PrepareProgram(prog)
		if err != nil {
			return nil, err
		}
	}
	modExecutor, err := NewExecutorFromPrepared(prog, prepared)
	if err != nil {
		return nil, err
	}
	modExecutor.ModuleLoader = e.ModuleLoader
	modExecutor.ModulePlanLoader = e.ModulePlanLoader
	modExecutor.StepLimit = e.StepLimit
	modExecutor.routes = e.routes

	modSession := modExecutor.NewSession(parent.Context, "global")
	modSession.StepLimit = parent.StepLimit
	modSession.StepCount = parent.StepCount
	modSession.ModuleCache = parent.ModuleCache
	modSession.LoadingModules = parent.LoadingModules
	modSession.Debugger = parent.Debugger

	if err := modExecutor.InitializeSession(modSession, nil, false); err != nil {
		parent.StepCount = modSession.StepCount
		return nil, err
	}
	parent.StepCount = modSession.StepCount
	return e.buildImportedModuleValue(path, modExecutor, modSession), nil
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
	if session.ModuleCache == nil {
		session.ModuleCache = make(map[string]*Var)
	}
	if session.LoadingModules == nil {
		session.LoadingModules = make(map[string]bool)
	}

	for i := len(stmts) - 1; i >= 0; i-- {
		session.TaskStack = append(session.TaskStack, e.tasksForStmt(stmts[i], nil)...)
	}

	err := e.Run(session)

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
	if ctx.ModuleCache == nil {
		ctx.ModuleCache = make(map[string]*Var)
	}
	if ctx.LoadingModules == nil {
		ctx.LoadingModules = make(map[string]bool)
	}

	err := e.Run(ctx)
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
