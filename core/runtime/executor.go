package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/debugger"
)

type Executor struct {
	structs    map[string]*ast.StructStmt
	interfaces map[string]*ast.InterfaceStmt
	types      map[string]ast.GoMiniType
	consts     map[string]string
	funcs      map[ast.Ident]*Var
	program    *ast.ProgramStmt

	routes map[string]FFIRoute

	Loader func(path string) (*ast.ProgramStmt, error)

	StepLimit int64

	interfaceCache map[ast.GoMiniType]map[string]*ast.FunctionType
	mu             sync.RWMutex
	lastSession    *StackContext
}

func (e *Executor) LastSession() *StackContext {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastSession
}

func NewExecutor(program *ast.ProgramStmt) (*Executor, error) {
	if program == nil {
		return nil, errors.New("invalid program")
	}
	result := &Executor{
		program:        program,
		structs:        make(map[string]*ast.StructStmt),
		interfaces:     make(map[string]*ast.InterfaceStmt),
		types:          make(map[string]ast.GoMiniType),
		funcs:          make(map[ast.Ident]*Var),
		consts:         make(map[string]string),
		routes:         make(map[string]FFIRoute),
		interfaceCache: make(map[ast.GoMiniType]map[string]*ast.FunctionType),
	}
	if program.Structs != nil {
		for ident, stmt := range program.Structs {
			result.structs[string(ident)] = stmt
		}
	}
	if program.Interfaces != nil {
		for ident, stmt := range program.Interfaces {
			result.interfaces[string(ident)] = stmt
		}
	}
	if program.Types != nil {
		for ident, t := range program.Types {
			result.types[string(ident)] = t
		}
	}
	for s, s2 := range program.Constants {
		result.consts[s] = s2
	}
	return result, nil
}

func (e *Executor) RegisterRoute(name string, route FFIRoute) {
	e.routes[name] = route
}

// ExecExpr 模拟原 Executor.ExecExpr 用于初始化全局变量 (临时回退机制，直至完全重构)
func (e *Executor) ExecExpr(ctx *StackContext, s ast.Expr) (*Var, error) {
	// 在完全迭代化之前，先使用一个临时的子 Run()
	oldTasks := ctx.TaskStack
	oldValues := ctx.ValueStack

	ctx.TaskStack = []Task{{Op: OpEval, Node: s}}
	ctx.ValueStack = &ValueStack{}

	err := e.Run(ctx)
	if err != nil {
		ctx.TaskStack = oldTasks
		ctx.ValueStack = oldValues
		return nil, err
	}

	res := ctx.ValueStack.Pop()
	ctx.TaskStack = oldTasks
	ctx.ValueStack = oldValues
	return res, nil
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
	inner := val
	if val.VType == TypeAny && val.Ref != nil {
		if v, ok := val.Ref.(*Var); ok {
			inner = v
		}
	}
	if inner.Type.Equals(interfaceType) {
		return inner.Copy(), nil
	}

	actualInterfaceType := interfaceType
	if !interfaceType.IsInterface() {
		// 3. Resolve named type ONLY if it could be an interface or struct
		if actual, ok := e.types[string(interfaceType)]; ok {
			if actual.IsInterface() {
				return e.CheckSatisfaction(val, actual)
			}
			// If it's a struct alias, we'll find it below via structs map
		}

		// 4. Resolve named interface
		if iStmt, ok := e.interfaces[string(interfaceType)]; ok {
			actualInterfaceType = iStmt.Type
		} else {
			// If it wasn't an exact match and isn't an interface, it fails (like Go)
			return nil, fmt.Errorf("interface conversion: interface is %s, not %s", inner.Type, interfaceType)
		}
	}

	e.mu.RLock()
	methods, ok := e.interfaceCache[actualInterfaceType]
	e.mu.RUnlock()

	if !ok {
		parsedMethods, ok := actualInterfaceType.ReadInterfaceMethods()
		if !ok {
			return nil, fmt.Errorf("invalid interface type: %s", actualInterfaceType)
		}
		methods = parsedMethods
		e.mu.Lock()
		e.interfaceCache[actualInterfaceType] = methods
		e.mu.Unlock()
	}

	for name, sig := range methods {
		if !e.hasMethodWithSignature(inner, name, sig) {
			return nil, fmt.Errorf("type %v does not implement %s: missing or incompatible method %s", inner.VType, interfaceType, name)
		}
	}

	return &Var{
		Type:  interfaceType,
		VType: TypeInterface,
		Ref: &VMInterface{
			Target:  inner.Copy(),
			Methods: methods,
		},
	}, nil
}

func (e *Executor) hasMethodWithSignature(val *Var, name string, expectedSig *ast.FunctionType) bool {
	if val == nil {
		return false
	}

	// 穿透 TypeAny
	if val.VType == TypeAny && val.Ref != nil {
		if inner, ok := val.Ref.(*Var); ok {
			return e.hasMethodWithSignature(inner, name, expectedSig)
		}
	}

	switch val.VType {
	case TypeHandle:
		tName := string(val.Type)
		tName = strings.TrimPrefix(tName, "Ptr<")
		tName = strings.TrimPrefix(tName, "*")
		tName = strings.TrimSuffix(tName, ">")
		methodName := fmt.Sprintf("__method_%s_%s", tName, name)
		if route, ok := e.routes[methodName]; ok {
			// 校验 FFI 签名
			if fType, ok := ast.GoMiniType(route.Spec).ReadFunc(); ok {
				return e.isSignatureCompatible(fType, expectedSig)
			}
			return true // 兜底：如果没拿到签名但路由存在，暂且通过
		}
	case TypeMap:
		if m, ok := val.Ref.(*VMMap); ok {
			if v, ok := m.Data[name]; ok {
				return e.isCallableCompatible(v, expectedSig)
			}
		}
		// 检查 Mangle 后的脚本方法
		tName := string(val.Type)
		if tName != "" && tName != "Any" {
			mName := fmt.Sprintf("__method_%s_%s", tName, name)
			if f, ok := e.program.Functions[ast.Ident(mName)]; ok {
				return e.isSignatureCompatible(&f.FunctionType, expectedSig)
			}
		}
	case TypeModule:
		if mod, ok := val.Ref.(*VMModule); ok {
			var v *Var
			if mod.Context != nil {
				if vLoad, err := mod.Context.Load(name); err == nil {
					v = vLoad
				}
			}
			if v == nil {
				v = mod.Data[name]
			}
			if v != nil {
				return e.isCallableCompatible(v, expectedSig)
			}
		}
	case TypeInterface:
		if inter, ok := val.Ref.(*VMInterface); ok {
			if sig, ok := inter.Methods[name]; ok {
				return e.isSignatureCompatible(sig, expectedSig)
			}
		}
	}
	return false
}

func (e *Executor) isCallableCompatible(v *Var, expectedSig *ast.FunctionType) bool {
	if v.VType == TypeClosure {
		if cl, ok := v.Ref.(*VMClosure); ok {
			return e.isSignatureCompatible(&cl.FuncDef.FunctionType, expectedSig)
		}
	}
	if v.VType == TypeAny && v.Ref != nil {
		if route, ok := v.Ref.(FFIRoute); ok {
			if fType, ok := ast.GoMiniType(route.Spec).ReadFunc(); ok {
				return e.isSignatureCompatible(fType, expectedSig)
			}
		}
		if inner, ok := v.Ref.(*Var); ok {
			return e.isCallableCompatible(inner, expectedSig)
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

func (e *Executor) InvokeCallable(ctx *StackContext, callable *Var, methodName string, args []*Var) (*Var, error) {
	if callable == nil {
		return nil, errors.New("cannot invoke nil callable")
	}

	// Save old task stack and value stack
	oldTasks := ctx.TaskStack
	oldValues := ctx.ValueStack
	ctx.TaskStack = nil
	ctx.ValueStack = &ValueStack{}

	// Prepare the call
	// If methodName is provided, treat callable as receiver
	var receiver *Var
	name := methodName
	actualCallable := callable
	if methodName != "" {
		receiver = callable
		actualCallable = nil
	}

	err := e.invokeCall(ctx, &ast.CallExprStmt{}, name, receiver, nil, actualCallable, args)
	if err != nil {
		ctx.TaskStack = oldTasks
		ctx.ValueStack = oldValues
		return nil, err
	}

	// Run until the call returns (indicated by OpCallBoundary)
	err = e.Run(ctx)
	if err != nil {
		ctx.TaskStack = oldTasks
		ctx.ValueStack = oldValues
		return nil, err
	}

	// Get result
	var res *Var
	if ctx.ValueStack.Len() > 0 {
		res = ctx.ValueStack.Pop()
	}

	// Restore old stacks
	ctx.TaskStack = oldTasks
	ctx.ValueStack = oldValues
	return res, nil
}

func (e *Executor) Execute(ctx context.Context) (err error) {
	return e.ExecuteWithEnv(ctx, nil)
}

func (e *Executor) ExecuteWithEnv(ctx context.Context, env map[string]*Var) (err error) {
	session := &StackContext{
		Context:  ctx,
		Executor: e,
		Stack: &Stack{
			MemoryPtr: make(map[string]*Var),
			Scope:     "global",
			Depth:     1,
		},
		StepLimit:      e.StepLimit,
		ModuleCache:    make(map[string]*Var),
		LoadingModules: make(map[string]bool),
		ActiveHandles:  &HandleTracker{Handles: make([]HandleRef, 0, 128)},
		Debugger:       debugger.GetDebugger(ctx),
		TaskStack:      make([]Task, 0, 128),
		ValueStack:     &ValueStack{},
		UnwindMode:     UnwindNone,
		resumeSignal:   make(chan struct{}, 1),
	}

	// Setup Context Bridge (Fake Context logic)
	// Propagate host cancellation to VM internal status bit.
	if ctx != nil && ctx.Done() != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				session.Abort()
			case <-done:
			}
		}()
	}

	e.mu.Lock()
	e.lastSession = session
	e.mu.Unlock()

	defer func() {
		// Clean up all active handles to prevent memory leaks on VM exit
		if session.ActiveHandles != nil {
			for _, h := range session.ActiveHandles.Handles {
				if h.Bridge != nil && h.ID != 0 {
					_ = h.Bridge.DestroyHandle(h.ID)
				}
			}
		}
		// Ensure all defers are run
		session.Stack.RunDefers()
	}()

	// 注入环境变量
	for name, expr := range e.program.Variables {
		var val *Var
		if expr != nil {
			v, err := e.ExecExpr(session, expr)
			if err != nil {
				return fmt.Errorf("failed to initialize global var %s: %w", name, err)
			}
			val = v
		} else {
			// 如果没有初值，则初始化为零值变量 (Any 类型)
			val = &Var{VType: TypeAny, Type: "Any"}
		}
		// 确保变量已存储到内存中 (直接操作内存字典以避开 AddVariable 的 Copy 逻辑，适合初始化)
		session.Stack.MemoryPtr[string(name)] = val
	}

	// 注入内建 nil
	_ = session.AddVariable("nil", nil)

	// 注入环境变量 (放到后面，允许覆盖脚本定义的同名全局变量)
	for k, v := range env {
		_ = session.AddVariable(k, v)
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
	for i := len(e.program.Main) - 1; i >= 0; i-- {
		session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: e.program.Main[i]})
	}

	err = e.Run(session)
	if err != nil {
		return err
	}

	// 自动寻找并执行 main() 入口函数
	if f, ok := e.program.Functions["main"]; ok {
		session.TaskStack = append(session.TaskStack, Task{
			Op:   OpCallBoundary,
			Data: map[string]interface{}{"oldStack": session.Stack, "hasReturn": false},
		})
		session.TaskStack = append(session.TaskStack, Task{Op: OpDoCall, Node: f, Data: nil}) // args=nil

		// Start run loop again for main func
		err = e.Run(session)
		session.Stack.RunDefers()
		if err != nil {
			return err
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
		clause := task.Node.(*ast.CatchClause)
		session.TaskStack = append(session.TaskStack, Task{Op: OpScopeExit})
		session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: clause.Body})
		session.TaskStack = append(session.TaskStack, Task{
			Op:   OpCatchScopeEnter,
			Node: clause,
			Data: session.PanicVar,
		})
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

	if task.Op == OpLoopBoundary {
		if session.UnwindMode == UnwindBreak || session.UnwindMode == UnwindContinue {
			session.UnwindMode = UnwindNone
			return true, nil
		}
	}

	if task.Op == OpImportDone {
		// Even on panic, we must restore the parent session
		data := task.Data.(*ImportData)
		session.Executor = data.OldExecutor.(ExecutorAPI)
		session.Stack = data.OldStack
		delete(session.LoadingModules, data.Path)
		// Return true to indicate we handled this task, but keep UnwindMode as is to continue unwinding in parent
		return true, nil
	}

	if task.Op == OpCallBoundary {
		data := task.Data.(map[string]interface{})
		oldStack := data["oldStack"].(*Stack)
		hasReturn := data["hasReturn"].(bool)

		if session.UnwindMode == UnwindReturn {
			session.UnwindMode = UnwindNone
			if hasReturn {
				res, _ := session.Load("__return__")
				session.ValueStack.Push(res)
			}
			session.Stack = oldStack
			if oldExec, ok := data["oldExec"]; ok && oldExec != nil {
				session.Executor = oldExec.(ExecutorAPI)
			}
			return true, nil
		}

		// If it's a panic, still restore the stack and continue unwinding
		session.Stack = oldStack
		if oldExec, ok := data["oldExec"]; ok && oldExec != nil {
			session.Executor = oldExec.(ExecutorAPI)
		}
		return false, nil
	}

	if task.Op == OpLoopBoundary {
		if session.UnwindMode == UnwindBreak {
			session.UnwindMode = UnwindNone
			return true, nil
		}
		if session.UnwindMode == UnwindContinue {
			session.UnwindMode = UnwindNone
			if err := e.dispatch(session, *task); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, nil // Continue unwinding if it's a panic/return
	}

	if task.Op == OpCatchBoundary {
		data := task.Data.(map[string]interface{})
		if session.UnwindMode == UnwindReturn {
			session.UnwindMode = UnwindNone
			if err := e.dispatch(session, *task); err != nil {
				return false, err
			}
			return true, nil
		}
		oldStack := data["oldStack"].(*Stack)
		session.Stack = oldStack
		// Restore executor if saved (cross-module calls) during panic unwind
		if oldExec, ok := data["oldExec"]; ok {
			session.Executor = oldExec.(ExecutorAPI)
		}
		return false, nil
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
	case OpExec:
		return e.handleExec(session, task.Node.(ast.Stmt), task.Data)
	case OpEval:
		if task.Node == nil {
			session.ValueStack.Push(nil)
			return nil
		}
		return e.handleEval(session, task.Node.(ast.Expr))
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
		op := task.Data.(string)
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
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: task.Node.(ast.Expr)})
		return nil
	case OpLoadVar:
		name := task.Data.(string)
		v, err := session.Load(name)
		if err != nil {
			exec := session.Executor.(*Executor)
			if exec.program != nil {
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
	case OpScopeEnter:
		scopeName := task.Data.(string)
		session.ScopeApply(scopeName)
		return nil
	case OpScopeExit:
		session.ScopeExit()
		return nil
	case OpAssign:
		val := session.ValueStack.Pop()
		lhsDescVar := session.ValueStack.Pop()
		if lhsDescVar == nil {
			return nil
		}
		lhsDesc := lhsDescVar.Ref
		return e.assignToLHSDesc(session, lhsDesc, val)
	case OpDoCall:
		f := task.Node.(*ast.FunctionStmt)
		var args []*Var
		if task.Data != nil {
			args = task.Data.([]*Var)
		}
		return e.setupFuncCall(session, string(f.Name), &ast.FuncLitExpr{
			FunctionType: f.FunctionType,
			Body:         f.Body,
		}, args, nil)
	case OpMultiAssign:
		lhsCount := task.Data.(int)
		val := session.ValueStack.Pop()
		descs := make([]interface{}, lhsCount)
		for i := lhsCount - 1; i >= 0; i-- {
			descVar := session.ValueStack.Pop()
			if descVar != nil {
				descs[i] = descVar.Ref
			} else {
				descs[i] = nil
			}
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
			if err := e.assignToLHSDesc(session, descs[i], elements[i]); err != nil {
				return err
			}
		}
		return nil
	case OpIncDec:
		op := task.Data.(string)
		lhsDesc := session.ValueStack.Pop().Ref
		return e.incDecLHSDesc(session, lhsDesc, op)
	case OpReturn:
		count := task.Data.(int)
		if count > 1 {
			// 多返回值，打包成 Tuple
			elements := make([]*Var, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = session.ValueStack.Pop()
			}
			res := &Var{VType: TypeArray, Ref: &VMArray{Data: elements}}
			_ = session.Store("__return__", res)
		} else if count == 1 {
			// 单返回值
			res := session.ValueStack.Pop()
			if res != nil {
				_ = session.Store("__return__", res)
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
		if task.Node == nil {
			session.ValueStack.Push(nil)
			return nil
		}
		return e.evalLHS(session, task.Node.(ast.Expr))
	case OpEvalLHSIndex:
		idx := session.ValueStack.Pop()
		obj := session.ValueStack.Pop()
		if obj != nil && obj.VType == TypeCell {
			obj = obj.Ref.(*Cell).Value
		}
		// idx also needs to be unwrapped and copied to ensure it's stable
		if idx != nil && idx.VType == TypeCell {
			idx = idx.Ref.(*Cell).Value
		}
		if idx != nil {
			idx = idx.Copy()
		}
		session.ValueStack.Push(&Var{VType: TypeAny, Ref: &LHSIndex{Obj: obj, Index: idx}})
		return nil
	case OpEvalLHSMember:
		prop := task.Data.(string)
		obj := session.ValueStack.Pop()
		if obj != nil && obj.VType == TypeCell {
			obj = obj.Ref.(*Cell).Value
		}
		session.ValueStack.Push(&Var{VType: TypeAny, Ref: &LHSMember{Obj: obj, Property: prop}})
		return nil
	case OpIndex:
		idx := session.ValueStack.Pop()
		obj := session.ValueStack.Pop()
		n := task.Node.(*ast.IndexExpr)
		if n.Multi {
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
				session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: tuple}, Type: n.GetBase().Type})
				return nil
			}
			// Fallback for Any
			if obj.VType == TypeAny && obj.Ref != nil {
				if inner, ok := obj.Ref.(*Var); ok {
					// Recursively handle if it's a Map inside Any
					if inner.VType == TypeMap {
						// Simple trick: replace obj and re-dispatch
						session.ValueStack.Push(inner)
						session.ValueStack.Push(idx)
						return e.dispatch(session, task)
					}
				}
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
		n := task.Node.(*ast.CompositeExpr)
		isArray := n.Type.IsArray()
		isMap := n.Type.IsMap()

		if isArray {
			elemType, _ := n.Type.ReadArrayItemType()
			res := make([]*Var, len(n.Values))
			// ValueStack has [V1, V2, ..., VN]
			// So we must pop in reverse: V_N first, then V_N-1...
			for i := len(n.Values) - 1; i >= 0; i-- {
				val := session.ValueStack.Pop()
				if val != nil && (val.Type == "" || val.Type == "Any") {
					val.Type = elemType
					if val.VType == TypeAny && val.Ref != nil {
						if _, ok := val.Ref.(*VMArray); ok {
							val.VType = TypeArray
						} else if _, ok := val.Ref.(*VMMap); ok {
							val.VType = TypeMap
						}
					}
				}
				res[i] = val
			}
			session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: res}, Type: n.Type})
			return nil
		}

		res := make(map[string]*Var)
		var valType ast.GoMiniType
		if isMap {
			_, valType, _ = n.Type.GetMapKeyValueTypes()
		}

		// Values are pushed as [..., K_i, V_i]
		// So we must pop in reverse order: V_i then K_i
		for i := len(n.Values) - 1; i >= 0; i-- {
			v := n.Values[i]

			val := session.ValueStack.Pop()
			if val != nil && isMap && (val.Type == "" || val.Type == "Any") {
				val.Type = valType
				if val.VType == TypeAny && val.Ref != nil {
					if _, ok := val.Ref.(*VMArray); ok {
						val.VType = TypeArray
					} else if _, ok := val.Ref.(*VMMap); ok {
						val.VType = TypeMap
					}
				}
			}

			keyName := ""
			var keyVal *Var
			if v.Key != nil {
				if ident, ok := v.Key.(*ast.IdentifierExpr); ok {
					keyName = string(ident.Name)
				} else {
					keyVal = session.ValueStack.Pop()
					keyName = keyVal.Str
					if keyVal.VType == TypeInt {
						keyName = strconv.FormatInt(keyVal.I64, 10)
					}
				}
			}

			res[keyName] = val
		}
		session.ValueStack.Push(&Var{VType: TypeMap, Ref: &VMMap{Data: res}, Type: n.Type})
		return nil
	case OpSlice:
		n := task.Node.(*ast.SliceExpr)
		var high, low, obj *Var
		if n.High != nil {
			high = session.ValueStack.Pop()
		}
		if n.Low != nil {
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
		n := task.Node.(*ast.CallExprStmt)
		var name string
		var receiver *Var
		var mod *VMModule
		var callable *Var

		// Arguments are on top of stack, then Func eval result (if any)
		// Let's pop arguments first!
		args := make([]*Var, len(n.Args))
		for i := len(n.Args) - 1; i >= 0; i-- {
			args[i] = session.ValueStack.Pop()
		}

		// 处理变长参数展开 f(args...)
		if n.Ellipsis && len(args) > 0 {
			last := args[len(args)-1]
			if last != nil && last.VType == TypeArray {
				arr := last.Ref.(*VMArray)
				newArgs := make([]*Var, len(args)-1+len(arr.Data))
				copy(newArgs, args[:len(args)-1])
				copy(newArgs[len(args)-1:], arr.Data)
				args = newArgs
			} else if last != nil && last.VType == TypeAny && last.Ref != nil {
				// 支持 Any 包装下的 Array 展开
				if inner, ok := last.Ref.(*Var); ok && inner.VType == TypeArray {
					arr := inner.Ref.(*VMArray)
					newArgs := make([]*Var, len(args)-1+len(arr.Data))
					copy(newArgs, args[:len(args)-1])
					copy(newArgs[len(args)-1:], arr.Data)
					args = newArgs
				}
			}
		}

		if ident, ok := n.Func.(*ast.ConstRefExpr); ok {
			name = string(ident.Name)
		} else if ident, ok := n.Func.(*ast.IdentifierExpr); ok {
			name = string(ident.Name)
		} else if member, ok := n.Func.(*ast.MemberExpr); ok {
			obj := session.ValueStack.Pop()
			if obj == nil {
				return errors.New("calling method on nil object")
			}

			// Use unified member evaluation logic
			res, err := e.evalMemberExprDirect(session, obj, string(member.Property))
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
				name = string(member.Property)
			} else if res != nil {
				callable = res
			} else {
				return fmt.Errorf("property %s is not a callable function on %v", member.Property, obj.VType)
			}
		} else {
			callable = session.ValueStack.Pop()
		}

		if name != "" && mod == nil && callable == nil {
			if v, err := session.Load(name); err == nil && v != nil {
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

		return e.invokeCall(session, n, name, receiver, mod, callable, finalArgs)
	case OpCallBoundary:
		dataMap, ok := task.Data.(map[string]interface{})
		if !ok {
			return fmt.Errorf("OpCallBoundary data is not map[string]interface{}: %T (%v)", task.Data, task.Data)
		}
		oldStack := dataMap["oldStack"].(*Stack)
		hasReturn := dataMap["hasReturn"].(bool)

		// Restore executor if saved (cross-module calls)
		if oldExec, ok := dataMap["oldExec"]; ok {
			session.Executor = oldExec.(ExecutorAPI)
		}

		var retVal *Var
		if hasReturn {
			retVal, _ = session.Load("__return__")
		}

		session.Stack = oldStack

		if hasReturn {
			session.ValueStack.Push(retVal)
		}

		if session.UnwindMode == UnwindReturn {
			session.UnwindMode = UnwindNone
		}
		return nil
	case OpAssert:
		val := session.ValueStack.Pop()
		n := task.Node.(*ast.TypeAssertExpr)
		targetType := n.Type
		res, err := e.CheckSatisfaction(val, targetType)
		if n.Multi {
			if err != nil {
				// 返回 (nil, false)
				tuple := make([]*Var, 2)
				tuple[0] = nil
				tuple[1] = NewBool(false)
				session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: tuple}, Type: n.GetBase().Type})
			} else {
				// 返回 (res, true)
				tuple := make([]*Var, 2)
				tuple[0] = res
				tuple[1] = NewBool(true)
				session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: tuple}, Type: n.GetBase().Type})
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
	case OpLoopBoundary:
		if err := session.Context.Err(); err != nil {
			return err
		}
		if n, ok := task.Node.(*ast.ForStmt); ok {
			if n.Cond != nil {
				session.TaskStack = append(session.TaskStack, Task{Op: OpForCond, Node: n})
				session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Cond})
			} else {
				e.scheduleForBody(session, n)
			}
		}
		return nil
	case OpForCond:
		n := task.Node.(*ast.ForStmt)
		condVal := session.ValueStack.Pop()
		b, err := condVal.ToBool()
		if err != nil {
			return err
		}
		if b {
			e.scheduleForBody(session, n)
		}
		return nil
	case OpForScopeEnter:
		parentVars := session.Stack.MemoryPtr
		session.ScopeApply("for_body")
		for k, v := range parentVars {
			val := v
			if v != nil && v.VType == TypeCell {
				val = v.Ref.(*Cell).Value
			}
			_ = session.AddVariable(k, val)
		}
		return nil
	case OpForScopeExit:
		bodyVars := session.Stack.MemoryPtr
		parentVars := session.Stack.Parent.MemoryPtr
		for k, v := range bodyVars {
			if parentVar, ok := parentVars[k]; ok {
				source := v
				if v != nil && v.VType == TypeCell {
					source = v.Ref.(*Cell).Value
				}
				dest := parentVar
				if parentVar != nil && parentVar.VType == TypeCell {
					dest = parentVar.Ref.(*Cell).Value
				}
				if dest != nil && source != nil {
					copyVarData(dest, source)
				}
			}
		}
		session.ScopeExit()
		return nil
	case OpLoopContinue:
		return nil
	case OpRangeInit:
		n := task.Node.(*ast.RangeStmt)
		obj := session.ValueStack.Pop()
		if obj == nil {
			return nil
		}
		rData := &RangeData{Stmt: n, Obj: obj}
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
		session.TaskStack = append(session.TaskStack, Task{Op: OpLoopBoundary, Node: n})
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
		session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: rData.Stmt.Body})
		session.TaskStack = append(session.TaskStack, Task{
			Op:   OpRangeScopeEnter,
			Data: map[string]interface{}{"rData": rData, "key": key, "val": val},
		})
		return nil
	case OpRangeScopeEnter:
		data := task.Data.(map[string]interface{})
		rData := data["rData"].(*RangeData)
		key := data["key"].(*Var)
		val := data["val"].(*Var)
		n := rData.Stmt
		session.ScopeApply("for_range_body")
		if n.Define {
			if n.Key != "" && n.Key != "_" {
				_ = session.AddVariable(string(n.Key), key)
			}
			if n.Value != "" && n.Value != "_" && val != nil {
				_ = session.AddVariable(string(n.Value), val)
			}
		} else {
			if n.Key != "" && n.Key != "_" {
				_ = session.Store(string(n.Key), key)
			}
			if n.Value != "" && n.Value != "_" && val != nil {
				_ = session.Store(string(n.Value), val)
			}
		}
		return nil
	case OpSwitchTag:
		n := task.Node.(*ast.SwitchStmt)
		var tag *Var
		if n.Tag != nil {
			tag = session.ValueStack.Pop()
		} else {
			tag = NewBool(true)
		}
		// 如果是 v := x.(type)，在这里处理初值绑定（如果需要，或者推迟到 case）
		// 在 Go 中，v 的类型在每个 case 块中是不同的。
		session.TaskStack = append(session.TaskStack, Task{
			Op:   OpSwitchNextCase,
			Node: n,
			Data: map[string]interface{}{"tag": tag, "idx": 0},
		})
		return nil
	case OpSwitchNextCase:
		data := task.Data.(map[string]interface{})
		n := task.Node.(*ast.SwitchStmt)
		tag := data["tag"].(*Var)
		idx := data["idx"].(int)

		if idx >= len(n.Body.Children) {
			var defaultClause *ast.CaseClause
			for _, child := range n.Body.Children {
				clause := child.(*ast.CaseClause)
				if clause.List == nil {
					defaultClause = clause
					break
				}
			}
			if defaultClause != nil {
				session.TaskStack = append(session.TaskStack, Task{Op: OpScopeExit})
				session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: &ast.BlockStmt{Children: defaultClause.Body, Inner: true}})
				if n.IsType && n.Assign != nil {
					// 即使是 default，也要绑定变量
					session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n.Assign, Data: tag})
				}
				if n.IsType {
					session.TaskStack = append(session.TaskStack, Task{Op: OpScopeEnter, Data: "switch_default"})
				}
			}
			return nil
		}

		clause := n.Body.Children[idx].(*ast.CaseClause)
		if clause.List == nil {
			// 这是 default 分支，索引加 1 并继续下一轮
			data["idx"] = idx + 1
			session.TaskStack = append(session.TaskStack, task)
			return nil
		}

		if n.IsType {
			// Type Switch 匹配
			match := false
			for _, caseExpr := range clause.List {
				var targetType ast.GoMiniType
				if id, ok := caseExpr.(*ast.IdentifierExpr); ok {
					targetType = ast.GoMiniType(id.Name)
				} else {
					targetType = caseExpr.GetBase().Type
				}

				if targetType == "" {
					continue
				}

				// 0. Nil 匹配处理
				if tag == nil || (tag.VType == TypeAny && tag.Ref == nil) {
					if targetType == "nil" || targetType == "Any" || targetType == "interface{}" {
						match = true
						break
					}
					continue // Nil 不匹配具体的非 Any 类型
				}

				// 1. 基础类型匹配
				switch targetType {
				case "Int64", "int", "int64":
					if tag.VType == TypeInt {
						match = true
					}
				case "Float64", "float64":
					if tag.VType == TypeFloat {
						match = true
					}
				case "String", "string":
					if tag.VType == TypeString {
						match = true
					}
				case "Bool", "bool":
					if tag.VType == TypeBool {
						match = true
					}
				case "TypeBytes", "bytes":
					if tag.VType == TypeBytes {
						match = true
					}
				case "Any", "interface{}":
					if tag != nil {
						match = true
					}
				case "Error", "error":
					if tag.VType == TypeError {
						match = true
					}
				}

				if match {
					break
				}

				// 2. 接口满足性匹配
				if targetType.IsInterface() || e.interfaces[string(targetType)] != nil {
					_, err := e.CheckSatisfaction(tag, targetType)
					if err == nil {
						match = true
						break
					}
				}
			}

			if match {
				// 关键：所有分支逻辑（包括赋值和主体）都必须在一个统一的 Switch 作用域内
				session.TaskStack = append(session.TaskStack, Task{Op: OpScopeExit})
				session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: &ast.BlockStmt{Children: clause.Body, Inner: true}})
				if n.Assign != nil {
					session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n.Assign, Data: tag})
				}
				session.TaskStack = append(session.TaskStack, Task{Op: OpScopeEnter, Data: "switch_matched"})
				return nil
			}
			// 没匹配上，继续下一个 case
			data["idx"] = idx + 1
			session.TaskStack = append(session.TaskStack, task)
			return nil
		}

		// Value Switch (原有逻辑)
		session.TaskStack = append(session.TaskStack, Task{
			Op:   OpSwitchMatchCase,
			Node: clause,
			Data: map[string]interface{}{"tag": tag, "switchTask": task, "exprIdx": 0},
		})
		return nil
	case OpSwitchMatchCase:
		data := task.Data.(map[string]interface{})
		clause := task.Node.(*ast.CaseClause)
		tag := data["tag"].(*Var)
		switchTask := data["switchTask"].(Task)
		exprIdx := data["exprIdx"].(int)

		if exprIdx > 0 {
			val := session.ValueStack.Pop()
			res, _ := e.evalComparison("==", tag, val)
			if res != nil && res.Bool {
				session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: &ast.BlockStmt{Children: clause.Body, Inner: true}})
				return nil
			}
		}

		if exprIdx < len(clause.List) {
			data["exprIdx"] = exprIdx + 1
			session.TaskStack = append(session.TaskStack, task)
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: clause.List[exprIdx]})
			return nil
		}

		nextData := switchTask.Data.(map[string]interface{})
		nextData["idx"] = nextData["idx"].(int) + 1
		session.TaskStack = append(session.TaskStack, switchTask)
		return nil
	case OpCatchBoundary:
		return nil
	case OpFinally:
		n := task.Node.(*ast.BlockStmt)
		session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n})
		return nil
	case OpCatchScopeEnter:
		clause := task.Node.(*ast.CatchClause)
		panicVar := task.Data.(*Var)
		session.ScopeApply("catch")
		if clause.VarName != "" {
			_ = session.NewVar(string(clause.VarName), "Any")
			_ = session.Store(string(clause.VarName), panicVar)
		}
		return nil
	case OpBranchIf:
		condVal := session.ValueStack.Pop()
		b, err := condVal.ToBool()
		if err != nil {
			return err
		}
		n := task.Node.(*ast.IfStmt)
		if b {
			session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n.Body})
		} else if n.ElseBody != nil {
			session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n.ElseBody})
		}
		return nil
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
		n := task.Node.(*ast.ImportExpr)
		path := strings.Trim(n.Path, " \t\n\r")
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

		if e.Loader != nil {
			prog, err := e.Loader(path)
			if err == nil {
				session.LoadingModules[path] = true
				modExecutor, err := NewExecutor(prog)
				if err != nil {
					delete(session.LoadingModules, path)
					return err
				}
				modExecutor.Loader = e.Loader
				modExecutor.routes = e.routes

				modSession := &StackContext{
					Context:        session.Context,
					Executor:       modExecutor,
					Stack:          &Stack{MemoryPtr: make(map[string]*Var), Scope: "global", Depth: 1},
					StepLimit:      session.StepLimit,
					StepCount:      session.StepCount,
					ModuleCache:    session.ModuleCache,
					LoadingModules: session.LoadingModules,
					ActiveHandles:  session.ActiveHandles, // Share the same handles slice
					Debugger:       session.Debugger,
				}

				// Push Done task to current stack (restore context later)
				session.TaskStack = append(session.TaskStack, Task{
					Op: OpImportDone,
					Data: &ImportData{
						Path:          path,
						OldExecutor:   session.Executor,
						OldStack:      session.Stack,
						OldTaskStack:  session.TaskStack,
						OldValueStack: session.ValueStack,
						ModSession:    modSession,
					},
				})

				// Switch current session fields
				session.Executor = modExecutor
				session.Stack = modSession.Stack
				session.UnwindMode = UnwindNone

				// Push Global variables init
				var names []string
				for name := range prog.Variables {
					names = append(names, string(name))
				}
				sort.Strings(names)
				for i := len(names) - 1; i >= 0; i-- {
					name := names[i]
					expr := prog.Variables[ast.Ident(name)]
					session.TaskStack = append(session.TaskStack, Task{Op: OpInitVar, Data: name})
					session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: expr})
				}

				// Push Main block execution
				for i := len(prog.Main) - 1; i >= 0; i-- {
					session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: prog.Main[i]})
				}

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

		if found {
			res := &Var{VType: TypeModule, Ref: ffiMod}
			session.ModuleCache[path] = res
			session.ValueStack.Push(res)
			return nil
		}
		return fmt.Errorf("failed to load module %s", path)

	case OpImportDone:
		data := task.Data.(*ImportData)
		path := data.Path
		modSession := data.ModSession
		prog := modSession.Executor.(*Executor).program

		delete(session.LoadingModules, path)

		exports := make(map[string]*Var)
		for name := range prog.Variables {
			if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
				v, err := modSession.Load(string(name))
				if err == nil {
					exports[string(name)] = v
				}
			}
		}
		for name, fn := range prog.Functions {
			if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
				exports[string(name)] = &Var{
					VType: TypeClosure,
					Ref: &VMClosure{
						FuncDef: &ast.FuncLitExpr{
							BaseNode:     fn.BaseNode,
							FunctionType: fn.FunctionType,
							Body:         fn.Body,
						},
						Upvalues: make(map[string]*Var),
						Context:  modSession,
					},
				}
			}
		}
		for name, val := range prog.Constants {
			if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
				exports[name] = NewString(val)
			}
		}
		for name, s := range prog.Structs {
			if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
				exports[string(name)] = &Var{
					VType: TypeAny,
					Ref:   s,
				}
			}
		}

		// modSession.Stack should remain its own global stack for future member access to module variables
		// modSession.Stack = session.Stack <--- REMOVED THIS LINE

		// Restore session
		session.Executor = data.OldExecutor.(ExecutorAPI)
		session.Stack = data.OldStack
		session.TaskStack = data.OldTaskStack
		session.ValueStack = data.OldValueStack
		session.UnwindMode = UnwindNone

		res := &Var{
			VType: TypeModule,
			Ref: &VMModule{
				Name:    path,
				Data:    exports,
				Context: modSession,
			},
		}

		session.ModuleCache[path] = res
		session.ValueStack.Push(res)
		return nil
	case OpPush:
		if v, ok := task.Data.(*Var); ok {
			session.ValueStack.Push(v)
		} else {
			session.ValueStack.Push(nil)
		}
		return nil
	default:
		return fmt.Errorf("unhandled opcode: %v", task.Op)
	}
}

func (e *Executor) handleExec(session *StackContext, stmt ast.Stmt, data interface{}) error {
	switch n := stmt.(type) {
	case *ast.BadStmt:
		return errors.New("cannot execute BadStmt: AST contains syntax errors")
	case *ast.BlockStmt:
		if !n.Inner {
			session.TaskStack = append(session.TaskStack, Task{Op: OpScopeExit})
		}
		for i := len(n.Children) - 1; i >= 0; i-- {
			session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n.Children[i], Data: data})
		}
		if !n.Inner {
			session.TaskStack = append(session.TaskStack, Task{Op: OpScopeEnter, Data: "block"})
		}
	case *ast.GenDeclStmt:
		if session.Stack.Depth == 1 && session.Stack.Scope == "global" {
			if _, ok := session.Stack.MemoryPtr[string(n.Name)]; ok {
				return nil
			}
		}
		return session.NewVar(string(n.Name), n.Kind)
	case *ast.AssignmentStmt:
		session.TaskStack = append(session.TaskStack, Task{Op: OpAssign})
		if data != nil {
			if v, ok := data.(*Var); ok {
				// 正确入栈顺序：OpAssign -> OpPush -> OpEvalLHS
				// 执行顺序：OpEvalLHS (压入 desc) -> OpPush (压入 val) -> OpAssign (弹 val, 弹 desc)
				session.TaskStack = append(session.TaskStack, Task{Op: OpPush, Data: v})
				session.TaskStack = append(session.TaskStack, Task{Op: OpEvalLHS, Node: n.LHS})
				return nil
			}
		}
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Value})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEvalLHS, Node: n.LHS})
	case *ast.MultiAssignmentStmt:
		session.TaskStack = append(session.TaskStack, Task{Op: OpMultiAssign, Data: len(n.LHS)})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Value})
		// push LHS in reverse order so they execute left-to-right
		for i := len(n.LHS) - 1; i >= 0; i-- {
			session.TaskStack = append(session.TaskStack, Task{Op: OpEvalLHS, Node: n.LHS[i]})
		}
	case *ast.IncDecStmt:
		session.TaskStack = append(session.TaskStack, Task{Op: OpIncDec, Data: string(n.Operator)})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEvalLHS, Node: n.Operand})
	case *ast.ReturnStmt:
		numResults := len(n.Results)
		session.TaskStack = append(session.TaskStack, Task{Op: OpReturn, Data: numResults})
		if numResults > 0 {
			// 倒序入栈，确保执行顺序为正序
			for i := numResults - 1; i >= 0; i-- {
				session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Results[i]})
			}
		}
	case *ast.InterruptStmt:
		session.TaskStack = append(session.TaskStack, Task{Op: OpInterrupt, Data: n.InterruptType})
	case *ast.IfStmt:
		session.TaskStack = append(session.TaskStack, Task{Op: OpBranchIf, Node: n})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Cond})
	case *ast.ForStmt:
		session.TaskStack = append(session.TaskStack, Task{Op: OpScopeExit})
		session.TaskStack = append(session.TaskStack, Task{Op: OpLoopBoundary, Node: n})
		if n.Init != nil {
			session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n.Init})
		}
		session.TaskStack = append(session.TaskStack, Task{Op: OpScopeEnter, Data: "for"})
	case *ast.RangeStmt:
		session.TaskStack = append(session.TaskStack, Task{Op: OpRangeInit, Node: n})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.X})
	case *ast.SwitchStmt:
		session.TaskStack = append(session.TaskStack, Task{Op: OpSwitchTag, Node: n})
		if n.Tag != nil {
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Tag})
		}
		if n.Init != nil {
			session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n.Init})
		}
	case *ast.TryStmt:
		if n.Finally != nil {
			session.TaskStack = append(session.TaskStack, Task{Op: OpFinally, Node: n.Finally})
		}
		if n.Catch != nil {
			session.TaskStack = append(session.TaskStack, Task{Op: OpCatchBoundary, Node: n.Catch})
		}
		session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n.Body})
	case *ast.DeferStmt:
		call := n.Call
		session.Stack.AddDefer(func() {
			if c, ok := call.(*ast.CallExprStmt); ok {
				if !c.GetBase().Type.IsVoid() {
					session.TaskStack = append(session.TaskStack, Task{Op: OpPop})
				}
				session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: c})
			}
		})
	case *ast.CallExprStmt:
		if !n.GetBase().Type.IsVoid() {
			session.TaskStack = append(session.TaskStack, Task{Op: OpPop})
		}
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n})
	}
	return nil
}

func (e *Executor) evalLHS(session *StackContext, lhsExpr ast.Expr) error {
	switch lhs := lhsExpr.(type) {
	case *ast.IdentifierExpr:
		session.ValueStack.Push(&Var{VType: TypeAny, Ref: &LHSEnv{Name: string(lhs.Name)}})
		return nil
	case *ast.IndexExpr:
		session.TaskStack = append(session.TaskStack, Task{Op: OpEvalLHSIndex})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: lhs.Index})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: lhs.Object})
		return nil
	case *ast.MemberExpr:
		session.TaskStack = append(session.TaskStack, Task{Op: OpEvalLHSMember, Data: string(lhs.Property)})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: lhs.Object})
		return nil
	case *ast.StarExpr:
		// Assignment to dereferenced pointer (*p = val)
		session.TaskStack = append(session.TaskStack, Task{Op: OpEvalLHSMember, Data: "__deref__"})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: lhs.X})
		return nil
	}
	return &VMError{Message: fmt.Sprintf("unsupported LHS in assignment: %T", lhsExpr), IsPanic: true}
}

func (e *Executor) scheduleForBody(session *StackContext, n *ast.ForStmt) {
	session.TaskStack = append(session.TaskStack, Task{Op: OpLoopBoundary, Node: n})
	if n.Update != nil {
		session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n.Update})
	}
	session.TaskStack = append(session.TaskStack, Task{Op: OpLoopContinue, Node: n})
	session.TaskStack = append(session.TaskStack, Task{Op: OpForScopeExit})
	session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: n.Body})
	session.TaskStack = append(session.TaskStack, Task{Op: OpForScopeEnter})
}

func (e *Executor) incDecLHSDesc(session *StackContext, lhsDesc interface{}, op string) error {
	switch desc := lhsDesc.(type) {
	case *LHSEnv:
		v, err := session.Load(desc.Name)
		if err != nil {
			return err
		}
		if v != nil {
			if op == "++" {
				v.I64++
			} else {
				v.I64--
			}
		}
		return nil
	case *LHSIndex:
		obj := desc.Obj
		idx := desc.Index
		if obj == nil || idx == nil {
			return nil
		}
		switch obj.VType {
		case TypeMap:
			m := obj.Ref.(*VMMap)
			key := idx.Str
			if idx.VType == TypeInt {
				key = strconv.FormatInt(idx.I64, 10)
			}
			if val, exists := m.Data[key]; exists && val != nil {
				if op == "++" {
					val.I64++
				} else {
					val.I64--
				}
			}
		case TypeArray:
			arr := obj.Ref.(*VMArray)
			i := int(idx.I64)
			if i >= 0 && i < len(arr.Data) {
				if val := arr.Data[i]; val != nil {
					if op == "++" {
						val.I64++
					} else {
						val.I64--
					}
				}
			}
		case TypeAny:
			if obj.Ref != nil {
				if m, ok := obj.Ref.(*VMMap); ok {
					key := idx.Str
					if idx.VType == TypeInt {
						key = strconv.FormatInt(idx.I64, 10)
					}
					if val, exists := m.Data[key]; exists && val != nil {
						if op == "++" {
							val.I64++
						} else {
							val.I64--
						}
					}
				} else if arr, ok := obj.Ref.(*VMArray); ok {
					i := int(idx.I64)
					if i >= 0 && i < len(arr.Data) {
						if val := arr.Data[i]; val != nil {
							if op == "++" {
								val.I64++
							} else {
								val.I64--
							}
						}
					}
				}
			}
		}
		return nil
	case *LHSMember:
		obj := desc.Obj
		if obj == nil {
			return nil
		}
		switch obj.VType {
		case TypeMap:
			m := obj.Ref.(*VMMap)
			if val, exists := m.Data[desc.Property]; exists && val != nil {
				if op == "++" {
					val.I64++
				} else {
					val.I64--
				}
			}
		case TypeAny:
			if obj.Ref != nil {
				if m, ok := obj.Ref.(*VMMap); ok {
					if val, exists := m.Data[desc.Property]; exists && val != nil {
						if op == "++" {
							val.I64++
						} else {
							val.I64--
						}
					}
				}
			}
		}
		return nil
	}
	return &VMError{Message: fmt.Sprintf("unsupported LHS descriptor: %T", lhsDesc), IsPanic: true}
}

func (e *Executor) assignToLHSDesc(session *StackContext, lhsDesc interface{}, val *Var) error {
	if lhsDesc == nil {
		return nil
	}
	switch desc := lhsDesc.(type) {
	case *LHSEnv:
		return session.Store(desc.Name, val)
	case *LHSIndex:
		obj := desc.Obj
		idx := desc.Index
		if obj == nil || idx == nil {
			return errors.New("assignment to nil object or index")
		}

		switch obj.VType {
		case TypeArray:
			arr := obj.Ref.(*VMArray)
			i := int(idx.I64)
			if i < 0 || i >= len(arr.Data) {
				return &VMError{Message: fmt.Sprintf("index out of range: %d", i), IsPanic: true}
			}
			arr.Data[i] = val
			return nil
		case TypeMap:
			m := obj.Ref.(*VMMap)
			key, err := e.varToMapKey(idx)
			if err != nil {
				return err
			}
			m.Data[key] = val
			return nil
		case TypeAny:
			if obj.Ref != nil {
				if m, ok := obj.Ref.(*VMMap); ok {
					key, err := e.varToMapKey(idx)
					if err != nil {
						return err
					}
					m.Data[key] = val
					return nil
				} else if arr, ok := obj.Ref.(*VMArray); ok {
					i := int(idx.I64)
					if i >= 0 && i < len(arr.Data) {
						arr.Data[i] = val
						return nil
					}
					return &VMError{Message: fmt.Sprintf("index out of range: %d", i), IsPanic: true}
				}
			}
		}
		return fmt.Errorf("type %v does not support index assignment", obj.VType)
	case *LHSMember:
		obj := desc.Obj
		if obj == nil {
			return errors.New("member assignment on nil object")
		}

		switch obj.VType {
		case TypeMap:
			m := obj.Ref.(*VMMap)
			m.Data[desc.Property] = val
			return nil
		case TypeModule:
			mod := obj.Ref.(*VMModule)
			if mod.Context == nil {
				return &VMError{Message: fmt.Sprintf("module %s is read-only", mod.Name), IsPanic: true}
			}
			return mod.Context.Store(desc.Property, val)
		case TypeHandle:
			if obj.Ref != nil {
				if valVar, ok := obj.Ref.(*Var); ok {
					if desc.Property == "__deref__" {
						copyVarData(valVar, val)
						return nil
					}
					return e.assignToLHSDesc(session, &LHSMember{Obj: valVar, Property: desc.Property}, val)
				}
			}
			return errors.New("type Handle does not support member assignment")
		case TypeAny:
			if obj.Ref != nil {
				if m, ok := obj.Ref.(*VMMap); ok {
					m.Data[desc.Property] = val
					return nil
				}
			}
			return errors.New("unsupported Any wrapper for member assignment")
		}
		return fmt.Errorf("type %v does not support member assignment", obj.VType)
	}
	return &VMError{Message: fmt.Sprintf("unsupported LHS descriptor: %T", lhsDesc), IsPanic: true}
}

func (e *Executor) handleEval(session *StackContext, expr ast.Expr) error {
	switch n := expr.(type) {
	case *ast.BadExpr:
		return errors.New("cannot evaluate BadExpr: AST contains syntax errors")
	case *ast.LiteralExpr:
		val, err := e.evalLiteralDirect(n)
		if err != nil {
			return err
		}
		session.ValueStack.Push(val)
	case *ast.IdentifierExpr:
		session.TaskStack = append(session.TaskStack, Task{Op: OpLoadVar, Data: string(n.Name)})
	case *ast.ConstRefExpr:
		if val, ok := e.program.Constants[string(n.Name)]; ok {
			session.ValueStack.Push(NewString(val))
		} else {
			return fmt.Errorf("const %s not found", n.Name)
		}
	case *ast.UnaryExpr:
		session.TaskStack = append(session.TaskStack, Task{Op: OpApplyUnary, Data: string(n.Operator)})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Operand})
	case *ast.BinaryExpr:
		op := string(n.Operator)
		if op == "&&" || op == "And" || op == "||" || op == "Or" {
			session.TaskStack = append(session.TaskStack, Task{Op: OpJumpIf, Node: n.Right, Data: op})
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Left})
		} else {
			session.TaskStack = append(session.TaskStack, Task{Op: OpApplyBinary, Data: op})
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Right})
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Left})
		}
	case *ast.IndexExpr:
		session.TaskStack = append(session.TaskStack, Task{Op: OpIndex, Node: n})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Index})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Object})
	case *ast.MemberExpr:
		session.TaskStack = append(session.TaskStack, Task{Op: OpMember, Data: string(n.Property)})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Object})
	case *ast.TypeAssertExpr:
		session.TaskStack = append(session.TaskStack, Task{Op: OpAssert, Node: n})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.X})
	case *ast.CompositeExpr:
		session.TaskStack = append(session.TaskStack, Task{Op: OpComposite, Node: n})
		for i := len(n.Values) - 1; i >= 0; i-- {
			v := n.Values[i]
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: v.Value})
			if v.Key != nil {
				if _, isIdent := v.Key.(*ast.IdentifierExpr); !isIdent {
					session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: v.Key})
				}
			}
		}
	case *ast.SliceExpr:
		session.TaskStack = append(session.TaskStack, Task{Op: OpSlice, Node: n})
		if n.High != nil {
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.High})
		}
		if n.Low != nil {
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Low})
		}
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.X})
	case *ast.StarExpr:
		// Dereference load
		session.TaskStack = append(session.TaskStack, Task{Op: OpApplyUnary, Data: "Dereference"})
		session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.X})
	case *ast.CallExprStmt:
		session.TaskStack = append(session.TaskStack, Task{Op: OpCall, Node: n})
		for i := len(n.Args) - 1; i >= 0; i-- {
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Args[i]})
		}
		if member, ok := n.Func.(*ast.MemberExpr); ok {
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: member.Object})
		} else if _, ok := n.Func.(*ast.IdentifierExpr); ok {
			// Extract name directly in OpCall
		} else if _, ok := n.Func.(*ast.ConstRefExpr); ok {
			// Extract name directly in OpCall
		} else {
			session.TaskStack = append(session.TaskStack, Task{Op: OpEval, Node: n.Func})
		}
	case *ast.FuncLitExpr:
		clCtx := &StackContext{
			Context:        session.Context,
			Executor:       session.Executor,
			Stack:          session.Stack,
			StepLimit:      session.StepLimit,
			ModuleCache:    session.ModuleCache,
			LoadingModules: session.LoadingModules,
			ActiveHandles:  session.ActiveHandles,
			Debugger:       session.Debugger,
		}
		closure := &VMClosure{
			FuncDef:  n,
			Upvalues: make(map[string]*Var),
			Context:  clCtx,
		}
		for _, name := range n.CaptureNames {
			cellVar, err := session.CaptureVar(name)
			if err != nil {
				return fmt.Errorf("failed to capture variable %s: %w", name, err)
			}
			closure.Upvalues[name] = cellVar
		}
		v := NewVar(ast.TypeClosure, TypeClosure)
		v.Ref = closure
		session.ValueStack.Push(v)
	case *ast.ImportExpr:
		session.TaskStack = append(session.TaskStack, Task{Op: OpImportInit, Node: n})
	}
	return nil
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

		if task.Op == OpExec {
			session.StepCount++
			if session.StepLimit > 0 {
				if session.StepCount > session.StepLimit {
					return fmt.Errorf("instruction limit exceeded (%d)", session.StepLimit)
				}
			}
			// Use high-performance internal signaling (Fake Context)
			if session.Aborted() {
				return session.Err()
			}
			if session.Debugger != nil {
				loc := task.Node.GetBase().Loc
				if loc != nil && session.Debugger.ShouldTrigger(loc.L) {
					session.Debugger.SetStepping(false)
					session.Debugger.EventChan <- &debugger.Event{
						Loc:       loc,
						Variables: session.Stack.DumpVariables(),
					}
					cmd := <-session.Debugger.CommandChan
					if cmd == debugger.CmdStepInto {
						session.Debugger.SetStepping(true)
					}
				}
			}
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

func (e *Executor) GetProgram() *ast.ProgramStmt {
	return e.program
}

func (e *Executor) ExecuteStmts(session *StackContext, stmts []ast.Stmt) error {
	oldTasks := session.TaskStack
	oldValues := session.ValueStack
	oldUnwind := session.UnwindMode

	session.TaskStack = []Task{}
	session.ValueStack = &ValueStack{}
	session.UnwindMode = UnwindNone
	if session.ActiveHandles == nil {
		session.ActiveHandles = &HandleTracker{Handles: make([]HandleRef, 0, 64)}
	}
	if session.ModuleCache == nil {
		session.ModuleCache = make(map[string]*Var)
	}
	if session.LoadingModules == nil {
		session.LoadingModules = make(map[string]bool)
	}

	for i := len(stmts) - 1; i >= 0; i-- {
		session.TaskStack = append(session.TaskStack, Task{Op: OpExec, Node: stmts[i]})
	}

	err := e.Run(session)

	session.TaskStack = oldTasks
	session.ValueStack = oldValues
	session.UnwindMode = oldUnwind
	return err
}

func (e *Executor) ImportModule(ctx *StackContext, n *ast.ImportExpr) (*Var, error) {
	oldTasks := ctx.TaskStack
	oldValues := ctx.ValueStack
	oldUnwind := ctx.UnwindMode

	ctx.TaskStack = []Task{{Op: OpImportInit, Node: n}}
	ctx.ValueStack = &ValueStack{}
	ctx.UnwindMode = UnwindNone
	if ctx.ActiveHandles == nil {
		ctx.ActiveHandles = &HandleTracker{Handles: make([]HandleRef, 0, 64)}
	}
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
	ctx.UnwindMode = oldUnwind
	return res, err
}
