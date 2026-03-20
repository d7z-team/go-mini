package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strconv"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/debugger"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type Executor struct {
	structs map[string]*ast.StructStmt
	consts  map[string]string
	funcs   map[ast.Ident]*Var
	program *ast.ProgramStmt

	routes map[string]FFIRoute // 显式映射外部函数名到 Bridge

	Loader func(path string) (*ast.ProgramStmt, error)

	StepLimit int64
}

type MonitorManager interface {
	ReportProgram(state, message string, duration int)
	ReportStep(state string, meta ast.BaseNode, duration int)
}

type MiniRuntimeError struct {
	BaseNode ast.BaseNode
	Err      error
}

type PanicError struct {
	Value *Var
}

func (p *PanicError) Error() string {
	if p.Value != nil && p.Value.VType == TypeString {
		return "panic: " + p.Value.Str
	}
	return "panic"
}

func (e *MiniRuntimeError) Error() string {
	if e.Err == nil {
		return "unknown error"
	}
	return e.Err.Error()
}

func (e *MiniRuntimeError) Unwrap() error {
	return e.Err
}

func NewExecutor(program *ast.ProgramStmt) (*Executor, error) {
	if program == nil {
		return nil, errors.New("invalid program")
	}
	result := &Executor{
		program: program,
		structs: make(map[string]*ast.StructStmt),
		funcs:   make(map[ast.Ident]*Var),
		consts:  make(map[string]string),
		routes:  make(map[string]FFIRoute),
	}
	for ident, stmt := range program.Structs {
		result.structs[string(ident)] = stmt
	}
	for s, s2 := range program.Constants {
		result.consts[s] = s2
	}
	return result, nil
}

func (e *Executor) RegisterRoute(name string, route FFIRoute) {
	e.routes[name] = route
}

func (e *Executor) Execute(ctx context.Context) (err error) {
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
		Debugger:       debugger.GetDebugger(ctx),
	}

	defer func() {
		// Clean up all active handles to prevent memory leaks on VM exit
		for _, h := range session.ActiveHandles {
			if h.Bridge != nil && h.ID != 0 {
				_ = h.Bridge.DestroyHandle(h.ID)
			}
		}
	}()

	// 注入内建 nil
	_ = session.AddVariable("nil", nil)

	// 初始化全局变量
	for name, expr := range e.program.Variables {
		val, err := e.ExecExpr(session, expr)
		if err != nil {
			return fmt.Errorf("failed to initialize global var %s: %w", name, err)
		}
		_ = session.AddVariable(string(name), val)
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error("Executor panic", "error", r, "stack", string(debug.Stack()))
			// Only set err if it's currently nil to avoid overriding intentional script errors
			if err == nil {
				if errRec, ok := r.(error); ok {
					err = errRec
				} else {
					err = fmt.Errorf("panic: %v", r)
				}
			}
		}
	}()

	// 1. 执行顶级语句 (Main)
	if err = e.ExecuteStmts(session, e.program.Main); err != nil {
		targetErr := err
		for {
			if mErr, ok := targetErr.(*MiniRuntimeError); ok {
				targetErr = mErr.Err
			} else {
				break
			}
		}
		if pErr, ok := targetErr.(*PanicError); ok {
			session.PanicVar = pErr.Value
			err = pErr
		}
	}
	session.Stack.RunDefers()
	if err != nil {
		return err
	}

	// 2. 自动寻找并执行 main() 入口函数
	if f, ok := e.program.Functions["main"]; ok {
		err = session.WithFuncScope("main", func(old *Stack, c *StackContext) (innerErr error) {
			defer func() {
				c.Stack.RunDefers()
				if c.PanicVar != nil {
					innerErr = &PanicError{Value: c.PanicVar}
				}
			}()
			c.Executor = e
			for _, p := range f.Params {
				_ = c.NewVar(string(p.Name), p.Type)
			}
			execErr := e.ExecuteStmts(c, f.Body.Children)
			if execErr != nil {
				targetErr := execErr
				for {
					if mErr, ok := targetErr.(*MiniRuntimeError); ok {
						targetErr = mErr.Err
					} else {
						break
					}
				}
				if pErr, ok := targetErr.(*PanicError); ok {
					c.PanicVar = pErr.Value
				} else {
					innerErr = execErr
				}
			}
			return innerErr
		})
		if err != nil {
			return err
		}
	}

	return err
}

func (e *Executor) ExecuteStmts(ctx *StackContext, children []ast.Stmt) error {
	for _, child := range children {
		// 检查指令限制
		if ctx.StepLimit > 0 {
			ctx.StepCount++
			if ctx.StepCount > ctx.StepLimit {
				return fmt.Errorf("instruction limit exceeded (%d)", ctx.StepLimit)
			}
		}

		// 检查 Context 是否已取消
		if err := ctx.Context.Err(); err != nil {
			return err
		}

		// --- Debugger Hook ---
		if ctx.Debugger != nil {
			loc := child.GetBase().Loc
			if loc != nil {
				if ctx.Debugger.ShouldTrigger(loc.L) {
					ctx.Debugger.SetStepping(false)
					ctx.Debugger.EventChan <- &debugger.Event{
						Loc:       loc,
						Variables: ctx.Stack.DumpVariables(),
					}
					cmd := <-ctx.Debugger.CommandChan
					if cmd == debugger.CmdStepInto {
						ctx.Debugger.SetStepping(true)
					}
				}
			}
		}
		// --- Debugger Hook 结束 ---

		if ctx.Interrupt() {
			return nil
		}
		if err := e.execStmt(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) execStmt(ctx *StackContext, s ast.Stmt) (err error) {
	defer func() {
		if err != nil {
			err = &MiniRuntimeError{Err: err, BaseNode: *s.GetBase()}
		}
	}()

	switch n := s.(type) {
	case *ast.BadStmt:
		return errors.New("cannot execute BadStmt: AST contains syntax errors")
	case *ast.BlockStmt:
		if n.Inner {
			return e.ExecuteStmts(ctx, n.Children)
		}
		ctx.WithScope("block", func(ctx *StackContext) {
			err = e.ExecuteStmts(ctx, n.Children)
		})
		return err
	case *ast.GenDeclStmt:
		return ctx.NewVar(string(n.Name), n.Kind)
	case *ast.AssignmentStmt:
		val, err := e.ExecExpr(ctx, n.Value)
		if err != nil {
			return err
		}
		return e.assignToLHS(ctx, n.LHS, val)

	case *ast.MultiAssignmentStmt:
		val, err := e.ExecExpr(ctx, n.Value)
		if err != nil {
			return err
		}
		if val == nil {
			return errors.New("multi assignment: RHS evaluated to nil")
		}

		var elements []*Var
		switch val.VType {
		case TypeArray:
			elements = val.Ref.(*VMArray).Data
		case TypeResult:
			// [val, err]
			errVar := NewString(val.ResultErr)
			if val.ResultErr == "" {
				errVar = nil // Use nil for success
			}
			elements = []*Var{val.ResultVal, errVar}
		default:
			return fmt.Errorf("cannot destructure type %v", val.VType)
		}

		if len(elements) < len(n.LHS) {
			return fmt.Errorf("multi assignment: not enough elements to destructure (need %d, got %d)", len(n.LHS), len(elements))
		}

		for i, lhsExpr := range n.LHS {
			item := elements[i]
			if err := e.assignToLHS(ctx, lhsExpr, item); err != nil {
				return err
			}
		}
		return nil
	case *ast.IfStmt:

		cond, err := e.ExecExpr(ctx, n.Cond)
		if err != nil {
			return err
		}
		if cond != nil && cond.Bool {
			return e.execStmt(ctx, n.Body)
		} else if n.ElseBody != nil {
			return e.execStmt(ctx, n.ElseBody)
		}
		return nil
	case *ast.ForStmt:
		var forErr error
		ctx.WithScope("for", func(ctx *StackContext) {
			if n.Init != nil {
				if initErr := e.execStmt(ctx, n.Init.(ast.Stmt)); initErr != nil {
					forErr = initErr
					return
				}
			}
			for {
				// 增加 Context 检查
				if ctxErr := ctx.Context.Err(); ctxErr != nil {
					forErr = ctxErr
					return
				}

				if n.Cond != nil {
					cond, execErr := e.ExecExpr(ctx, n.Cond)
					if execErr != nil || cond == nil || !cond.Bool {
						break
					}
				}

				// Go 1.22 语义：每次迭代创建一个逻辑上的新变量副本
				// 这样闭包捕获的就是当前迭代的值
				var bodyErr error

				// 预先收集父级作用域的变量，因为 WithScope 会修改 ctx.Stack
				parentVars := make(map[string]*Var)
				for k, v := range ctx.Stack.MemoryPtr {
					parentVars[k] = v
				}

				ctx.WithScope("for_body", func(bodyCtx *StackContext) {
					// 1. 从父级作用域（for init）拷贝当前变量的纯值快照
					// 注意：即便父级变量已经是 TypeCell，我们这里也只取其 Value，
					// 从而切断与父级 Cell 的联系，让 bodyCtx 能独立升格。
					for k, v := range parentVars {
						val := v
						if v != nil && v.VType == TypeCell {
							val = v.Ref.(*Cell).Value
						}
						_ = bodyCtx.AddVariable(k, val)
					}

					// 2. 执行主体
					bodyErr = e.ExecuteStmts(bodyCtx, n.Body.(*ast.BlockStmt).Children)
					if bodyErr == nil && bodyCtx.Interrupt() {
						// bodyCtx.Stack is currently top, its parent is the for scope
						bodyCtx.Stack.Parent.interrupt = bodyCtx.Stack.interrupt
					}

					// 3. 同步回父级。
					// 只有将修改后的值同步回父级，Update 语句 (i++) 才能作用于正确的当前值，
					// 并带入下一次迭代。
					for k, v := range bodyCtx.Stack.MemoryPtr {
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
				})

				if bodyErr != nil {
					forErr = bodyErr
					break
				}
				if ctx.Interrupt() {
					if ctx.Stack.interrupt == "break" {
						ctx.Stack.interrupt = ""
						break
					}
					if ctx.Stack.interrupt == "continue" {
						ctx.Stack.interrupt = ""
					} else {
						break
					}
				}
				if n.Update != nil {
					if updateErr := e.execStmt(ctx, n.Update.(ast.Stmt)); updateErr != nil {
						forErr = updateErr
						break
					}
				}
			}
		})
		return forErr
	case *ast.RangeStmt:
		obj, err := e.ExecExpr(ctx, n.X)
		if err != nil {
			return err
		}
		if obj == nil {
			return nil
		}
		return e.evalRangeStmt(ctx, n, obj)
	case *ast.SwitchStmt:
		return e.evalSwitchStmt(ctx, n)
	case *ast.DeferStmt:
		call := n.Call
		ctx.Stack.AddDefer(func() {
			if c, ok := call.(*ast.CallExprStmt); ok {
				_, _ = e.evalCallExpr(ctx, c)
			}
		})
		return nil
	case *ast.TryStmt:
		err = e.execStmt(ctx, n.Body)
		if err != nil {
			// Unwrapping the error to check if it's a PanicError
			targetErr := err
			for {
				if mErr, ok := targetErr.(*MiniRuntimeError); ok {
					targetErr = mErr.Err
				} else {
					break
				}
			}

			if pErr, ok := targetErr.(*PanicError); ok {
				// 成功捕获异常后，清理全局的 PanicVar，防止泄漏给外层环境
				ctx.PanicVar = nil

				if n.Catch != nil {
					ctx.WithScope("catch", func(catchCtx *StackContext) {
						if n.Catch.VarName != "" {
							_ = catchCtx.NewVar(string(n.Catch.VarName), "Any")
							_ = catchCtx.Store(string(n.Catch.VarName), pErr.Value)
						}
						err = e.ExecuteStmts(catchCtx, n.Catch.Body.Children)
					})
				}
			}
		}

		if n.Finally != nil {
			finallyErr := e.execStmt(ctx, n.Finally)
			if finallyErr != nil {
				err = finallyErr
			}
		}
		return err
	case *ast.InterruptStmt:
		if ctx.Stack != nil {
			ctx.Stack.interrupt = n.InterruptType
		}
		return nil
	case *ast.ReturnStmt:
		_ = ctx.SetInterrupt("function", "return")
		if len(n.Results) > 0 {
			res, err := e.ExecExpr(ctx, n.Results[0])
			if err == nil && res != nil {
				_ = ctx.Store("__return__", res)
			}
		}
		return nil
	case *ast.CallExprStmt:
		_, err := e.ExecExpr(ctx, n)
		return err
	case *ast.IncDecStmt:
		if ident, ok := n.Operand.(*ast.IdentifierExpr); ok {
			v, _ := ctx.Load(string(ident.Name))
			if v != nil {
				if n.Operator == "++" {
					v.I64++
				} else {
					v.I64--
				}
			}
		} else if member, ok := n.Operand.(*ast.MemberExpr); ok {
			obj, err := e.ExecExpr(ctx, member.Object)
			if err == nil && obj != nil {
				switch obj.VType {
				case TypeMap:
					m := obj.Ref.(*VMMap)
					if val, exists := m.Data[string(member.Property)]; exists && val != nil {
						if n.Operator == "++" {
							val.I64++
						} else {
							val.I64--
						}
					}
				case TypeAny:
					if obj.Ref != nil {
						if m, ok2 := obj.Ref.(*VMMap); ok2 {
							if val, exists := m.Data[string(member.Property)]; exists && val != nil {
								if n.Operator == "++" {
									val.I64++
								} else {
									val.I64--
								}
							}
						}
					}
				}
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported stmt: %T", s)
}

func (e *Executor) assignToLHS(ctx *StackContext, lhsExpr ast.Expr, val *Var) error {
	switch lhs := lhsExpr.(type) {
	case *ast.IdentifierExpr:
		return ctx.Store(string(lhs.Name), val)
	case *ast.IndexExpr:
		obj, err := e.ExecExpr(ctx, lhs.Object)
		if err != nil {
			return err
		}
		idx, err := e.ExecExpr(ctx, lhs.Index)
		if err != nil {
			return err
		}
		if obj == nil || idx == nil {
			return errors.New("assignment to nil object or index")
		}

		switch obj.VType {
		case TypeArray:
			arr := obj.Ref.(*VMArray)
			i := int(idx.I64)
			if i < 0 || i >= len(arr.Data) {
				return fmt.Errorf("index out of range: %d", i)
			}
			arr.Data[i] = val
			return nil
		case TypeMap:
			m := obj.Ref.(*VMMap)
			key := idx.Str
			if idx.VType == TypeInt {
				key = strconv.FormatInt(idx.I64, 10)
			}
			m.Data[key] = val
			return nil
		}
		return fmt.Errorf("type %v does not support index assignment", obj.VType)

	case *ast.MemberExpr:
		obj, err := e.ExecExpr(ctx, lhs.Object)
		if err != nil {
			return err
		}
		if obj == nil {
			return errors.New("member assignment on nil object")
		}

		switch obj.VType {
		case TypeMap:
			m := obj.Ref.(*VMMap)
			m.Data[string(lhs.Property)] = val
			return nil
		case TypeModule:
			mod := obj.Ref.(*VMModule)
			if mod.Context == nil {
				return fmt.Errorf("module %s is read-only", mod.Name)
			}
			// 必须通过模块的 Store 逻辑进行更新，以确保能够正确处理 TypeCell 并同步到模块内部
			return mod.Context.Store(string(lhs.Property), val)
		case TypeAny:
			if obj.Ref != nil {
				if m, ok := obj.Ref.(*VMMap); ok {
					m.Data[string(lhs.Property)] = val
					return nil
				}
			}
			return errors.New("unsupported Any wrapper for member assignment")
		}
		return fmt.Errorf("type %v does not support member assignment", obj.VType)
	}
	return fmt.Errorf("unsupported LHS in assignment: %T", lhsExpr)
}

func (e *Executor) ExecExpr(ctx *StackContext, s ast.Expr) (v *Var, err error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if s == nil {
		return nil, nil
	}
	switch n := s.(type) {
	case *ast.BadExpr:
		return nil, errors.New("cannot evaluate BadExpr: AST contains syntax errors")
	case *ast.LiteralExpr:
		return e.evalLiteral(n)
	case *ast.IdentifierExpr:
		return ctx.Load(string(n.Name))
	case *ast.BinaryExpr:
		// 逻辑运算短路处理
		op := string(n.Operator)
		if op == "&&" || op == "And" || op == "||" || op == "Or" {
			l, err := e.ExecExpr(ctx, n.Left)
			if err != nil {
				return nil, err
			}
			lb, _ := l.ToBool()
			if op == "&&" || op == "And" {
				if !lb {
					return NewBool(false), nil
				}
			} else {
				if lb {
					return NewBool(true), nil
				}
			}
			r, err := e.ExecExpr(ctx, n.Right)
			if err != nil {
				return nil, err
			}
			rb, _ := r.ToBool()
			return NewBool(rb), nil
		}

		l, _ := e.ExecExpr(ctx, n.Left)
		r, _ := e.ExecExpr(ctx, n.Right)
		return e.evalBinaryExprDirect(op, l, r)
	case *ast.UnaryExpr:
		val, _ := e.ExecExpr(ctx, n.Operand)
		return e.evalUnaryExprDirect(string(n.Operator), val)
	case *ast.ConstRefExpr:
		val, ok := e.program.Constants[string(n.Name)]
		if !ok {
			return nil, fmt.Errorf("const %s not found", n.Name)
		}
		return NewString(val), nil
	case *ast.CallExprStmt:
		return e.evalCallExpr(ctx, n)
	case *ast.CompositeExpr:
		return e.evalCompositeExpr(ctx, n)
	case *ast.MemberExpr:
		return e.evalMemberExpr(ctx, n)
	case *ast.IndexExpr:
		return e.evalIndexExpr(ctx, n)
	case *ast.SliceExpr:
		return e.evalSliceExpr(ctx, n)
	case *ast.ImportExpr:
		return e.ImportModule(ctx, n)
	case *ast.FuncLitExpr:
		return e.evalFuncLit(ctx, n)
	}
	return nil, fmt.Errorf("unsupported expression type: %T", s)
}

func (e *Executor) evalFuncLit(ctx *StackContext, n *ast.FuncLitExpr) (*Var, error) {
	closure := &VMClosure{
		FuncDef:  n,
		Upvalues: make(map[string]*Var),
		Context:  ctx, // 记录创建时的上下文
	}

	for _, name := range n.CaptureNames {
		cellVar, err := ctx.CaptureVar(name)
		if err != nil {
			return nil, fmt.Errorf("failed to capture variable %s: %w", name, err)
		}
		closure.Upvalues[name] = cellVar
	}

	v := NewVar(ast.TypeClosure, TypeClosure)
	v.Ref = closure
	return v, nil
}

func (e *Executor) ImportModule(ctx *StackContext, n *ast.ImportExpr) (*Var, error) {
	// 1. 路径规范化，防止 "../" 注入
	path := strings.Trim(n.Path, " \t\n\r")
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("invalid import path: %s", path)
	}

	if v, ok := ctx.ModuleCache[path]; ok {
		return v, nil
	}

	if ctx.LoadingModules[path] {
		return nil, fmt.Errorf("circular dependency detected: %s", path)
	}

	// 2. Try script loader
	if e.Loader != nil {
		prog, err := e.Loader(path)
		if err == nil {
			ctx.LoadingModules[path] = true
			defer func() { delete(ctx.LoadingModules, path) }()

			// Execute module
			modExecutor, err := NewExecutor(prog)
			if err != nil {
				return nil, err
			}
			modExecutor.Loader = e.Loader
			modExecutor.routes = e.routes

			modSession := &StackContext{
				Context:        ctx.Context,
				Executor:       modExecutor,
				Stack:          &Stack{MemoryPtr: make(map[string]*Var), Scope: "global", Depth: 1},
				StepLimit:      ctx.StepLimit,
				StepCount:      ctx.StepCount,
				ModuleCache:    ctx.ModuleCache,
				LoadingModules: ctx.LoadingModules,
				ActiveHandles:  make([]HandleRef, 0),
			}

			// 1. 初始化模块全局变量
			for name, expr := range prog.Variables {
				val, err := modExecutor.ExecExpr(modSession, expr)
				if err != nil {
					return nil, fmt.Errorf("failed to initialize module var %s: %w", name, err)
				}
				_ = modSession.AddVariable(string(name), val)
			}

			// 2. 执行模块顶级语句
			err = modExecutor.ExecuteStmts(modSession, prog.Main)
			// Sync step count back
			ctx.StepCount = modSession.StepCount

			// 修复：将子模块产生的句柄合并到主上下文，由主上下文在结束时统一强制清理
			ctx.ActiveHandles = append(ctx.ActiveHandles, modSession.ActiveHandles...)

			if err != nil {
				return nil, fmt.Errorf("module %s execution failed: %w", path, err)
			}

			// Collect exports
			exports := make(map[string]*Var)
			// 导出所有符合规则的 Ident (首字母大写)
			// 同时检查变量、常量和函数
			for name := range prog.Variables {
				if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
					v, err := modSession.Load(string(name))
					if err == nil {
						exports[string(name)] = v
					}
				}
			}
			// 导出函数 (作为闭包)
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
							Context:  modSession, // 绑定模块上下文
						},
					}
				}
			}
			// 导出常量
			for name, val := range prog.Constants {
				if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
					exports[name] = NewString(val)
				}
			}
			// 导出结构体定义
			for name, s := range prog.Structs {
				if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
					exports[string(name)] = &Var{
						VType: TypeAny,
						Ref:   s,
					}
				}
			}

			res := &Var{
				VType: TypeModule,
				Ref: &VMModule{
					Name:    path,
					Data:    exports,
					Context: modSession, // 锚定模块上下文
				},
			}
			ctx.ModuleCache[path] = res
			return res, nil
		}
	}

	// 3. Fallback to FFI Virtual Module
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
		ctx.ModuleCache[path] = res
		return res, nil
	}

	return nil, fmt.Errorf("failed to load module %s", path)
}

func (e *Executor) evalSliceExpr(ctx *StackContext, n *ast.SliceExpr) (*Var, error) {
	obj, err := e.ExecExpr(ctx, n.X)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, errors.New("slice on nil object")
	}

	low, high := 0, -1
	if n.Low != nil {
		l, err := e.ExecExpr(ctx, n.Low)
		if err != nil {
			return nil, err
		}
		if l != nil && l.VType == TypeInt {
			low = int(l.I64)
		}
	}
	if n.High != nil {
		h, err := e.ExecExpr(ctx, n.High)
		if err != nil {
			return nil, err
		}
		if h != nil && h.VType == TypeInt {
			high = int(h.I64)
		}
	}

	switch obj.VType {
	case TypeBytes:
		l := len(obj.B)
		if high == -1 {
			high = l
		}
		if low < 0 || high < low || high > l {
			return nil, fmt.Errorf("slice bounds out of range [%d:%d] with capacity %d", low, high, l)
		}
		return NewBytes(obj.B[low:high]), nil
	case TypeString:
		l := len(obj.Str)
		if high == -1 {
			high = l
		}
		if low < 0 || high < low || high > l {
			return nil, fmt.Errorf("slice bounds out of range [%d:%d] with capacity %d", low, high, l)
		}
		return NewString(obj.Str[low:high]), nil
	case TypeArray:
		arr := obj.Ref.(*VMArray)
		l := len(arr.Data)
		if high == -1 {
			high = l
		}
		if low < 0 || high < low || high > l {
			return nil, fmt.Errorf("slice bounds out of range [%d:%d] with capacity %d", low, high, l)
		}
		return &Var{VType: TypeArray, Ref: &VMArray{Data: arr.Data[low:high]}, Type: obj.Type}, nil
	}
	return nil, fmt.Errorf("type %v does not support slice operations", obj.VType)
}

func (e *Executor) evalIndexExpr(ctx *StackContext, n *ast.IndexExpr) (*Var, error) {
	obj, err := e.ExecExpr(ctx, n.Object)
	if err != nil {
		return nil, err
	}
	idx, err := e.ExecExpr(ctx, n.Index)
	if err != nil {
		return nil, err
	}

	if obj == nil || (obj.VType == TypeAny && obj.Ref == nil) {
		// Go 语义：访问 nil map 返回零值
		if obj != nil {
			return e.ToVar(ctx, obj.Type.ZeroVar(), nil), nil
		}
		return nil, errors.New("index access on nil")
	}

	if idx == nil {
		return nil, errors.New("index access with nil index")
	}

	switch obj.VType {
	case TypeArray:
		arr := obj.Ref.(*VMArray)
		i := int(idx.I64)
		if i < 0 || i >= len(arr.Data) {
			return nil, fmt.Errorf("index out of range: %d", i)
		}
		return arr.Data[i], nil
	case TypeMap:
		m := obj.Ref.(*VMMap)
		key := idx.Str
		if idx.VType == TypeInt {
			key = strconv.FormatInt(idx.I64, 10)
		}
		if val, ok := m.Data[key]; ok {
			return val, nil
		}
		// 返回该类型的默认零值
		_, valType, _ := obj.Type.GetMapKeyValueTypes()
		return e.ToVar(ctx, valType.ZeroVar(), nil), nil
	}
	return nil, fmt.Errorf("type %v does not support indexing", obj.VType)
}

func (e *Executor) evalMemberExpr(ctx *StackContext, n *ast.MemberExpr) (*Var, error) {
	obj, err := e.ExecExpr(ctx, n.Object)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, errors.New("member access on nil object")
	}

	switch obj.VType {
	case TypeResult:
		if n.Property == "val" {
			return obj.ResultVal, nil
		}
		if n.Property == "err" {
			if obj.ResultErr == "" {
				return nil, nil // Represent success as nil error
			}
			return NewString(obj.ResultErr), nil
		}
	case TypeMap:
		m := obj.Ref.(*VMMap)
		if val, ok := m.Data[string(n.Property)]; ok {
			return val, nil
		}
		return nil, nil
	case TypeModule:
		mod := obj.Ref.(*VMModule)
		// 优先从模块执行上下文中实时加载，以处理闭包捕获和跨模块变量状态同步
		if mod.Context != nil {
			val, err := mod.Context.Load(string(n.Property))
			if err == nil {
				return val, nil
			}
		}
		// 回退到导出快照（用于访问函数、常量和结构体定义）
		if val, ok := mod.Data[string(n.Property)]; ok {
			return val, nil
		}
		return nil, fmt.Errorf("module member %s not found in %s", n.Property, mod.Name)
	case TypeAny:
		// 支持动态成员访问
		if obj.Ref != nil {
			if m, ok := obj.Ref.(*VMMap); ok {
				if val, ok := m.Data[string(n.Property)]; ok {
					return val, nil
				}
			}
		}
		return nil, nil
	}

	// 方法提取支持 (Method Value)
	tName := string(obj.Type)
	tName = strings.TrimPrefix(tName, "Ptr<")
	tName = strings.TrimPrefix(tName, "*")
	tName = strings.TrimSuffix(tName, ">")
	methodName := fmt.Sprintf("__method_%s_%s", tName, n.Property)

	if _, ok := e.routes[methodName]; ok {
		return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: obj, Method: methodName}}, nil
	}

	return nil, fmt.Errorf("type %v does not support member access: %s", obj.VType, n.Property)
}

func (e *Executor) evalCompositeExpr(ctx *StackContext, n *ast.CompositeExpr) (*Var, error) {
	isArray := n.Type.IsArray()
	isMap := n.Type.IsMap()

	if isArray {
		elemType, _ := n.Type.ReadArrayItemType()
		res := make([]*Var, len(n.Values))
		for i, v := range n.Values {
			val, err := e.ExecExpr(ctx, v.Value)
			if err != nil {
				return nil, err
			}
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
		return &Var{VType: TypeArray, Ref: &VMArray{Data: res}, Type: n.Type}, nil
	}

	// 结构体或 Map 字面量
	res := make(map[string]*Var)
	var valType ast.GoMiniType
	if isMap {
		_, valType, _ = n.Type.GetMapKeyValueTypes()
	}

	for _, v := range n.Values {
		keyName := ""
		if v.Key != nil {
			if ident, ok := v.Key.(*ast.IdentifierExpr); ok {
				keyName = string(ident.Name)
			} else {
				// 支持计算型 Key，如 { ["a"+"b"]: 1 }
				kVar, err := e.ExecExpr(ctx, v.Key)
				if err != nil {
					return nil, err
				}
				keyName = kVar.Str
				if kVar.VType == TypeInt {
					keyName = strconv.FormatInt(kVar.I64, 10)
				}
			}
		}

		val, err := e.ExecExpr(ctx, v.Value)
		if err != nil {
			return nil, err
		}
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
		res[keyName] = val
	}

	return &Var{VType: TypeMap, Ref: &VMMap{Data: res}, Type: n.Type}, nil
}

func (e *Executor) evalLiteral(n *ast.LiteralExpr) (*Var, error) {
	switch n.Type {
	case "Int64":
		v, _ := strconv.ParseInt(n.Value, 10, 64)
		return NewInt(v), nil
	case "Float64":
		v, _ := strconv.ParseFloat(n.Value, 64)
		return NewFloat(v), nil
	case "String":
		return NewString(n.Value), nil
	case "Bool":
		return NewBool(n.Value == "true"), nil
	}
	return nil, fmt.Errorf("unknown literal %s", n.Type)
}

func (e *Executor) evalBinaryExprDirect(operator string, l, r *Var) (*Var, error) {
	// 解包 Cell
	if l != nil && l.VType == TypeCell {
		l = l.Ref.(*Cell).Value
	}
	if r != nil && r.VType == TypeCell {
		r = r.Ref.(*Cell).Value
	}

	// 允许比较运算的操作数为 nil
	if operator == "==" || operator == "Eq" || operator == "!=" || operator == "Neq" {
		isLEmpty := isEmptyVar(l)
		isREmpty := isEmptyVar(r)

		if isLEmpty && isREmpty {
			return NewBool(operator == "==" || operator == "Eq"), nil
		}
		if isLEmpty || isREmpty {
			return NewBool(operator == "!=" || operator == "Neq"), nil
		}
	}

	if l == nil || r == nil {
		return nil, errors.New("binary op with nil operand")
	}

	// 路由到专门的处理函数
	switch operator {
	case "+", "Plus", "Add", "-", "Minus", "Sub", "*", "Mult", "/", "Div", "%", "Mod":
		return e.evalArithmetic(operator, l, r)
	case "&", "BitAnd", "|", "BitOr", "^", "BitXor", "<<", "Lsh", ">>", "Rsh":
		return e.evalBitwise(operator, l, r)
	case "==", "Eq", "!=", "Neq", "<", "Lt", ">", "Gt", "<=", "Le", ">=", "Ge":
		return e.evalComparison(operator, l, r)
	case "&&", "And", "||", "Or":
		return e.evalLogic(operator, l, r)
	}

	return nil, fmt.Errorf("unsupported operator: %s", operator)
}

func (e *Executor) evalArithmetic(op string, l, r *Var) (*Var, error) {
	if l.VType != TypeInt && l.VType != TypeFloat {
		// 特殊处理字符串/字节切片拼接
		if (op == "+" || op == "Plus" || op == "Add") && (l.VType == TypeString || l.VType == TypeBytes) {
			lStr := l.Str
			if l.VType == TypeBytes {
				lStr = string(l.B)
			}
			if r.VType != TypeString && r.VType != TypeBytes {
				return nil, fmt.Errorf("cannot concatenate %v to %v", r.VType, l.VType)
			}
			rStr := r.Str
			if r.VType == TypeBytes {
				rStr = string(r.B)
			}
			return NewString(lStr + rStr), nil
		}
		return nil, fmt.Errorf("arithmetic operation %s on non-numeric type %v", op, l.VType)
	}
	if r.VType != TypeInt && r.VType != TypeFloat {
		return nil, fmt.Errorf("arithmetic operation %s on non-numeric type %v", op, r.VType)
	}

	lf, _ := l.ToFloat()
	rf, _ := r.ToFloat()
	useFloat := l.VType == TypeFloat || r.VType == TypeFloat

	switch op {
	case "+", "Plus", "Add":
		if useFloat {
			return NewFloat(lf + rf), nil
		}
		return NewInt(l.I64 + r.I64), nil
	case "-", "Minus", "Sub":
		if useFloat {
			return NewFloat(lf - rf), nil
		}
		return NewInt(l.I64 - r.I64), nil
	case "*", "Mult":
		if useFloat {
			return NewFloat(lf * rf), nil
		}
		return NewInt(l.I64 * r.I64), nil
	case "/", "Div":
		if rf == 0 {
			return nil, errors.New("division by zero")
		}
		if useFloat {
			return NewFloat(lf / rf), nil
		}
		// 防止 MinInt64 / -1 导致的 host panic
		if l.I64 == -9223372036854775808 && r.I64 == -1 {
			return NewInt(-9223372036854775808), nil
		}
		return NewInt(l.I64 / r.I64), nil
	case "%", "Mod":
		lVal, _ := l.ToInt()
		rVal, _ := r.ToInt()
		if rVal == 0 {
			return nil, errors.New("division by zero")
		}
		// 防止 MinInt64 % -1 导致的 host panic
		if lVal == -9223372036854775808 && rVal == -1 {
			return NewInt(0), nil
		}
		return NewInt(lVal % rVal), nil
	}
	return nil, fmt.Errorf("unsupported arithmetic operator: %s", op)
}

func (e *Executor) evalBitwise(op string, l, r *Var) (*Var, error) {
	li, err := l.ToInt()
	if err != nil {
		return nil, err
	}
	ri, err := r.ToInt()
	if err != nil {
		return nil, err
	}

	if ri < 0 {
		return nil, fmt.Errorf("negative shift count %d", ri)
	}

	switch op {
	case "&", "BitAnd":
		return NewInt(li & ri), nil
	case "|", "BitOr":
		return NewInt(li | ri), nil
	case "^", "BitXor":
		return NewInt(li ^ ri), nil
	case "<<", "Lsh":
		return NewInt(li << uint(ri)), nil
	case ">>", "Rsh":
		return NewInt(li >> uint(ri)), nil
	}
	return nil, fmt.Errorf("unsupported bitwise operator: %s", op)
}

func isEmptyVar(v *Var) bool {
	if v == nil {
		return true
	}
	switch v.VType {
	case TypeArray:
		if arr, ok := v.Ref.(*VMArray); ok {
			return arr == nil
		}
		return v.Ref == nil
	case TypeMap:
		if m, ok := v.Ref.(*VMMap); ok {
			return m == nil
		}
		return v.Ref == nil
	case TypeClosure, TypeModule, TypeResult:
		return v.Ref == nil && v.ResultVal == nil && v.ResultErr == ""
	case TypeHandle:
		return v.Handle == 0
	case TypeAny:
		return v.Ref == nil
	}
	return false
}

func (e *Executor) evalComparison(op string, l, r *Var) (*Var, error) {
	// Nil 安全比较
	lEmpty := isEmptyVar(l)
	rEmpty := isEmptyVar(r)

	if op == "==" || op == "Eq" {
		if lEmpty && rEmpty {
			return NewBool(true), nil
		}
		if lEmpty || rEmpty {
			return NewBool(false), nil
		}
	}
	if op == "!=" || op == "Neq" {
		if lEmpty && rEmpty {
			return NewBool(false), nil
		}
		if lEmpty || rEmpty {
			return NewBool(true), nil
		}
	}

	if l != nil && r != nil {
		if l.VType == TypeString && r.VType == TypeString {
			switch op {
			case "==", "Eq":
				return NewBool(l.Str == r.Str), nil
			case "!=", "Neq":
				return NewBool(l.Str != r.Str), nil
			}
		}
		if l.VType == TypeBool && r.VType == TypeBool {
			switch op {
			case "==", "Eq":
				return NewBool(l.Bool == r.Bool), nil
			case "!=", "Neq":
				return NewBool(l.Bool != r.Bool), nil
			}
		}

		lf, lErr := l.ToFloat()
		rf, rErr := r.ToFloat()
		if lErr == nil && rErr == nil {
			switch op {
			case "==", "Eq":
				return NewBool(lf == rf), nil
			case "!=", "Neq":
				return NewBool(lf != rf), nil
			case "<", "Lt":
				return NewBool(lf < rf), nil
			case ">", "Gt":
				return NewBool(lf > rf), nil
			case "<=", "Le":
				return NewBool(lf <= rf), nil
			case ">=", "Ge":
				return NewBool(lf >= rf), nil
			}
		}
	}

	// 基础比较
	if op == "==" || op == "Eq" {
		if l != nil && r != nil && l.VType == r.VType {
			switch l.VType {
			case TypeArray, TypeMap, TypeModule, TypeClosure:
				return NewBool(l.Ref == r.Ref), nil
			case TypeHandle:
				return NewBool(l.Handle == r.Handle), nil
			}
		}
		return NewBool(l == r), nil
	}
	if op == "!=" || op == "Neq" {
		if l != nil && r != nil && l.VType == r.VType {
			switch l.VType {
			case TypeArray, TypeMap, TypeModule, TypeClosure:
				return NewBool(l.Ref != r.Ref), nil
			case TypeHandle:
				return NewBool(l.Handle != r.Handle), nil
			}
		}
		return NewBool(l != r), nil
	}

	return nil, fmt.Errorf("unsupported comparison %s between %v and %v", op, l, r)
}

func (e *Executor) evalLogic(op string, l, r *Var) (*Var, error) {
	lb, err := l.ToBool()
	if err != nil {
		return nil, err
	}
	rb, err := r.ToBool()
	if err != nil {
		return nil, err
	}

	switch op {
	case "&&", "And":
		return NewBool(lb && rb), nil
	case "||", "Or":
		return NewBool(lb || rb), nil
	}
	return nil, fmt.Errorf("unsupported logic operator: %s", op)
}

func (e *Executor) evalRangeStmt(ctx *StackContext, n *ast.RangeStmt, obj *Var) error {
	var rangeErr error

	executeIteration := func(key, value *Var) {
		ctx.WithScope("for_range_body", func(bodyCtx *StackContext) {
			if n.Define {
				// Go 1.22 semantics: each iteration gets fresh variables if they were defined in the range clause.
				if n.Key != "" && n.Key != "_" {
					_ = bodyCtx.AddVariable(string(n.Key), key)
				}
				if n.Value != "" && n.Value != "_" && value != nil {
					_ = bodyCtx.AddVariable(string(n.Value), value)
				}
			} else {
				// If not defined with :=, it assigns to variables in outer scope.
				if n.Key != "" && n.Key != "_" {
					_ = ctx.Store(string(n.Key), key) // Use ctx, not bodyCtx
				}
				if n.Value != "" && n.Value != "_" && value != nil {
					_ = ctx.Store(string(n.Value), value)
				}
			}

			err := e.ExecuteStmts(bodyCtx, n.Body.Children)
			if err != nil {
				rangeErr = err
			}
			if bodyCtx.Interrupt() {
				// propagate interrupt to the parent scope temporarily to handle break/continue
				ctx.Stack.interrupt = bodyCtx.Stack.interrupt
			}
		})
	}

	switch obj.VType {
	case TypeArray:
		arr := obj.Ref.(*VMArray)
		for i, v := range arr.Data {
			if ctx.Context.Err() != nil {
				return ctx.Context.Err()
			}
			executeIteration(NewInt(int64(i)), v)
			if rangeErr != nil {
				return rangeErr
			}
			if ctx.Interrupt() {
				if ctx.Stack.interrupt == "break" {
					ctx.Stack.interrupt = ""
					break
				}
				if ctx.Stack.interrupt == "continue" {
					ctx.Stack.interrupt = ""
				} else {
					break
				}
			}
		}
	case TypeMap:
		m := obj.Ref.(*VMMap)
		for k, v := range m.Data {
			if ctx.Context.Err() != nil {
				return ctx.Context.Err()
			}
			executeIteration(NewString(k), v)
			if rangeErr != nil {
				return rangeErr
			}
			if ctx.Interrupt() {
				if ctx.Stack.interrupt == "break" {
					ctx.Stack.interrupt = ""
					break
				}
				if ctx.Stack.interrupt == "continue" {
					ctx.Stack.interrupt = ""
				} else {
					break
				}
			}
		}
	}
	return nil
}

func (e *Executor) evalSwitchStmt(ctx *StackContext, n *ast.SwitchStmt) error {
	if n.Init != nil {
		_ = e.execStmt(ctx, n.Init)
	}
	var tag *Var
	if n.Tag != nil {
		var err error
		tag, err = e.ExecExpr(ctx, n.Tag)
		if err != nil {
			return err
		}
	} else {
		tag = NewBool(true) // default to switch true
	}

	var defaultClause *ast.CaseClause
	found := false

	for _, child := range n.Body.Children {
		clause := child.(*ast.CaseClause)
		if clause.List == nil {
			defaultClause = clause
			continue
		}
		for _, expr := range clause.List {
			val, err := e.ExecExpr(ctx, expr)
			if err != nil {
				return err
			}
			res, _ := e.evalComparison("==", tag, val)
			if res != nil && res.Bool {
				found = true
				if err := e.ExecuteStmts(ctx, clause.Body); err != nil {
					return err
				}
				break
			}
		}
		if found {
			break
		}
	}

	if !found && defaultClause != nil {
		return e.ExecuteStmts(ctx, defaultClause.Body)
	}
	return nil
}

func (e *Executor) evalUnaryExprDirect(operator string, val *Var) (*Var, error) {
	if val == nil {
		return nil, errors.New("unary op with nil operand")
	}
	// 解包 Cell
	if val.VType == TypeCell {
		val = val.Ref.(*Cell).Value
	}
	switch operator {
	case "!", "Not":
		return NewBool(!val.Bool), nil
	case "-", "Sub", "Minus":
		if val.VType == TypeInt {
			return NewInt(-val.I64), nil
		}
	case "^", "BitXor", "Xor":
		if val.VType == TypeInt {
			return NewInt(^val.I64), nil
		}
	}
	return nil, fmt.Errorf("unsupported unary op %s", operator)
}

func (e *Executor) evalCallExpr(ctx *StackContext, n *ast.CallExprStmt) (*Var, error) {
	var name string
	var receiver *Var
	var mod *VMModule
	var callable *Var

	// 1. Determine the callable and receiver
	if ident, ok := n.Func.(*ast.ConstRefExpr); ok {
		name = string(ident.Name)
	} else if ident, ok := n.Func.(*ast.IdentifierExpr); ok {
		name = string(ident.Name)
	} else if member, ok := n.Func.(*ast.MemberExpr); ok {
		obj, err := e.ExecExpr(ctx, member.Object)
		if err != nil {
			return nil, err
		}
		if obj == nil {
			return nil, errors.New("calling method on nil object")
		}

		if obj.VType == TypeModule {
			// 模块函数调用
			mod = obj.Ref.(*VMModule)
			name = string(member.Property)
		} else {
			// 方法调用支持
			receiver = obj
			tName := string(obj.Type)
			tName = strings.TrimPrefix(tName, "Ptr<")
			tName = strings.TrimPrefix(tName, "*")
			tName = strings.TrimSuffix(tName, ">")
			name = fmt.Sprintf("__method_%s_%s", tName, member.Property)
		}
	} else {
		// It could be a FuncLitExpr or another CallExpr returning a closure
		var err error
		callable, err = e.ExecExpr(ctx, n.Func)
		if err != nil {
			return nil, err
		}
	}

	// If it's a local variable, it could be a closure.
	if name != "" && mod == nil && callable == nil {
		if v, err := ctx.Load(name); err == nil && v != nil {
			callable = v
		}
	}

	if name != "" || mod != nil || callable != nil {
		// 2. Prepare arguments
		totalArgs := len(n.Args)
		offset := 0
		if receiver != nil {
			totalArgs++
			offset = 1
		}
		args := make([]*Var, totalArgs)
		if receiver != nil {
			args[0] = receiver
		}
		for i, aExpr := range n.Args {
			var err error
			args[i+offset], err = e.ExecExpr(ctx, aExpr)
			if err != nil {
				return nil, err
			}
		}

		// 3. Handle Intrinsics (only if not a module call and no local callable shadows it)
		if mod == nil && callable == nil && name != "" {
			if res, handled, err := e.evalIntrinsic(ctx, name, args, n); handled {
				return res, err
			}
		}

		// 4. Resolve and execute callable
		if mod != nil {
			if val, ok := mod.Data[name]; ok {
				return e.evalDynamicCall(ctx, val, args, mod.Context)
			}
			return nil, fmt.Errorf("module member %s not found in %s", name, mod.Name)
		}

		if callable != nil {
			return e.evalDynamicCall(ctx, callable, args, nil)
		}

		// Internal function in current program
		if f, ok := e.program.Functions[ast.Ident(name)]; ok {
			return e.execInternalFunc(ctx, name, f, args)
		}

		// FFI Route
		if route, ok := e.routes[name]; ok {
			return e.evalFFI(ctx, route, args)
		}
	}

	return nil, fmt.Errorf("unsupported call expression: %v (name: %s)", n.Func, name)
}

func (e *Executor) evalDynamicCall(ctx *StackContext, callable *Var, args []*Var, modCtx *StackContext) (*Var, error) {
	if callable == nil {
		return nil, errors.New("cannot call nil")
	}

	// 确定实际执行的上下文：
	// 1. 优先使用传入的 modCtx (由 MemberExpr 提取)
	// 2. 其次使用闭包自带的 Context (记录了创建时的母上下文)
	// 3. 最后回退到当前的调用上下文
	execCtx := ctx
	if modCtx != nil {
		execCtx = modCtx
	}

	if callable.VType == TypeClosure {
		if method, ok := callable.Ref.(*VMMethodValue); ok {
			fullArgs := make([]*Var, len(args)+1)
			fullArgs[0] = method.Receiver
			copy(fullArgs[1:], args)
			if route, ok := e.routes[method.Method]; ok {
				return e.evalFFI(ctx, route, fullArgs)
			}
			return nil, fmt.Errorf("method route not found: %s", method.Method)
		}

		closure := callable.Ref.(*VMClosure)
		if closure.Context != nil && modCtx == nil {
			execCtx = closure.Context
		}
		return e.execClosure(execCtx, closure, args)
	}

	// If it has Ref to FunctionStmt, it's a script function
	if f, ok := callable.Ref.(*ast.FunctionStmt); ok {
		return e.execInternalFunc(execCtx, string(f.Name), f, args)
	}

	// If it has Ref to FFIRoute, it's an FFI function
	if route, ok := callable.Ref.(FFIRoute); ok {
		return e.evalFFI(ctx, route, args)
	}

	return nil, fmt.Errorf("type %v is not callable", callable.VType)
}

func (e *Executor) evalIntrinsic(ctx *StackContext, name string, args []*Var, n *ast.CallExprStmt) (*Var, bool, error) {
	switch name {
	case "require":
		if len(n.Args) != 1 {
			return nil, true, errors.New("require expects 1 argument")
		}
		pathVar, err := e.ExecExpr(ctx, n.Args[0])
		if err != nil {
			return nil, true, err
		}
		v, err := e.ImportModule(ctx, &ast.ImportExpr{Path: pathVar.Str})
		return v, true, err
	case "panic":
		var val *Var
		if len(args) > 0 {
			val = args[0]
		} else {
			val = NewString("panic")
		}
		return nil, true, &PanicError{Value: val}
	case "recover":
		res := ctx.PanicVar
		ctx.PanicVar = nil
		return res, true, nil
	case "String":
		if len(args) > 0 && args[0] != nil {
			arg := args[0]
			switch arg.VType {
			case TypeString:
				return arg, true, nil
			case TypeBytes:
				return NewString(string(arg.B)), true, nil
			case TypeInt:
				return NewString(strconv.FormatInt(arg.I64, 10)), true, nil
			case TypeFloat:
				return NewString(strconv.FormatFloat(arg.F64, 'f', -1, 64)), true, nil
			case TypeBool:
				return NewString(strconv.FormatBool(arg.Bool)), true, nil
			}
		}
		return NewString(""), true, nil
	case "TypeBytes":
		if len(args) > 0 && args[0] != nil {
			arg := args[0]
			switch arg.VType {
			case TypeBytes:
				return arg, true, nil
			case TypeString:
				return NewBytes([]byte(arg.Str)), true, nil
			}
		}
		return NewBytes(nil), true, nil
	case "Int64":
		if len(args) > 0 && args[0] != nil {
			arg := args[0]
			switch arg.VType {
			case TypeInt:
				return arg, true, nil
			case TypeFloat:
				return NewInt(int64(arg.F64)), true, nil
			case TypeString:
				val, _ := strconv.ParseInt(arg.Str, 10, 64)
				return NewInt(val), true, nil
			case TypeBool:
				if arg.Bool {
					return NewInt(1), true, nil
				}
				return NewInt(0), true, nil
			}
		}
		return NewInt(0), true, nil
	case "Float64":
		if len(args) > 0 && args[0] != nil {
			arg := args[0]
			switch arg.VType {
			case TypeFloat:
				return arg, true, nil
			case TypeInt:
				return NewFloat(float64(arg.I64)), true, nil
			case TypeString:
				val, _ := strconv.ParseFloat(arg.Str, 64)
				return NewFloat(val), true, nil
			case TypeBool:
				if arg.Bool {
					return NewFloat(1.0), true, nil
				}
				return NewFloat(0.0), true, nil
			}
		}
		return NewFloat(0.0), true, nil
	case "len":
		if len(args) > 0 && args[0] != nil {
			arg := args[0]
			switch arg.VType {
			case TypeString:
				return NewInt(int64(len(arg.Str))), true, nil
			case TypeBytes:
				return NewInt(int64(len(arg.B))), true, nil
			case TypeArray:
				arr := arg.Ref.(*VMArray)
				return NewInt(int64(len(arr.Data))), true, nil
			case TypeMap:
				m := arg.Ref.(*VMMap)
				return NewInt(int64(len(m.Data))), true, nil
			}
		}
		return NewInt(0), true, nil
	case "new":
		if len(args) < 1 || args[0] == nil || args[0].VType != TypeString {
			return nil, true, errors.New("invalid arguments to new")
		}
		tStr := args[0].Str
		innerType := tStr
		if strings.HasPrefix(tStr, "Ptr<") && strings.HasSuffix(tStr, ">") {
			innerType = tStr[4 : len(tStr)-1]
		}

		res := e.initializeType(ctx, ast.GoMiniType(innerType), 0)
		if res != nil {
			res.Type = ast.GoMiniType(tStr)
		}
		return res, true, nil
	case "make":
		if len(args) < 1 || args[0] == nil || args[0].VType != TypeString {
			return nil, true, errors.New("invalid arguments to make")
		}
		tStr := args[0].Str
		if strings.HasPrefix(tStr, "Map<") {
			return &Var{VType: TypeMap, Ref: &VMMap{Data: make(map[string]*Var)}, Type: ast.GoMiniType(tStr)}, true, nil
		} else if strings.HasPrefix(tStr, "Array<") || tStr == "TypeBytes" {
			length := 0
			capacity := 0
			if len(args) > 1 && args[1] != nil {
				lInt, _ := args[1].ToInt()
				if lInt < 0 {
					return nil, true, fmt.Errorf("negative length in make: %d", lInt)
				}
				if lInt > 10000000 {
					return nil, true, fmt.Errorf("requested length too large: %d", lInt)
				}
				length = int(lInt)
				capacity = length
			}
			if len(args) > 2 && args[2] != nil {
				cInt, _ := args[2].ToInt()
				if int(cInt) < length {
					return nil, true, fmt.Errorf("capacity %d less than length %d", cInt, length)
				}
				if cInt > 10000000 {
					return nil, true, fmt.Errorf("requested capacity too large: %d", cInt)
				}
				capacity = int(cInt)
			}
			if tStr == "TypeBytes" {
				return &Var{VType: TypeBytes, B: make([]byte, length, capacity), Type: "TypeBytes"}, true, nil
			}
			return &Var{VType: TypeArray, Ref: &VMArray{Data: make([]*Var, length, capacity)}, Type: ast.GoMiniType(tStr)}, true, nil
		}
		return nil, true, fmt.Errorf("cannot make type %s", tStr)
	case "append":
		if len(args) < 1 || args[0] == nil {
			return nil, true, errors.New("missing arguments to append")
		}
		sliceArg := args[0]
		if sliceArg.VType != TypeArray && sliceArg.VType != TypeBytes {
			return nil, true, fmt.Errorf("first argument to append must be slice; have %v", sliceArg.VType)
		}
		if sliceArg.VType == TypeBytes {
			data := make([]byte, len(sliceArg.B))
			copy(data, sliceArg.B)
			for i := 1; i < len(args); i++ {
				arg := args[i]
				if arg != nil {
					switch arg.VType {
					case TypeInt:
						data = append(data, byte(arg.I64))
					case TypeBytes:
						data = append(data, arg.B...)
					}
				}
			}
			if len(data) > 10000000 {
				return nil, true, errors.New("slice size limit exceeded in append")
			}
			return &Var{VType: TypeBytes, B: data, Type: sliceArg.Type}, true, nil
		}
		arr := sliceArg.Ref.(*VMArray)
		if len(arr.Data)+len(args)-1 > 10000000 {
			return nil, true, errors.New("array size limit exceeded in append")
		}
		data := make([]*Var, len(arr.Data))
		copy(data, arr.Data)
		for i := 1; i < len(args); i++ {
			data = append(data, args[i])
		}
		return &Var{VType: TypeArray, Ref: &VMArray{Data: data}, Type: sliceArg.Type}, true, nil
	case "delete":
		if len(args) != 2 || args[0] == nil {
			return nil, true, errors.New("invalid arguments to delete")
		}
		mapArg := args[0]
		if mapArg.VType != TypeMap && mapArg.VType != TypeAny {
			return nil, true, fmt.Errorf("first argument to delete must be map; have %v", mapArg.VType)
		}
		keyArg := args[1]
		if keyArg != nil {
			var m *VMMap
			if mapArg.VType == TypeMap {
				m = mapArg.Ref.(*VMMap)
			} else if mapArg.Ref != nil {
				m, _ = mapArg.Ref.(*VMMap)
			}
			if m != nil {
				key := keyArg.Str
				if keyArg.VType == TypeInt {
					key = strconv.FormatInt(keyArg.I64, 10)
				}
				delete(m.Data, key)
			}
		}
		return nil, true, nil
	}
	return nil, false, nil
}

func (e *Executor) execInternalFunc(ctx *StackContext, name string, f *ast.FunctionStmt, args []*Var) (*Var, error) {
	var res *Var
	err := ctx.WithFuncScope(name, func(old *Stack, c *StackContext) (innerErr error) {
		c.Executor = e
		for i, p := range f.Params {
			_ = c.NewVar(string(p.Name), p.Type)
			if f.Variadic && i == len(f.Params)-1 {
				var variadicArgs []*Var
				if i < len(args) {
					variadicArgs = args[i:]
				}
				_ = c.Store(string(p.Name), &Var{VType: TypeArray, Ref: &VMArray{Data: variadicArgs}, Type: p.Type})
			} else if i < len(args) && args[i] != nil {
				_ = c.Store(string(p.Name), args[i])
			}
		}
		if !f.Return.IsVoid() {
			_ = c.NewVar("__return__", f.Return)
		}

		execErr := e.ExecuteStmts(c, f.Body.Children)
		if execErr != nil {
			targetErr := execErr
			for {
				if mErr, ok := targetErr.(*MiniRuntimeError); ok {
					targetErr = mErr.Err
				} else {
					break
				}
			}
			if pErr, ok := targetErr.(*PanicError); ok {
				c.PanicVar = pErr.Value
			} else {
				innerErr = execErr
			}
		}

		// 执行 defer
		c.Stack.RunDefers()

		// 如果 RunDefers 之后 PanicVar 依然存在，则说明没被恢复
		if c.PanicVar != nil {
			innerErr = &PanicError{Value: c.PanicVar}
		}

		if !f.Return.IsVoid() && innerErr == nil {
			res, _ = c.Load("__return__")
		}
		return innerErr
	})
	return res, err
}

func (e *Executor) execClosure(ctx *StackContext, closure *VMClosure, args []*Var) (*Var, error) {
	f := closure.FuncDef
	var res *Var

	err := ctx.WithFuncScope("closure", func(old *Stack, c *StackContext) (innerErr error) {
		c.Executor = e

		// Inject upvalues
		for k, v := range closure.Upvalues {
			c.Stack.MemoryPtr[k] = v // directly assign the Cell pointer
		}

		// Inject parameters
		for i, p := range f.Params {
			if string(p.Name) != "" {
				_ = c.NewVar(string(p.Name), p.Type)
				if i < len(args) && args[i] != nil {
					_ = c.Store(string(p.Name), args[i])
				}
			}
		}

		if !f.Return.IsVoid() {
			_ = c.NewVar("__return__", f.Return)
		}

		execErr := e.ExecuteStmts(c, f.Body.Children)
		if execErr != nil {
			targetErr := execErr
			for {
				if mErr, ok := targetErr.(*MiniRuntimeError); ok {
					targetErr = mErr.Err
				} else {
					break
				}
			}
			if pErr, ok := targetErr.(*PanicError); ok {
				c.PanicVar = pErr.Value
			} else {
				innerErr = execErr
			}
		}

		// 执行 defer
		c.Stack.RunDefers()

		if c.PanicVar != nil {
			innerErr = &PanicError{Value: c.PanicVar}
		}

		if !f.Return.IsVoid() && innerErr == nil {
			res, _ = c.Load("__return__")
		}
		return innerErr
	})

	return res, err
}

func (e *Executor) evalFFI(ctx *StackContext, route FFIRoute, args []*Var) (*Var, error) {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)

	// 获取函数签名以获取参数类型列表
	fn, ok := ast.GoMiniType(route.Spec).ReadCallFunc()

	// 序列化参数
	if ok && fn.Variadic {
		// 1. 序列化常规参数
		numNormal := len(fn.Params) - 1
		for i := 0; i < numNormal; i++ {
			arg := &Var{VType: TypeAny} // 默认
			if i < len(args) {
				arg = args[i]
			}
			if err := e.serializeVar(buf, arg, fn.Params[i]); err != nil {
				return nil, err
			}
		}

		// 2. 序列化变长参数部分：[Count (Uint32)] [Item1] [Item2]...
		numVariadic := 0
		if len(args) > numNormal {
			numVariadic = len(args) - numNormal
		}
		buf.WriteUint32(uint32(numVariadic))
		itemType := fn.Params[numNormal]
		if numVariadic > 0 {
			for i := 0; i < numVariadic; i++ {
				if err := e.serializeVar(buf, args[numNormal+i], itemType); err != nil {
					return nil, err
				}
			}
		}
	} else {
		// 普通非变长函数序列化
		for i, arg := range args {
			var argType ast.GoMiniType = "Any"
			if ok && i < len(fn.Params) {
				argType = fn.Params[i]
			}
			if err := e.serializeVar(buf, arg, argType); err != nil {
				return nil, err
			}
		}
	}

	// 呼叫 Bridge
	retData, err := route.Bridge.Call(ctx.Context, route.MethodID, buf.Bytes())
	if err != nil {
		return nil, err
	}

	// 解析返回值
	if len(retData) == 0 {
		return nil, nil
	}

	reader := ffigo.NewReader(retData)
	retType := ast.GoMiniType(route.Returns)

	// 检查是否是 Result<T> 类型
	if retType.IsResult() {
		status := reader.ReadByte() // 0: Success, 1: Error
		innerType, _ := retType.ReadResult()

		if status == 0 {
			val, err := e.deserializeVar(ctx, reader, innerType, route.Bridge)
			if err != nil {
				return nil, err
			}
			return &Var{VType: TypeResult, ResultVal: val, Type: retType}, nil
		}
		errMsg := reader.ReadString()
		return &Var{VType: TypeResult, ResultErr: errMsg, Type: retType}, nil
	}

	return e.deserializeVar(ctx, reader, retType, route.Bridge)
}

func (e *Executor) serializeKey(buf *ffigo.Buffer, key string, kType ast.GoMiniType) error {
	switch kType {
	case "String":
		buf.WriteString(key)
	case "Int64":
		v, _ := strconv.ParseInt(key, 10, 64)
		buf.WriteInt64(v)
	default:
		return fmt.Errorf("unsupported map key type: %s", kType)
	}
	return nil
}

func (e *Executor) serializeVar(buf *ffigo.Buffer, v *Var, typ ast.GoMiniType) error {
	// 如果 typ 是 Any，回退到动态序列化
	if typ == "Any" {
		e.serializeVarToAny(buf, v)
		return nil
	}

	// 严格按照 typ 类型进行序列化，防止协议层错位
	switch {
	case typ == "String":
		str := ""
		if v != nil {
			str = v.Str
			if v.VType == TypeBytes {
				str = string(v.B)
			}
		}
		buf.WriteString(str)
	case typ == "Float64":
		fVal := 0.0
		if v != nil {
			fVal, _ = v.ToFloat()
		}
		buf.WriteFloat64(fVal)
	case typ.IsNumeric():
		iVal := int64(0)
		if v != nil {
			iVal, _ = v.ToInt()
		}
		buf.WriteInt64(iVal)
	case typ == "Bool":
		bVal := false
		if v != nil {
			bVal, _ = v.ToBool()
		}
		buf.WriteBool(bVal)
	case typ == "TypeBytes":
		var bVal []byte
		if v != nil {
			bVal, _ = v.ToBytes()
		}
		buf.WriteBytes(bVal)
	case typ.IsPtr() || typ == "TypeHandle":
		hVal := uint32(0)
		if v != nil {
			hVal, _ = v.ToHandle()
		}
		buf.WriteUint32(hVal)
	case typ.IsArray():
		if v == nil || v.VType != TypeArray {
			buf.WriteUint32(0)
			return nil
		}
		arr := v.Ref.(*VMArray)
		buf.WriteUint32(uint32(len(arr.Data)))
		itemType, _ := typ.ReadArrayItemType()
		if itemType == "" {
			itemType = "Any"
		}
		for _, item := range arr.Data {
			if err := e.serializeVar(buf, item, itemType); err != nil {
				return err
			}
		}
	case typ.IsMap():
		if v == nil || v.VType != TypeMap {
			buf.WriteUint32(0)
			return nil
		}
		kType, vType, ok := typ.GetMapKeyValueTypes()
		if ok {
			vmMap := v.Ref.(*VMMap)
			buf.WriteUint32(uint32(len(vmMap.Data)))
			for k, val := range vmMap.Data {
				if err := e.serializeKey(buf, k, kType); err != nil {
					return err
				}
				if err := e.serializeVar(buf, val, vType); err != nil {
					return err
				}
			}
		}
	default:
		// 结构体序列化
		if name, ok := typ.StructName(); ok {
			if sDef, ok := e.program.Structs[name]; ok {
				var mData map[string]*Var
				if v != nil && v.VType == TypeMap {
					mData = v.Ref.(*VMMap).Data
				}
				for _, fName := range sDef.FieldNames {
					fType := sDef.Fields[fName]
					var fVal *Var
					if mData != nil {
						fVal = mData[string(fName)]
					}
					if err := e.serializeVar(buf, fVal, fType); err != nil {
						return err
					}
				}
				return nil
			}
		}
		// 其他情况回退到 Any 动态序列化
		e.serializeVarToAny(buf, v)
	}
	return nil
}

func (e *Executor) serializeVarToAny(buf *ffigo.Buffer, v *Var) {
	e.serializeVarToAnyWithDepth(buf, v, 0)
}

func (e *Executor) serializeVarToAnyWithDepth(buf *ffigo.Buffer, v *Var, depth int) {
	if depth > 100 {
		buf.WriteAny(nil)
		return
	}
	if v == nil {
		buf.WriteAny(nil)
		return
	}
	switch v.VType {
	case TypeInt:
		buf.WriteAny(v.I64)
	case TypeFloat:
		buf.WriteAny(v.F64)
	case TypeString:
		buf.WriteAny(v.Str)
	case TypeBytes:
		buf.WriteAny(v.B)
	case TypeBool:
		buf.WriteAny(v.Bool)
	case TypeHandle:
		buf.WriteAny(v.Handle)
	case TypeArray:
		arr := v.Ref.(*VMArray)
		buf.WriteByte(8) // TypeTagArray
		buf.WriteUint32(uint32(len(arr.Data)))
		for _, item := range arr.Data {
			e.serializeVarToAnyWithDepth(buf, item, depth+1)
		}
	case TypeMap:
		vmMap := v.Ref.(*VMMap)
		buf.WriteByte(7) // TypeTagMap
		buf.WriteUint32(uint32(len(vmMap.Data)))
		for k, val := range vmMap.Data {
			buf.WriteString(k)
			e.serializeVarToAnyWithDepth(buf, val, depth+1)
		}
	default:
		buf.WriteAny(nil)
	}
}

func (e *Executor) ToVar(ctx *StackContext, val interface{}, bridge ffigo.FFIBridge) *Var {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case *Var:
		return v
	case int:
		return NewInt(int64(v))
	case int64:
		return NewInt(v)
	case float64:
		return NewFloat(v)
	case string:
		return NewString(v)
	case []byte:
		if v == nil {
			return nil
		}
		buf := make([]byte, len(v))
		copy(buf, v)
		return &Var{VType: TypeBytes, B: buf}
	case bool:
		return NewBool(v)
	case uint32:
		var h *VMHandle
		if v != 0 {
			h = NewVMHandle(v, bridge)
			ctx.ActiveHandles = append(ctx.ActiveHandles, HandleRef{Bridge: bridge, ID: v})
		}
		return &Var{VType: TypeHandle, Handle: v, Bridge: bridge, Ref: h}
	case map[string]interface{}:
		res := make(map[string]*Var)
		for k, raw := range v {
			res[k] = e.ToVar(ctx, raw, bridge)
		}
		return &Var{VType: TypeMap, Ref: &VMMap{Data: res}}
	case []interface{}:
		res := make([]*Var, len(v))
		for i, raw := range v {
			res[i] = e.ToVar(ctx, raw, bridge)
		}
		return &Var{VType: TypeArray, Ref: &VMArray{Data: res}}
	}
	return nil
}

func (e *Executor) deserializeKey(reader *ffigo.Reader, kType ast.GoMiniType) (string, error) {
	switch kType {
	case "String":
		return reader.ReadString(), nil
	case "Int64":
		return strconv.FormatInt(reader.ReadInt64(), 10), nil
	default:
		return "", fmt.Errorf("unsupported map key type: %s", kType)
	}
}

func (e *Executor) deserializeVar(ctx *StackContext, reader *ffigo.Reader, typ ast.GoMiniType, bridge ffigo.FFIBridge) (*Var, error) {
	if typ.IsVoid() {
		return nil, nil
	}
	if reader.Available() == 0 {
		return nil, nil
	}

	var res *Var
	var err error

	if typ == "Any" {
		res = e.ToVar(ctx, reader.ReadAny(), bridge)
	} else {
		switch {
		case typ == "String":
			res = NewString(reader.ReadString())
		case typ == "Int64" || typ == "Uint32":
			res = NewInt(reader.ReadInt64())
		case typ == "Float64":
			res = NewFloat(reader.ReadFloat64())
		case typ == "Bool":
			res = NewBool(reader.ReadBool())
		case typ == "TypeBytes":
			res = &Var{VType: TypeBytes, B: reader.ReadBytes()}
		case strings.HasPrefix(string(typ), "Ptr<") || typ == "TypeHandle":
			id := reader.ReadUint32()
			var h *VMHandle
			if id != 0 {
				h = NewVMHandle(id, bridge)
				ctx.ActiveHandles = append(ctx.ActiveHandles, HandleRef{Bridge: bridge, ID: id})
			}
			res = &Var{VType: TypeHandle, Handle: id, Bridge: bridge, Ref: h}
		case typ.IsArray():
			count := int(reader.ReadUint32())
			itemType, _ := typ.ReadArrayItemType()
			arrData := make([]*Var, count)
			for i := 0; i < count; i++ {
				val, err := e.deserializeVar(ctx, reader, itemType, bridge)
				if err != nil {
					return nil, err
				}
				arrData[i] = val
			}
			res = &Var{VType: TypeArray, Ref: &VMArray{Data: arrData}}
		case typ.IsMap():
			count := int(reader.ReadUint32())
			kType, vType, _ := typ.GetMapKeyValueTypes()
			mapData := make(map[string]*Var)
			for i := 0; i < count; i++ {
				k, err := e.deserializeKey(reader, kType)
				if err != nil {
					return nil, err
				}
				val, err := e.deserializeVar(ctx, reader, vType, bridge)
				if err != nil {
					return nil, err
				}
				mapData[k] = val
			}
			res = &Var{VType: TypeMap, Ref: &VMMap{Data: mapData}}
		case typ.IsTuple():
			types, _ := typ.ReadTuple()
			tupleData := make([]*Var, len(types))
			for i, t := range types {
				val, err := e.deserializeVar(ctx, reader, t, bridge)
				if err != nil {
					return nil, err
				}
				tupleData[i] = val
			}
			res = &Var{VType: TypeArray, Ref: &VMArray{Data: tupleData}}
		default:
			if name, ok := typ.StructName(); ok {
				if sDef, ok := e.program.Structs[name]; ok {
					resMap := make(map[string]*Var)
					for _, fName := range sDef.FieldNames {
						fType := sDef.Fields[fName]
						val, err := e.deserializeVar(ctx, reader, fType, bridge)
						if err != nil {
							return nil, err
						}
						resMap[string(fName)] = val
					}
					res = &Var{VType: TypeMap, Ref: &VMMap{Data: resMap}}
					break
				}
			}
			return nil, fmt.Errorf("unsupported FFI return type: %s", typ)
		}
	}

	if res != nil {
		res.Type = typ
	}
	return res, err
}

func (e *Executor) GetProgram() *ast.ProgramStmt { return e.program }

func (e *Executor) initializeType(ctx *StackContext, t ast.GoMiniType, depth int) *Var {
	if depth > 10 {
		return &Var{VType: TypeAny, Type: t}
	}

	if t.IsPtr() || t.IsArray() || t.IsMap() || t.IsAny() {
		return &Var{VType: TypeAny, Type: t}
	}

	// 基础类型
	zero := t.ZeroVar()
	res := e.ToVar(ctx, zero, nil)
	if res != nil {
		res.Type = t
		return res
	}

	// 结构体
	mData := make(map[string]*Var)
	if sDef, ok := e.structs[string(t)]; ok {
		for _, fName := range sDef.FieldNames {
			fType := sDef.Fields[fName]
			mData[string(fName)] = e.initializeType(ctx, fType, depth+1)
		}
	}
	return &Var{VType: TypeMap, Ref: &VMMap{Data: mData}, Type: t}
}
