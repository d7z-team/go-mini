package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
	"weak"

	"gopkg.d7z.net/go-mini/core/ast"
)

const (
	ContextKeyMonitor  = "ContextKeyMonitor"
	ContextKeyNodeMeta = "ContextKeyNodeMeta"
)

type Executor struct {
	structs map[string]interface{}
	consts  map[string]string
	funcs   map[ast.Ident]*Var
	program *ast.ProgramStmt
	ctx     *StackContext

	monitor MonitorManager
}

// MonitorManager 定义了执行过程中的监控接口
type MonitorManager interface {
	ReportProgram(state string, message string, duration int)
	ReportStep(state string, meta ast.BaseNode, duration int)
}

type MiniRuntimeError struct {
	BaseNode ast.BaseNode
	Err      error
}

func (e *MiniRuntimeError) Error() string {
	return e.Err.Error()
}

func NewExecutor(program *ast.ProgramStmt, customStrcuts ...any) (*Executor, error) {
	if program == nil || program.Main == nil || len(program.Main) == 0 {
		return nil, errors.New("invalid program")
	}
	result := &Executor{
		program: program,
		structs: make(map[string]interface{}),
		funcs:   make(map[ast.Ident]*Var),
		consts:  make(map[string]string),
	}
	for ident, stmt := range program.Structs {
		result.structs[string(ident)] = stmt
	}
	for s, s2 := range program.Constants {
		result.consts[s] = s2
	}
	result.ctx = &StackContext{
		Stack: &Stack{
			Parent:    nil,
			MemoryPtr: make(map[string]*Var),
			Scope:     "global",
			interrupt: "",
		},
	}
	for _, stdlibStruct := range ast.StdlibStructs {
		if err := result.AddStruct(stdlibStruct); err != nil {
			return nil, err
		}
	}
	for _, struc := range customStrcuts {
		if err := result.AddStruct(struc); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (e *Executor) GetMonitor() MonitorManager {
	return e.monitor
}

func (e *Executor) Execute(ctx context.Context) (err error) {
	e.ctx.Context = ctx
	e.ctx.Executor = e
	defer e.ctx.ExecuteDeferred()
	begin := time.Now()
	if mm, ok := ctx.Value(ContextKeyMonitor).(MonitorManager); ok {
		e.monitor = mm
	}
	if e.monitor != nil {
		e.monitor.ReportProgram("program_start", "", 0)
	}

	finalState := "program_success"
	var finalMsg string

	defer func() {
		if r := recover(); r != nil {
			slog.Error("Executor panic", "error", r, "stack", string(debug.Stack()))
			finalState = "program_fail"
			finalMsg = fmt.Sprintf("panic: %v", r)
			err = fmt.Errorf("panic: %v", r)
		}

		if e.monitor != nil {
			e.monitor.ReportProgram(finalState, finalMsg, int(time.Since(begin).Milliseconds()))
		}
	}()

	if e.program == nil {
		err = errors.New("program is nil")
		finalState = "program_fail"
		finalMsg = err.Error()
		return err
	}

	err = e.execStmts(e.ctx, e.program.Main)
	if err != nil {
		finalState = "program_fail"
		finalMsg = err.Error()
	}
	return err
}

func (e *Executor) GetProgram() *ast.ProgramStmt {
	return e.program
}

func (e *Executor) wrapError(err error, node ast.Node) error {
	if err == nil {
		return nil
	}
	var astErr *MiniRuntimeError
	if errors.As(err, &astErr) {
		return err
	}
	return &MiniRuntimeError{
		Err:      err,
		BaseNode: *node.GetBase(),
	}
}

func (e *Executor) execStmt(ctx *StackContext, s ast.Stmt) (err error) {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	defer func() {
		if err != nil {
			err = e.wrapError(err, s)
		}
	}()
	start := time.Now()
	if e.monitor != nil {
		e.monitor.ReportStep("cmd_start", *s.GetBase(), 0)
		defer func() {
			node := *s.GetBase()
			if err != nil {
				node.Message = err.Error()
			}
			e.monitor.ReportStep("cmd_end", node, int(time.Since(start).Milliseconds()))
		}()
	}
	switch n := s.(type) {
	case *ast.BlockStmt:
		return e.execStmts(ctx, n.Children)
	case *ast.GenDeclStmt:
		return ctx.NewVar(string(n.Name), n.Kind)
	case *ast.AssignmentStmt:
		expr, err := e.ExecExpr(ctx, n.Value)
		if err != nil {
			return err
		}
		// Allow assignment if target is Any or types match
		return ctx.Store(string(n.Variable), expr)
	case *ast.DerefAssignmentStmt:
		obj, err := e.ExecExpr(ctx, n.Object)
		if err != nil {
			return err
		}
		val, err := e.ExecExpr(ctx, n.Value)
		if err != nil {
			return err
		}

		if obj.Data == nil {
			return errors.New("dereference of nil pointer")
		}

		rv := reflect.ValueOf(obj.Data)
		// 递归解开指针直到找到 Set 方法或者到达底层结构体
		for rv.Kind() == reflect.Ptr {
			if method := rv.MethodByName("Set"); method.IsValid() {
				goVal := e.toGoValue(val.Data)
				targetType := method.Type().In(0)
				argVal := reflect.ValueOf(goVal)
				if argVal.Type().ConvertibleTo(targetType) {
					method.Call([]reflect.Value{argVal.Convert(targetType)})
					return nil
				}
			}
			if rv.Elem().Kind() != reflect.Ptr {
				break
			}
			rv = rv.Elem()
		}

		// 直接反射设置
		if rv.Kind() == reflect.Ptr {
			target := rv.Elem()
			if target.CanSet() {
				source := reflect.ValueOf(val.Data)
				if source.Type().AssignableTo(target.Type()) {
					target.Set(source)
					return nil
				} else if source.Kind() == reflect.Ptr && source.Elem().Type().AssignableTo(target.Type()) {
					target.Set(source.Elem())
					return nil
				} else if source.Type().ConvertibleTo(target.Type()) {
					target.Set(source.Convert(target.Type()))
					return nil
				}
			}
		}

		return fmt.Errorf("无法将 %T 赋值给 %T 的解引用", val.Data, obj.Data)
	case *ast.DeferStmt:
		if ctx.Stack == nil {
			return errors.New("stack is nil")
		}
		ctx.Stack.Deferred = append(ctx.Stack.Deferred, n.Call)
		return nil
	case *ast.ForStmt:
		var forErr error
		ctx.WithScope("for", func(ctx *StackContext) {
			if n.Init != nil {
				if err := e.execStmts(ctx, n.Init.(*ast.BlockStmt).Children); err != nil {
					forErr = err
					return
				}
			}
			for {
				ctx.ScopeApply("for-main")
				expr, err := e.ExecExpr(ctx, n.Cond)
				if err != nil {
					forErr = err
					ctx.ScopeExit()
					return
				}
				miniBool := expr.Data.(ast.MiniBool)
				if !miniBool.Data() {
					ctx.ScopeExit()
					break
				}
				ctx.WithScope("for-body", func(ctx *StackContext) {
					if n.Body != nil {
						if err := e.execStmts(ctx, n.Body.(*ast.BlockStmt).Children); err != nil {
							forErr = err
							return
						}
					}
				})
				if forErr != nil {
					ctx.ScopeExit()
					return
				}
				if ctx.Interrupt() {
					ctx.ScopeExit()
					break
				}
				if n.Update != nil {
					if err := e.execStmts(ctx, n.Update.(*ast.BlockStmt).Children); err != nil {
						forErr = err
						ctx.ScopeExit()
						return
					}
				}
				ctx.ScopeExit()
			}
		})
		return forErr
	case *ast.IfStmt:
		expr, err := e.ExecExpr(ctx, n.Cond)
		if err != nil {
			return err
		}
		ctx.ScopeApply("if")
		miniBool := expr.Data.(ast.MiniBool)
		if miniBool.Data() {
			if n.Body != nil {
				if err := e.execStmts(ctx, n.Body.Children); err != nil {
					return err
				}
			}
		} else {
			if n.ElseBody != nil {
				if err := e.execStmts(ctx, n.ElseBody.Children); err != nil {
					return err
				}
			}
		}
		ctx.ScopeExit()
		return nil
	case *ast.ReturnStmt:
		if err := ctx.SetInterrupt("function", "return"); err != nil {
			return err
		}
		if len(n.Results) == 0 {
			return nil
		}
		var results []*Var
		var resultsType []ast.OPSType
		for _, result := range n.Results {
			expr, err := e.ExecExpr(ctx, result)
			if err != nil {
				return err
			}
			results = append(results, expr)
			resultsType = append(resultsType, expr.Type)
		}
		if len(results) > 1 {
			var resultTuple []any
			for _, result := range results {
				resultTuple = append(resultTuple, result.Data)
			}
			return ctx.Store("__return__", NewVar(ast.CreateTupleType(resultsType...), reflect.TypeOf(resultTuple), resultTuple, nil))
		}
		return ctx.Store("__return__", NewVar(results[0].Type, results[0].GoType, results[0].Data, nil))
	case *ast.InterruptStmt:
		if n.InterruptType == "continue" {
			return ctx.SetInterrupt("for-body", n.InterruptType)
		}
		return ctx.SetInterrupt("for-main", n.InterruptType)
	case *ast.CallExprStmt:
		r, err := e.ExecExpr(ctx, n)
		if err != nil {
			return err
		}
		if r != nil && r.Data != nil {
			if err, ok := r.Data.(error); ok {
				return err
			}
		}
		return nil
	}
	return errors.New("todo: " + s.GetBase().Meta)
}

// execStmts todo：处理 func 中断
func (e *Executor) execStmts(ctx *StackContext, children []ast.Stmt) error {
	for _, child := range children {
		if ctx.Interrupt() {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := e.execStmt(ctx, child); err != nil {
			return err
		}
	}

	// 如果当前是函数作用域且发生了 return 中断，执行 defer
	if ctx.Stack != nil && ctx.Stack.interrupt == "return" {
		ctx.ExecuteDeferred()
	}

	return nil
}

func (e *Executor) ExecExpr(ctx *StackContext, s ast.Expr) (v *Var, err error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	defer func() {
		if err != nil {
			err = e.wrapError(err, s)
		}
	}()
	switch n := s.(type) {
	case *ast.LiteralExpr:
		if n.Data != nil {
			return NewVar(n.Type, reflect.TypeOf(n.Data), n.Data, ctx.Stack), nil
		}
		if n.Type.IsPtr() && n.Value == "" {
			var goType reflect.Type
			if elemType, ok := n.Type.GetPtrElementType(); ok {
				if structDef, ok := e.structs[string(elemType)]; ok {
					switch stru := structDef.(type) {
					case *ast.NativeStruct:
						goType = reflect.PointerTo(stru.Type)
					}
				}
			}
			return NewVar(n.Type, goType, nil, ctx.Stack), nil
		}
		return nil, fmt.Errorf("unexpected literal type: %s", n.Type)
	case *ast.BinaryExpr:
		left, err := e.ExecExpr(ctx, n.Left)
		if err != nil {
			return nil, err
		}
		right, err := e.ExecExpr(ctx, n.Right)
		if err != nil {
			return nil, err
		}

		if n.Operator == "Eq" || n.Operator == "Neq" {
			res := e.compareData(left.Data, right.Data)
			if n.Operator == "Neq" {
				res = !res
			}
			return NewVar("Bool", reflect.TypeOf(ast.MiniBool{}), ast.NewMiniBool(res), ctx.Stack), nil
		}
		return nil, fmt.Errorf("unsupported binary operator in executor: %s", n.Operator)
	case *ast.IdentifierExpr:
		return ctx.Load(string(n.Name))
	case *ast.ConstRefExpr:
		if i, ok := e.funcs[n.Name]; ok {
			return i, nil
		}
		if i, ok := e.consts[string(n.Name)]; ok {
			return NewVar("Constant", reflect.TypeOf(i), i, nil), nil
		}
		if i, ok := e.program.Functions[n.Name]; ok {
			functionType := i.ToCallFunctionType()
			return NewVar(functionType.MiniType(), reflect.TypeOf(i), i, nil), nil
		}
		if v, err := e.resolveArrayMapMethod(n.Name); err == nil {
			return v, nil
		}
		return nil, errors.New(string("unknown const: " + n.Name))
	case *ast.StructCallExpr:
		obj, err := e.ExecExpr(ctx, n.Object)
		if err != nil {
			return nil, err
		}

		miniType := obj.Type
		if miniType.IsPtr() {
			miniType, _ = miniType.GetPtrElementType()
		}

		// Use the object's actual data type if it's Any
		if miniType.IsAny() && obj.Data != nil {
			if miniObj, ok := obj.Data.(ast.MiniObj); ok {
				miniType = ast.OPSType(miniObj.OPSType())
			} else {
				// Try pointer
				rv := reflect.ValueOf(obj.Data)
				if rv.Kind() == reflect.Ptr {
					if !rv.IsNil() {
						if miniObj, ok := rv.Interface().(ast.MiniObj); ok {
							miniType = ast.OPSType(miniObj.OPSType())
						}
					}
				} else {
					// Try address of value
					ptr := reflect.New(rv.Type())
					ptr.Elem().Set(rv)
					if miniObj, ok := ptr.Interface().(ast.MiniObj); ok {
						miniType = ast.OPSType(miniObj.OPSType())
					}
				}
			}
		}

		methodName := ast.Ident(fmt.Sprintf("__obj__%s__%s", miniType, n.Name))
		methodVar, ok := e.funcs[methodName]
		if !ok {
			// Try pointer receiver if non-pointer not found
			ptrMethodName := ast.Ident(fmt.Sprintf("__obj__%s__%s", miniType.ToPtr(), n.Name))
			if mv, ok2 := e.funcs[ptrMethodName]; ok2 {
				methodVar = mv
				ok = true
			}
		}

		if !ok {
			// Try resolveArrayMapMethod if it's a built-in array/map method
			if mv, err := e.resolveArrayMapMethod(methodName); err == nil {
				methodVar = mv
			} else {
				return nil, fmt.Errorf("method %s not found for type %s", n.Name, miniType)
			}
		}

		var argsRef []reflect.Value
		var args []*Var

		// Add object (receiver) as first arg
		args = append(args, obj)
		argsRef = append(argsRef, reflect.ValueOf(obj.Data))

		for _, arg := range n.Args {
			ExecExpr, err := e.ExecExpr(ctx, arg)
			if err != nil {
				return nil, err
			}
			args = append(args, ExecExpr)
			var value reflect.Value
			if ExecExpr.Data == nil {
				if ExecExpr.GoType != nil {
					value = reflect.Zero(ExecExpr.GoType)
				} else {
					value = reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem())
				}
			} else {
				if ExecExpr.GoType != nil {
					value = reflect.ValueOf(ExecExpr.Data).Convert(ExecExpr.GoType)
				} else {
					value = reflect.ValueOf(ExecExpr.Data)
				}
			}
			argsRef = append(argsRef, value)
		}

		if methodVar.GoType.Kind() == reflect.Func {
			funcValue := reflect.ValueOf(methodVar.Data)
			if !funcValue.IsValid() || funcValue.Kind() != reflect.Func {
				funcValue = methodVar.Value
			}
			ft := methodVar.GoType
			numIn := ft.NumIn()

			var call []reflect.Value
			offset := 0
			if numIn > 0 && ft.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
				stmtCtx := context.WithValue(ctx.Context, ContextKeyNodeMeta, n.BaseNode)
				call = append(call, reflect.ValueOf(stmtCtx))
				offset = 1
			}

			if len(argsRef) != numIn-offset {
				return nil, fmt.Errorf("函数参数数量不一致: 期望 %d, 实际 %d", numIn-offset, len(argsRef))
			}

			call, err = e.prepareCallArgs(ft, offset, args, argsRef, call)
			if err != nil {
				return nil, err
			}

			call, err = e.safeCall(funcValue, call)
			if err != nil {
				return nil, err
			}
			callFunc, _ := methodVar.Type.ReadCallFunc()
			return e.callRetParser(methodVar.GoType, call, callFunc.Returns)
		}
		return nil, fmt.Errorf("invalid method type: %T", methodVar.Data)
	case *ast.CallExprStmt:
		expr, err := e.ExecExpr(ctx, n.Func)
		if err != nil {
			return nil, err
		}
		var argsRef []reflect.Value
		var args []*Var
		for _, arg := range n.Args {
			ExecExpr, err := e.ExecExpr(ctx, arg)
			if err != nil {
				return nil, err
			}
			args = append(args, ExecExpr)
			var value reflect.Value
			if ExecExpr.Data == nil {
				// Handle nil data
				if ExecExpr.GoType != nil {
					value = reflect.Zero(ExecExpr.GoType)
				} else {
					value = reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem())
				}
			} else {
				if ExecExpr.GoType != nil {
					value = reflect.ValueOf(ExecExpr.Data).Convert(ExecExpr.GoType)
				} else {
					value = reflect.ValueOf(ExecExpr.Data)
				}
			}
			argsRef = append(argsRef, value)
		}
		callFunc, _ := expr.Type.ReadCallFunc()
		if expr.GoType.Kind() == reflect.Func {
			funcValue := reflect.ValueOf(expr.Data)
			if !funcValue.IsValid() || funcValue.Kind() != reflect.Func {
				funcValue = expr.Value
			}
			ft := expr.GoType
			numIn := ft.NumIn()

			var call []reflect.Value
			offset := 0
			if numIn > 0 && ft.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
				stmtCtx := context.WithValue(ctx.Context, ContextKeyNodeMeta, n.BaseNode)
				call = append(call, reflect.ValueOf(stmtCtx))
				offset = 1
			}

			// 处理 Eq/Neq 且包含 nil 的情况，避免进入 Mini 对象内部 panic
			methodName := ""
			if cr, ok := n.Func.(*ast.ConstRefExpr); ok {
				if strings.HasSuffix(string(cr.Name), "__Eq") {
					methodName = "Eq"
				} else if strings.HasSuffix(string(cr.Name), "__Neq") {
					methodName = "Neq"
				}
			}

			if methodName != "" && len(argsRef) == 2 {
				leftNil := args[0].Data == nil
				rightNil := args[1].Data == nil
				if leftNil || rightNil {
					res := leftNil == rightNil
					if methodName == "Neq" {
						res = !res
					}
					return NewVar("Bool", reflect.TypeOf(ast.MiniBool{}), ast.NewMiniBool(res), ctx.Stack), nil
				}
			}

			if len(argsRef) != numIn-offset {
				return nil, fmt.Errorf("函数参数数量不一致: 期望 %d, 实际 %d", numIn-offset, len(argsRef))
			}

			call, err = e.prepareCallArgs(ft, offset, args, argsRef, call)
			if err != nil {
				return nil, err
			}

			call, err = e.safeCall(funcValue, call)
			if err != nil {
				return nil, err
			}
			return e.callRetParser(expr.GoType, call, callFunc.Returns)
		}
		if funcStmt, ok := expr.Data.(*ast.FunctionStmt); ok {
			var result *Var
			err := ctx.WithFuncScope(string(funcStmt.Name), func(old *Stack, c *StackContext) error {
				defer c.ExecuteDeferred() // 执行本层作用域的 defer (包含参数和 __return__)
				for i, param := range funcStmt.Params {
					if callErr := c.NewVar(string(param.Name), param.Type); callErr != nil {
						return callErr
					}
					if callErr := c.Store(string(param.Name), args[i]); callErr != nil {
						return callErr
					}
				}
				if !funcStmt.Return.IsVoid() {
					if callErr := c.NewVar("__return__", funcStmt.Return); callErr != nil {
						return callErr
					}
				}
				callErr := func() error {
					c.ScopeApply("function")
					defer c.ScopeExit()
					defer c.ExecuteDeferred() // 确保在 return 或其他中断后也能执行，且在 ScopeExit 之前
					err := e.execStmts(c, funcStmt.Body.Children)
					return err
				}()
				if callErr != nil {
					return callErr
				}
				if !funcStmt.Return.IsVoid() {
					result, callErr = c.loadVar("__return__")
					if callErr != nil {
						return callErr
					}
				} else {
					result = NewVar("Void", nil, nil, old)
				}
				if rr, ok := result.Data.([]*Var); ok {
					for _, v := range rr {
						v.stack = weak.Make(old)
					}
				}
				result.stack = weak.Make(old)
				return nil
			})
			if err != nil {
				return nil, err
			}
			return result, nil
		}
		return nil, fmt.Errorf("expr:%v ", expr)
	case *ast.MemberExpr:
		expr, err := e.ExecExpr(ctx, n.Object)
		if err != nil {
			return nil, err
		}
		struName := string(expr.Type)
		if elementType, b := expr.Type.GetPtrElementType(); b {
			struName = string(elementType)
		}
		if structDef, ok := e.structs[struName]; ok {
			switch stru := structDef.(type) {
			case *ast.NativeStruct:
				val := reflect.ValueOf(expr.Data)
				if val.Kind() == reflect.Ptr {
					val = val.Elem()
				}
				data := val.FieldByName(string(n.Property))
				return NewVar(stru.Fields[n.Property], data.Type(), data.Interface(), ctx.Stack), nil
			case *ast.StructStmt:
				data := expr.Data.(*DynStruct)
				item := data.Body[string(n.Property)]
				return NewVar(data.Define.Fields[n.Property], reflect.TypeOf(item), item, ctx.Stack), nil
			}
		}
	case *ast.CompositeExpr:
		if ast.OPSType(n.Kind).IsArray() {
			var slice []interface{}
			for _, value := range n.Values {
				v, err := e.ExecExpr(ctx, value.Value)
				if err != nil {
					return nil, err
				}
				slice = append(slice, v.Data)
			}
			return NewVar(ast.OPSType(n.Kind), reflect.TypeOf(&slice), &slice, ctx.Stack), nil
		}
		if ast.OPSType(n.Kind).IsMap() {
			m := make(map[interface{}]interface{})
			for _, value := range n.Values {
				k, err := e.ExecExpr(ctx, value.Key)
				if err != nil {
					return nil, err
				}
				v, err := e.ExecExpr(ctx, value.Value)
				if err != nil {
					return nil, err
				}
				key := k.Data
				if ks, ok := k.Data.(ast.MiniString); ok {
					key = ks.GoString()
				} else if ks, ok := k.Data.(*ast.MiniString); ok {
					key = ks.GoString()
				}
				m[key] = v.Data
			}
			return NewVar(ast.OPSType(n.Kind), reflect.TypeOf(m), m, ctx.Stack), nil
		}
		if structDef, ok := e.structs[string(n.Kind)]; ok {
			// 定义普通的变量
			switch stru := structDef.(type) {
			case *ast.NativeStruct:
				ptr := reflect.New(stru.Type)
				val := ptr.Elem()

				// 填充字段
				for _, elem := range n.Values {
					if elem.Key == nil {
						continue
					}
					k, err := e.ExecExpr(ctx, elem.Key)
					if err != nil {
						return nil, err
					}
					v, err := e.ExecExpr(ctx, elem.Value)
					if err != nil {
						return nil, err
					}

					var fieldName string
					if ks, ok := k.Data.(ast.MiniString); ok {
						fieldName = ks.GoString()
					} else if ks, ok := k.Data.(*ast.MiniString); ok {
						fieldName = ks.GoString()
					} else {
						fieldName = fmt.Sprintf("%v", k.Data)
					}

					field := val.FieldByName(fieldName)
					if field.IsValid() && field.CanSet() {
						field.Set(reflect.ValueOf(v.Data))
					}
				}

				return NewVar(ast.OPSType(stru.StructName), stru.Type, val.Interface(), ctx.Stack), nil
			case *ast.StructStmt:
				data := DynStruct{
					Define: stru,
					Body:   make(map[string]any),
				}
				for ident := range stru.Fields {
					data.Body[string(ident)] = nil
				}
				for _, value := range n.Values {
					k, err := e.ExecExpr(ctx, value.Key)
					if err != nil {
						return nil, err
					}
					v, err := e.ExecExpr(ctx, value.Value)
					if err != nil {
						return nil, err
					}
					var key string
					if miniString, ok := k.Data.(ast.MiniString); ok {
						key = miniString.GoString()
					} else if miniString, ok := k.Data.(*ast.MiniString); ok {
						key = miniString.GoString()
					} else {
						// Fallback or error?
						// Assuming it is string for now based on struct definition
						key = fmt.Sprintf("%v", k.Data)
					}

					if _, ok := data.Body[key]; ok {
						data.Body[key] = v.Data
					}
				}
				return NewVar(ast.OPSType(stru.Name), reflect.TypeOf(data), data, ctx.Stack), nil
			}
		}
	case *ast.IndexExpr:
		obj, err := e.ExecExpr(ctx, n.Object)
		if err != nil {
			return nil, err
		}
		idx, err := e.ExecExpr(ctx, n.Index)
		if err != nil {
			return nil, err
		}
		if obj.Type.IsArray() {
			slicePtr, ok := obj.Data.(*[]interface{})
			if !ok {
				return nil, fmt.Errorf("object is not array pointer: %T", obj.Data)
			}
			slice := *slicePtr
			var index int
			if i, ok := idx.Data.(int); ok {
				index = i
			} else if miniNum, ok := idx.Data.(*ast.MiniNumber); ok {
				index = int(miniNum.GoValue().(int64))
			} else if miniNum, ok := idx.Data.(ast.MiniNumber); ok {
				index = int(miniNum.GoValue().(int64))
			} else {
				return nil, fmt.Errorf("invalid index type: %T", idx.Data)
			}
			if index < 0 || index >= len(slice) {
				return nil, errors.New("index out of bounds")
			}
			val := slice[index]
			elemType, _ := obj.Type.ReadArrayItemType()
			return NewVar(elemType, reflect.TypeOf(val), val, ctx.Stack), nil
		}
		if obj.Type.IsMap() {
			m, ok := obj.Data.(map[interface{}]interface{})
			if !ok {
				return nil, fmt.Errorf("object is not map: %T", obj.Data)
			}
			key := idx.Data
			if ks, ok := idx.Data.(ast.MiniString); ok {
				key = ks.GoString()
			} else if ks, ok := idx.Data.(*ast.MiniString); ok {
				key = ks.GoString()
			}
			val, ok := m[key]
			_, valType, _ := obj.Type.GetMapKeyValueTypes()
			if !ok {
				// Return zero value for the type if possible, or nil
				// For now, returning nil with correct type hint
				return NewVar(valType, nil, nil, ctx.Stack), nil
			}
			return NewVar(valType, reflect.TypeOf(val), val, ctx.Stack), nil
		}
	case *ast.AddressExpr:
		// 优先处理可寻址的表达式（变量、成员访问）
		switch op := n.Operand.(type) {
		case *ast.IdentifierExpr:
			addr, err := ctx.LoadAddr(string(op.Name))
			if err == nil {
				v, _ := ctx.Load(string(op.Name))
				return NewVarWithValue(v.Type.ToPtr(), addr, ctx.Stack), nil
			}
		case *ast.MemberExpr:
			// 递归获取对象的 Value
			obj, err := e.ExecExpr(ctx, op.Object)
			if err != nil {
				return nil, err
			}

			// 只有当对象容器本身可寻址，或者它是一个指针时，我们才能获取字段地址
			val := obj.Value
			if val.Kind() == reflect.Interface {
				val = val.Elem()
			}

			if val.Kind() == reflect.Ptr {
				val = val.Elem()
			}

			if val.Kind() == reflect.Struct {
				field := val.FieldByName(string(op.Property))
				if field.IsValid() && field.CanAddr() {
					addr := field.Addr()
					struName := string(obj.Type)
					if et, b := obj.Type.GetPtrElementType(); b {
						struName = string(et)
					}
					var fieldType ast.OPSType
					if sDef, ok := e.structs[struName]; ok {
						if ns, ok2 := sDef.(*ast.NativeStruct); ok2 {
							fieldType = ns.Fields[op.Property]
						}
					}
					return NewVarWithValue(fieldType.ToPtr(), addr, ctx.Stack), nil
				}
			}
		}

		// Fallback: 对副本进行提升（针对右值/字面量）
		expr, err := e.ExecExpr(ctx, n.Operand)
		if err != nil {
			return nil, err
		}

		data := expr.Data
		value := reflect.New(expr.GoType)
		value.Elem().Set(reflect.ValueOf(data))
		return NewVarWithValue(expr.Type.ToPtr(), value, expr.stack.Value()), nil
	case *ast.DerefExpr:
		expr, err := e.ExecExpr(ctx, n.Operand)
		if err != nil {
			return nil, err
		}
		if expr.Data == nil {
			return nil, errors.New("dereference of nil pointer")
		}
		rv := reflect.ValueOf(expr.Data)
		if rv.Kind() != reflect.Ptr {
			return nil, fmt.Errorf("cannot dereference non-pointer type %T", expr.Data)
		}
		elemType, _ := expr.Type.GetPtrElementType()
		data := expr.Data
		if _, ok := expr.Data.(ast.MiniObj); !ok {
			data = rv.Elem().Interface()
		}
		return NewVar(elemType, rv.Elem().Type(), data, ctx.Stack), nil
	}
	return nil, errors.New("todo: " + s.GetBase().Meta)
}

// 处理函数调用相关的内容
func (e *Executor) callRetParser(funcCall reflect.Type, call []reflect.Value, returnType ast.OPSType) (*Var, error) {
	if returnType.IsVoid() {
		if len(call) > 0 {
			errInter := call[0].Interface()
			if errInter != nil {
				if err, ok := errInter.(error); ok {
					return nil, err
				}
				return nil, fmt.Errorf("%v", errInter)
			}
		}
		return nil, nil
	}
	resultTypes, isTuple := returnType.ReadTuple()
	if isTuple {
		if len(resultTypes) != len(call) {
			if len(call)-len(resultTypes) != 1 {
				return nil, fmt.Errorf("错误的函数返回调用: %s != %v", returnType, funcCall)
			}
			// 此时最后一位为 error
			errInter := call[len(resultTypes)].Interface()
			if errInter != nil {
				if err, ok := errInter.(error); ok {
					return nil, err
				}
				return nil, fmt.Errorf("%v", errInter)
			}
		}
		// 移除 error
		call = call[:len(call)-1]
	} else {
		// 不是 tunple ，改为
		resultTypes = []ast.OPSType{returnType}
		if len(call) > 1 {
			if len(call) != 2 {
				return nil, fmt.Errorf("错误的函数返回调用: %s != %v", returnType, funcCall)
			}
			errInter := call[1].Interface()
			if errInter != nil {
				if err, ok := errInter.(error); ok {
					return nil, err
				}
				return nil, fmt.Errorf("%v", errInter)
			}
			// 移除 error
			call = call[:len(call)-1]
		}
	}

	var results []*Var
	for i, o := range resultTypes {
		outType := funcCall.Out(i)
		val := call[i].Interface()

		if o.IsArray() {
			rv := reflect.ValueOf(val)
			if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
				if _, ok := val.(*[]interface{}); !ok {
					newSlice := make([]interface{}, rv.Len())
					for j := 0; j < rv.Len(); j++ {
						newSlice[j] = rv.Index(j).Interface()
					}
					val = &newSlice
					outType = reflect.TypeOf(val)
				}
			}
		}

		// 如果返回类型是 interface{}，尝试使用值的实际类型
		if outType.Kind() == reflect.Interface && val != nil {
			outType = reflect.TypeOf(val)
		}

		actualType := o
		if val != nil {
			rv := reflect.ValueOf(val)
			if miniObj, ok := val.(ast.MiniObj); ok {
				actualType = ast.OPSType(miniObj.OPSType())
				if rv.Kind() == reflect.Ptr {
					actualType = actualType.ToPtr()
				}
			} else if rv.Kind() == reflect.Ptr && !rv.IsNil() {
				if miniObj, ok := rv.Interface().(ast.MiniObj); ok {
					actualType = ast.OPSType(miniObj.OPSType()).ToPtr()
				}
			}
		}

		results = append(results, NewVar(actualType, outType, val, nil))
	}
	if returnType.IsTuple() {
		return NewVar(returnType, reflect.TypeOf(results), results, nil), nil
	}
	if len(call) != 1 {
		return nil, fmt.Errorf("错误的函数返回调用: %s != %v", returnType, funcCall)
	}
	return results[0], nil
}

func (e *Executor) prepareCallArgs(ft reflect.Type, offset int, args []*Var, argsRef, initialCall []reflect.Value) ([]reflect.Value, error) {
	call := initialCall
	for i := range argsRef {
		var targetType reflect.Type
		if ft.IsVariadic() && i+offset >= ft.NumIn()-1 {
			targetType = ft.In(ft.NumIn() - 1)
		} else {
			targetType = ft.In(i + offset)
		}

		v := args[i]
		arg := v.Value

		if v.Data == nil {
			call = append(call, reflect.Zero(targetType))
		} else if arg.Type().AssignableTo(targetType) {
			call = append(call, arg)
		} else if targetType.Kind() == reflect.Ptr && arg.Type().AssignableTo(targetType.Elem()) {
			if arg.CanAddr() {
				call = append(call, arg.Addr())
			} else {
				ptr := reflect.New(arg.Type())
				ptr.Elem().Set(arg)
				call = append(call, ptr)
			}
		} else if arg.Kind() == reflect.Ptr && arg.Elem().Type().AssignableTo(targetType) {
			call = append(call, arg.Elem())
		} else if arg.Type().ConvertibleTo(targetType) {
			call = append(call, arg.Convert(targetType))
		} else {
			// 尝试自动解包 GoValueMini
			unwrapped := false
			goVal := e.toGoValue(v.Data)
			if goVal != v.Data {
				rv := reflect.ValueOf(goVal)
				if rv.Type().AssignableTo(targetType) {
					call = append(call, rv)
					unwrapped = true
				} else if rv.Type().ConvertibleTo(targetType) {
					call = append(call, rv.Convert(targetType))
					unwrapped = true
				}
			}

			if !unwrapped {
				// 特殊处理变长参数打包后的切片元素解包
				if ft.IsVariadic() && i+offset == ft.NumIn()-1 && targetType.Kind() == reflect.Slice {
					tempArg := arg
					for tempArg.Kind() == reflect.Ptr && !tempArg.IsNil() {
						if tempArg.Elem().Kind() == reflect.Slice {
							slice := tempArg.Elem()
							newSlice := reflect.MakeSlice(targetType, slice.Len(), slice.Len())
							elemType := targetType.Elem()
							for j := 0; j < slice.Len(); j++ {
								elem := slice.Index(j).Interface()
								rv := reflect.ValueOf(elem)
								// 优先检查原始值是否可以直接使用（处理 *ast.MiniString 等 Mini 对象）
								if rv.IsValid() {
									if rv.Type().AssignableTo(elemType) {
										newSlice.Index(j).Set(rv)
										continue
									} else if rv.Type().ConvertibleTo(elemType) {
										newSlice.Index(j).Set(rv.Convert(elemType))
										continue
									}
								}

								// 降级使用 GoValue 解包（处理基础类型 String, Int 等）
								unwrappedElem := e.toGoValue(elem)
								rv = reflect.ValueOf(unwrappedElem)
								if rv.IsValid() {
									if rv.Type().AssignableTo(elemType) {
										newSlice.Index(j).Set(rv)
									} else if rv.Type().ConvertibleTo(elemType) {
										newSlice.Index(j).Set(rv.Convert(elemType))
									} else {
										// 尝试递归解包或强转
										if rv.Kind() == reflect.Ptr && rv.Elem().Kind() == reflect.Slice && elemType.Kind() != reflect.Slice {
											// 如果解包出来还是个切片，但目标是单个元素，说明发生了意外的二次打包
											// 取第一个元素（防御性编程）
											if rv.Elem().Len() > 0 {
												first := rv.Elem().Index(0).Interface()
												rvFirst := reflect.ValueOf(e.toGoValue(first))
												if rvFirst.Type().AssignableTo(elemType) {
													newSlice.Index(j).Set(rvFirst)
													continue
												}
											}
										}
										newSlice.Index(j).Set(reflect.ValueOf(unwrappedElem).Convert(elemType))
									}
								} else {
									newSlice.Index(j).Set(reflect.Zero(elemType))
								}
							}
							call = append(call, newSlice)
							unwrapped = true
							break
						}
						tempArg = tempArg.Elem()
					}
				}
			}

			if !unwrapped {
				return nil, fmt.Errorf("函数参数类型不匹配: 期望 %v, 实际 %v (Data: %T)", targetType, arg.Type(), v.Data)
			}
		}
	}
	return call, nil
}

func (e *Executor) compareData(a, b any) bool {
	if a == b {
		return true
	}

	isNil := func(v any) bool {
		if v == nil {
			return true
		}
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Chan, reflect.Func, reflect.Map, reflect.Ptr, reflect.UnsafePointer, reflect.Interface, reflect.Slice:
			return rv.IsNil()
		}
		return false
	}

	isNilA := isNil(a)
	isNilB := isNil(b)

	if isNilA && isNilB {
		return true
	}
	if isNilA || isNilB {
		return false
	}

	return reflect.DeepEqual(a, b)
}

func (e *Executor) toGoValue(v any) any {
	if v == nil {
		return nil
	}
	if gv, ok := v.(ast.GoValueMini); ok {
		return gv.GoValue()
	}
	rv := reflect.ValueOf(v)
	if rv.IsValid() && rv.Kind() != reflect.Ptr {
		// 尝试取地址断言
		ptr := reflect.New(rv.Type())
		ptr.Elem().Set(rv)
		if gv, ok := ptr.Interface().(ast.GoValueMini); ok {
			return gv.GoValue()
		}
	}
	return v
}

func (e *Executor) AddGlobalFunc(name ast.Ident, miniType ast.OPSType, f any) {
	e.funcs[name] = NewVar(miniType, reflect.TypeOf(f), reflect.ValueOf(f), e.ctx.Stack)
}

func (e *Executor) AddStruct(stdlibStruct any) error {
	var pkg, name string
	// Check if it's wrapped to override namespace
	actualStruct := stdlibStruct
	if wrapper, ok := stdlibStruct.(ast.PackageStructWrapper); ok {
		pkg = wrapper.Pkg
		name = wrapper.Name
		actualStruct = wrapper.Stru
	}

	native, err := ast.ParseNative(reflect.TypeOf(actualStruct).Elem())
	if err != nil {
		return err
	}

	if pkg != "" && name != "" {
		native.StructName = ast.Ident(fmt.Sprintf("%s.%s", pkg, name))
	}

	e.structs[string(native.StructName)] = native
	for ident, functionType := range native.Methods {
		meth, b := reflect.PointerTo(native.Type).MethodByName(string(ident))
		if !b {
			return fmt.Errorf("%s method not found", ident)
		}
		e.funcs[ast.Ident(fmt.Sprintf("__obj__%s__%s", native.StructName, ident))] = NewVar(functionType.MiniType(), meth.Type, meth.Func, e.ctx.Stack)
	}
	if native.LiteralNew {
		callFunc, _ := ast.OPSType(fmt.Sprintf("function(Constant) %s", native.StructName)).ReadCallFunc()
		funcType := reflect.FuncOf([]reflect.Type{reflect.TypeOf("")}, []reflect.Type{native.Type}, false)
		var lErr error
		fc := reflect.MakeFunc(funcType, func(args []reflect.Value) []reflect.Value {
			input := args[0].String()
			value := reflect.New(native.Type).Interface().(ast.MiniObjLiteral)
			obj, err := value.New(input)
			if err != nil {
				lErr = err
				return make([]reflect.Value, 0)
			}
			return []reflect.Value{reflect.ValueOf(obj).Elem().Convert(native.Type)}
		})
		if lErr != nil {
			return lErr
		}
		e.funcs[ast.Ident(fmt.Sprintf("__obj__new__%s", native.StructName))] = NewVar(callFunc.MiniType(), funcType, fc, e.ctx.Stack)
	}
	return nil
}

func (e *Executor) resolveArrayMapMethod(name ast.Ident) (*Var, error) {
	sName := string(name)
	if !strings.HasPrefix(sName, "__obj__") {
		return nil, errors.New("not a method")
	}
	lastIdx := strings.LastIndex(sName, "__")
	if lastIdx == -1 {
		return nil, errors.New("invalid format")
	}
	typeStr := sName[7:lastIdx]
	method := sName[lastIdx+2:]

	miniType := ast.OPSType(typeStr)

	if miniType.IsArray() {
		return e.createArrayMethod(miniType, method)
	}
	if miniType.IsMap() {
		return e.createMapMethod(miniType, method)
	}
	return nil, errors.New("unknown type")
}

func (e *Executor) createArrayMethod(miniType ast.OPSType, method string) (*Var, error) {
	elemType, _ := miniType.ReadArrayItemType()

	switch method {
	case "get":
		// function(arr, index) -> value, error
		return e.makeFn(fmt.Sprintf("function(%s, Number) %s", miniType, elemType), 2, 2, func(args []reflect.Value) []reflect.Value {
			arrPtr, ok := args[0].Interface().(*[]interface{})
			if !ok {
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem()), reflect.ValueOf(errors.New("invalid array"))}
			}
			idxVal := args[1].Interface()
			var idx int
			if i, ok := idxVal.(int); ok {
				idx = i
			} else if miniNum, ok := idxVal.(*ast.MiniNumber); ok {
				idx = int(miniNum.GoValue().(int64))
			} else if miniNum, ok := idxVal.(ast.MiniNumber); ok {
				idx = int(miniNum.GoValue().(int64))
			} else {
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem()), reflect.ValueOf(fmt.Errorf("invalid index type: %T", idxVal))}
			}
			arr := *arrPtr
			if idx < 0 || idx >= len(arr) {
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem()), reflect.ValueOf(errors.New("index out of bounds"))}
			}
			return []reflect.Value{reflect.ValueOf(arr[idx]), reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	case "length":
		// function(arr) -> number, error
		return e.makeFn(fmt.Sprintf("function(%s) Number", miniType), 1, 1, func(args []reflect.Value) []reflect.Value {
			arrPtr, ok := args[0].Interface().(*[]interface{})
			if !ok {
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem()), reflect.ValueOf(errors.New("invalid array"))}
			}
			return []reflect.Value{reflect.ValueOf(ast.NewMiniNumber(int64(len(*arrPtr))))}
		}), nil
	case "push":
		// function(arr, val) -> void (error)
		return e.makeFn(fmt.Sprintf("function(%s, %s) Void", miniType, elemType), 2, 1, func(args []reflect.Value) []reflect.Value {
			arrPtr, ok := args[0].Interface().(*[]interface{})
			if !ok {
				return []reflect.Value{reflect.ValueOf(errors.New("invalid array"))}
			}
			val := args[1].Interface()
			*arrPtr = append(*arrPtr, val)
			return []reflect.Value{reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	case "set":
		// function(arr, idx, val) -> void (error)
		return e.makeFn(fmt.Sprintf("function(%s, Number, %s) Void", miniType, elemType), 3, 1, func(args []reflect.Value) []reflect.Value {
			arrPtr, ok := args[0].Interface().(*[]interface{})
			if !ok {
				return []reflect.Value{reflect.ValueOf(errors.New("invalid array"))}
			}
			idxVal := args[1].Interface()
			var idx int
			if i, ok := idxVal.(int); ok {
				idx = i
			} else if miniNum, ok := idxVal.(*ast.MiniNumber); ok {
				idx = int(miniNum.GoValue().(int64))
			} else if miniNum, ok := idxVal.(ast.MiniNumber); ok {
				idx = int(miniNum.GoValue().(int64))
			} else {
				return []reflect.Value{reflect.ValueOf(fmt.Errorf("invalid index type: %T", idxVal))}
			}
			if idx < 0 || idx >= len(*arrPtr) {
				return []reflect.Value{reflect.ValueOf(errors.New("index out of bounds"))}
			}
			(*arrPtr)[idx] = args[2].Interface()
			return []reflect.Value{reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	case "remove":
		// function(arr, idx) -> void (error)
		return e.makeFn(fmt.Sprintf("function(%s, Number) Void", miniType), 2, 1, func(args []reflect.Value) []reflect.Value {
			arrPtr, ok := args[0].Interface().(*[]interface{})
			if !ok {
				return []reflect.Value{reflect.ValueOf(errors.New("invalid array"))}
			}
			idxVal := args[1].Interface()
			var idx int
			if i, ok := idxVal.(int); ok {
				idx = i
			} else if miniNum, ok := idxVal.(*ast.MiniNumber); ok {
				idx = int(miniNum.GoValue().(int64))
			} else if miniNum, ok := idxVal.(ast.MiniNumber); ok {
				idx = int(miniNum.GoValue().(int64))
			} else {
				return []reflect.Value{reflect.ValueOf(fmt.Errorf("invalid index type: %T", idxVal))}
			}
			if idx < 0 || idx >= len(*arrPtr) {
				return []reflect.Value{reflect.ValueOf(errors.New("index out of bounds"))}
			}
			*arrPtr = append((*arrPtr)[:idx], (*arrPtr)[idx+1:]...)
			return []reflect.Value{reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	}
	return nil, errors.New("method not implemented")
}

func (e *Executor) makeFn(sig string, inCount, outCount int, fn func([]reflect.Value) []reflect.Value) *Var {
	in := make([]reflect.Type, inCount)
	for i := 0; i < inCount; i++ {
		in[i] = reflect.TypeOf((*interface{})(nil)).Elem()
	}
	out := make([]reflect.Type, outCount)
	for i := 0; i < outCount; i++ {
		out[i] = reflect.TypeOf((*interface{})(nil)).Elem()
	}
	return NewVar(ast.OPSType(sig), reflect.FuncOf(in, out, false), reflect.MakeFunc(reflect.FuncOf(in, out, false), fn), nil)
}

func (e *Executor) createMapMethod(miniType ast.OPSType, method string) (*Var, error) {
	keyType, valueType, _ := miniType.GetMapKeyValueTypes()

	getMap := func(v interface{}) (map[interface{}]interface{}, error) {
		if m, ok := v.(map[interface{}]interface{}); ok {
			return m, nil
		}
		return nil, errors.New("invalid map")
	}

	getKey := func(v interface{}) interface{} {
		if ks, ok := v.(ast.MiniString); ok {
			return ks.GoString()
		}
		if ks, ok := v.(*ast.MiniString); ok {
			return ks.GoString()
		}
		return v
	}

	switch method {
	case "get":
		return e.makeFn(fmt.Sprintf("function(%s, %s) %s", miniType, keyType, valueType), 2, 2, func(args []reflect.Value) []reflect.Value {
			m, err := getMap(args[0].Interface())
			if err != nil {
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem()), reflect.ValueOf(err)}
			}
			key := getKey(args[1].Interface())
			val := m[key]
			var retVal reflect.Value
			if val == nil {
				retVal = reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem())
			} else {
				retVal = reflect.ValueOf(val)
			}
			return []reflect.Value{retVal, reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	case "put", "set":
		return e.makeFn(fmt.Sprintf("function(%s, %s, %s) Void", miniType, keyType, valueType), 3, 1, func(args []reflect.Value) []reflect.Value {
			m, err := getMap(args[0].Interface())
			if err != nil {
				return []reflect.Value{reflect.ValueOf(err)}
			}
			key := getKey(args[1].Interface())
			m[key] = args[2].Interface()
			return []reflect.Value{reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	case "remove":
		return e.makeFn(fmt.Sprintf("function(%s, %s) Void", miniType, keyType), 2, 1, func(args []reflect.Value) []reflect.Value {
			m, err := getMap(args[0].Interface())
			if err != nil {
				return []reflect.Value{reflect.ValueOf(err)}
			}
			key := getKey(args[1].Interface())
			delete(m, key)
			return []reflect.Value{reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	case "size":
		return e.makeFn(fmt.Sprintf("function(%s) Number", miniType), 1, 2, func(args []reflect.Value) []reflect.Value {
			m, err := getMap(args[0].Interface())
			if err != nil {
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem()), reflect.ValueOf(err)}
			}
			val, _ := (&ast.MiniNumber{}).New(strconv.Itoa(len(m)))
			return []reflect.Value{reflect.ValueOf(val), reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	case "contains":
		return e.makeFn(fmt.Sprintf("function(%s, %s) Bool", miniType, keyType), 2, 2, func(args []reflect.Value) []reflect.Value {
			m, err := getMap(args[0].Interface())
			if err != nil {
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem()), reflect.ValueOf(err)}
			}
			key := getKey(args[1].Interface())
			_, ok := m[key]
			val, _ := (&ast.MiniBool{}).New(strconv.FormatBool(ok))
			return []reflect.Value{reflect.ValueOf(val), reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	case "keys":
		return e.makeFn(fmt.Sprintf("function(%s) Array<%s>", miniType, keyType), 1, 2, func(args []reflect.Value) []reflect.Value {
			m, err := getMap(args[0].Interface())
			if err != nil {
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem()), reflect.ValueOf(err)}
			}
			var keys []interface{}
			for k := range m {
				if s, ok := k.(string); ok {
					miniStr := ast.NewMiniString(s)
					keys = append(keys, &miniStr)
				} else {
					keys = append(keys, k)
				}
			}
			return []reflect.Value{reflect.ValueOf(&keys), reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	case "values":
		return e.makeFn(fmt.Sprintf("function(%s) Array<%s>", miniType, valueType), 1, 2, func(args []reflect.Value) []reflect.Value {
			m, err := getMap(args[0].Interface())
			if err != nil {
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem()), reflect.ValueOf(err)}
			}
			var values []interface{}
			for _, v := range m {
				values = append(values, v)
			}
			return []reflect.Value{reflect.ValueOf(&values), reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}), nil
	}
	return nil, errors.New("method not implemented")
}

func (e *Executor) safeCall(fn reflect.Value, args []reflect.Value) (results []reflect.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("native call panic: %v", r)
		}
	}()
	if fn.Type().IsVariadic() && len(args) > 0 && args[len(args)-1].Kind() == reflect.Slice {
		results = fn.CallSlice(args)
	} else {
		results = fn.Call(args)
	}
	return results, err
}

type DynStruct struct {
	Define *ast.StructStmt
	Body   map[string]any
}
