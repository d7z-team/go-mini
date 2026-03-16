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
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type Executor struct {
	structs map[string]*ast.StructStmt
	consts  map[string]string
	funcs   map[ast.Ident]*Var
	program *ast.ProgramStmt
	ctx     *StackContext

	routes  map[string]FFIRoute // 显式映射外部函数名到 Bridge
	monitor MonitorManager

	activeHandles []handleRef
}

type handleRef struct {
	Bridge ffigo.FFIBridge
	ID     uint32
}

type MonitorManager interface {
	ReportProgram(state, message string, duration int)
	ReportStep(state string, meta ast.BaseNode, duration int)
}

type MiniRuntimeError struct {
	BaseNode ast.BaseNode
	Err      error
}

func (e *MiniRuntimeError) Error() string {
	if e.Err == nil {
		return "unknown error"
	}
	return e.Err.Error()
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
	defer func() {
		// Clean up all active handles to prevent memory leaks on VM exit
		for _, h := range e.activeHandles {
			if h.Bridge != nil && h.ID != 0 {
				h.Bridge.DestroyHandle(h.ID)
			}
		}
		e.activeHandles = nil
	}()

	e.ctx = &StackContext{
		Context:  ctx,
		Executor: e,
		Stack: &Stack{
			MemoryPtr: make(map[string]*Var),
			Scope:     "global",
			Depth:     1,
		},
	}

	// 注入内建 nil
	_ = e.ctx.AddVariable("nil", nil)

	// 初始化全局变量
	for name, expr := range e.program.Variables {
		val, err := e.ExecExpr(e.ctx, expr)
		if err != nil {
			return fmt.Errorf("failed to initialize global var %s: %w", name, err)
		}
		e.ctx.AddVariable(string(name), val)
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error("Executor panic", "error", r, "stack", string(debug.Stack()))
			if errRec, ok := r.(error); ok {
				err = errRec
			} else {
				err = fmt.Errorf("panic: %v", r)
			}
		}
	}()

	return e.execStmts(e.ctx, e.program.Main)
}

func (e *Executor) execStmts(ctx *StackContext, children []ast.Stmt) error {
	for _, child := range children {
		if ctx.Interrupt() {
			break
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
	case *ast.BlockStmt:
		if n.Inner {
			return e.execStmts(ctx, n.Children)
		}
		ctx.WithScope("block", func(ctx *StackContext) {
			err = e.execStmts(ctx, n.Children)
		})
		return err
	case *ast.GenDeclStmt:
		return ctx.NewVar(string(n.Name), n.Kind)
	case *ast.AssignmentStmt:
		val, err := e.ExecExpr(ctx, n.Value)
		if err != nil {
			return err
		}
		return ctx.Store(string(n.Variable), val)
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
				e.execStmt(ctx, n.Init.(ast.Stmt))
			}
			for {
				if n.Cond != nil {
					cond, execErr := e.ExecExpr(ctx, n.Cond)
					if execErr != nil || cond == nil || !cond.Bool {
						break
					}
				}
				if bodyErr := e.execStmts(ctx, n.Body.(*ast.BlockStmt).Children); bodyErr != nil {
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
					e.execStmt(ctx, n.Update.(ast.Stmt))
				}
			}
		})
		return forErr
	case *ast.InterruptStmt:
		if ctx.Stack != nil {
			ctx.Stack.interrupt = n.InterruptType
		}
		return nil
	case *ast.ReturnStmt:
		ctx.SetInterrupt("function", "return")
		if len(n.Results) > 0 {
			res, err := e.ExecExpr(ctx, n.Results[0])
			if err == nil && res != nil {
				ctx.Store("__return__", res)
			}
		}
		return nil
	case *ast.CallExprStmt:
		_, err := e.ExecExpr(ctx, n)
		return err
	case *ast.IncDecStmt:
		ident, _ := n.Operand.(*ast.IdentifierExpr)
		v, _ := ctx.Load(string(ident.Name))
		if v != nil {
			if n.Operator == "++" {
				v.I64++
			} else {
				v.I64--
			}
		}
		return nil
	}
	return fmt.Errorf("todo: stmt %T", s)
}

func (e *Executor) ExecExpr(ctx *StackContext, s ast.Expr) (v *Var, err error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	switch n := s.(type) {
	case *ast.LiteralExpr:
		return e.evalLiteral(n)
	case *ast.IdentifierExpr:
		return ctx.Load(string(n.Name))
	case *ast.BinaryExpr:
		l, _ := e.ExecExpr(ctx, n.Left)
		r, _ := e.ExecExpr(ctx, n.Right)
		return e.evalBinaryExprDirect(string(n.Operator), l, r)
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
	}
	return nil, fmt.Errorf("todo: expr %T", s)
}

func (e *Executor) evalMemberExpr(ctx *StackContext, n *ast.MemberExpr) (*Var, error) {
	obj, err := e.ExecExpr(ctx, n.Object)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("member access on nil object")
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
	}

	return nil, fmt.Errorf("type %v does not support member access", obj.VType)
}

func (e *Executor) evalCompositeExpr(ctx *StackContext, n *ast.CompositeExpr) (*Var, error) {
	if n.Type.IsArray() {
		res := make([]*Var, len(n.Values))
		for i, v := range n.Values {
			val, err := e.ExecExpr(ctx, v.Value)
			if err != nil { return nil, err }
			res[i] = val
		}
		return &Var{VType: TypeArray, Ref: &VMArray{Data: res}, Type: n.Type}, nil
	}
	
	// 结构体或 Map 字面量
	res := make(map[string]*Var)
	for _, v := range n.Values {
		keyName := ""
		if v.Key != nil {
			if ident, ok := v.Key.(*ast.IdentifierExpr); ok {
				keyName = string(ident.Name)
			} else {
				// 支持计算型 Key，如 { ["a"+"b"]: 1 }
				kVar, err := e.ExecExpr(ctx, v.Key)
				if err != nil { return nil, err }
				keyName = kVar.Str
			}
		}
		
		val, err := e.ExecExpr(ctx, v.Value)
		if err != nil { return nil, err }
		res[keyName] = val
	}
	
	if n.Type.IsMap() {
		return &Var{VType: TypeMap, Ref: &VMMap{Data: res}, Type: n.Type}, nil
	}
	
	// 默认视为普通结构体对象（在 VM 内部以 Map 形式存储）
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
	// 允许比较运算的操作数为 nil
	if operator == "==" || operator == "Eq" || operator == "!=" || operator == "Neq" {
		isLEmpty := l == nil || (l.VType == TypeAny && l.Ref == nil && l.I64 == 0 && l.Str == "") || (l.VType == TypeHandle && l.Handle == 0)
		isREmpty := r == nil || (r.VType == TypeAny && r.Ref == nil && r.I64 == 0 && r.Str == "") || (r.VType == TypeHandle && r.Handle == 0)

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

	if (l.VType == TypeString || l.VType == TypeBytes) && (r.VType == TypeString || r.VType == TypeBytes) {
		lStr := l.Str
		if l.VType == TypeBytes { lStr = string(l.B) }
		rStr := r.Str
		if r.VType == TypeBytes { rStr = string(r.B) }

		switch operator {
		case "==", "Eq": return NewBool(lStr == rStr), nil
		case "!=", "Neq": return NewBool(lStr != rStr), nil
		}
	}

	if l.VType == TypeInt && r.VType == TypeInt {
		switch operator {
		case "+", "Plus":
			return NewInt(l.I64 + r.I64), nil
		case "-", "Minus", "Sub":
			return NewInt(l.I64 - r.I64), nil
		case "*", "Mult":
			return NewInt(l.I64 * r.I64), nil
		case "/", "Div":
			return NewInt(l.I64 / r.I64), nil
		case "<", "Lt":
			return NewBool(l.I64 < r.I64), nil
		case ">", "Gt":
			return NewBool(l.I64 > r.I64), nil
		case "==", "Eq":
			return NewBool(l.I64 == r.I64), nil
		case "!=", "Neq":
			return NewBool(l.I64 != r.I64), nil
		}
	}
	return nil, fmt.Errorf("unsupported binary op %s between %v and %v", operator, l.VType, r.VType)
}

func (e *Executor) evalUnaryExprDirect(operator string, val *Var) (*Var, error) {
	if val == nil {
		return nil, errors.New("unary op with nil operand")
	}
	switch operator {
	case "!", "Not":
		return NewBool(!val.Bool), nil
	case "-", "Sub", "Minus":
		if val.VType == TypeInt {
			return NewInt(-val.I64), nil
		}
	}
	return nil, fmt.Errorf("unsupported unary op %s", operator)
}

func (e *Executor) evalCallExpr(ctx *StackContext, n *ast.CallExprStmt) (*Var, error) {
	var name string
	if ident, ok := n.Func.(*ast.ConstRefExpr); ok {
		name = string(ident.Name)
	} else if ident, ok := n.Func.(*ast.IdentifierExpr); ok {
		name = string(ident.Name)
	}

	if name != "" {
		// 内建 Intrinsics
		if name == "panic" {
			msg := "panic"
			if len(n.Args) > 0 {
				arg, _ := e.ExecExpr(ctx, n.Args[0])
				if arg != nil { msg = arg.Str }
			}
			panic(fmt.Errorf("mini-panic: %v", msg))
		}
		if name == "string" {
			if len(n.Args) > 0 {
				arg, _ := e.ExecExpr(ctx, n.Args[0])
				if arg == nil { return NewString(""), nil }
				switch arg.VType {
				case TypeString: return arg, nil
				case TypeBytes: return NewString(string(arg.B)), nil
				case TypeInt: return NewString(strconv.FormatInt(arg.I64, 10)), nil
				case TypeFloat: return NewString(strconv.FormatFloat(arg.F64, 'f', -1, 64)), nil
				case TypeBool: return NewString(strconv.FormatBool(arg.Bool)), nil
				}
			}
			return NewString(""), nil
		}
		if name == "[]byte" {
			if len(n.Args) > 0 {
				arg, _ := e.ExecExpr(ctx, n.Args[0])
				if arg == nil { return NewBytes(nil), nil }
				switch arg.VType {
				case TypeBytes: return arg, nil
				case TypeString: return NewBytes([]byte(arg.Str)), nil
				}
			}
			return NewBytes(nil), nil
		}
		if name == "len" {
			if len(n.Args) > 0 {
				arg, _ := e.ExecExpr(ctx, n.Args[0])
				if arg == nil { return NewInt(0), nil }
				switch arg.VType {
				case TypeString: return NewInt(int64(len(arg.Str))), nil
				case TypeBytes: return NewInt(int64(len(arg.B))), nil
				case TypeArray:
					arr := arg.Ref.(*VMArray)
					return NewInt(int64(len(arr.Data))), nil
				case TypeMap:
					m := arg.Ref.(*VMMap)
					return NewInt(int64(len(m.Data))), nil
				}
			}
			return NewInt(0), nil
		}

		// 内部函数
		if f, ok := e.program.Functions[ast.Ident(name)]; ok {
			args := make([]*Var, len(n.Args))
			for i, aExpr := range n.Args {
				var err error
				args[i], err = e.ExecExpr(ctx, aExpr)
				if err != nil { return nil, err }
			}

			var res *Var
			_ = ctx.WithFuncScope(name, func(old *Stack, c *StackContext) error {
				c.Executor = e
				for i, p := range f.Params {
					c.NewVar(string(p.Name), p.Type)
					if i < len(args) && args[i] != nil {
						c.Store(string(p.Name), args[i])
					}
				}
				if !f.Return.IsVoid() {
					c.NewVar("__return__", f.Return)
				}
				_ = e.execStmts(c, f.Body.Children)
				if !f.Return.IsVoid() {
					res, _ = c.loadVar("__return__")
				}
				return nil
			})
			return res, nil
		}

		// 2. FFI 外部调用 (如果内部未找到)
		if route, ok := e.routes[name]; ok {
			args := make([]*Var, len(n.Args))
			for i, aExpr := range n.Args {
				var err error
				args[i], err = e.ExecExpr(ctx, aExpr)
				if err != nil { return nil, err }
			}
			return e.evalFFI(route, args)
		}
	}

	return nil, fmt.Errorf("unsupported call expression: %v", n.Func)
}

func (e *Executor) evalFFI(route FFIRoute, args []*Var) (*Var, error) {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)

	// 获取函数签名以获取参数类型列表
	fn, ok := ast.GoMiniType(route.Spec).ReadCallFunc()

	// 序列化参数
	for i, arg := range args {
		var argType ast.GoMiniType = "Any"
		if ok && i < len(fn.Params) {
			argType = fn.Params[i]
			// 如果是变长参数的最后一个，且输入已经是打包好的数组，
			// 确保 argType 也是数组形式，以触发正确的序列化逻辑（写入长度）。
			if fn.Variadic && i == len(fn.Params)-1 {
				if !argType.IsArray() {
					argType = ast.CreateArrayType(argType)
				}
			}
		}
		if err := e.serializeVar(buf, arg, argType); err != nil {
			return nil, err
		}
	}

	// 呼叫 Bridge
	retData, err := route.Bridge.Call(route.MethodID, buf.Bytes())
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
			val, err := e.deserializeVar(reader, innerType, route.Bridge)
			if err != nil { return nil, err }
			return &Var{VType: TypeResult, ResultVal: val, Type: retType}, nil
		} else {
			return &Var{VType: TypeResult, ResultErr: reader.ReadString(), Type: retType}, nil
		}
	}

	return e.deserializeVar(reader, retType, route.Bridge)
}

func (e *Executor) serializeVar(buf *ffigo.Buffer, v *Var, typ ast.GoMiniType) error {
	if v == nil {
		// 写入零值，必须确保字节对齐
		switch {
		case typ == "String": buf.WriteString("")
		case typ.IsNumeric(): buf.WriteInt64(0)
		case typ == "Bool": buf.WriteBool(false)
		case typ.IsPtr() || typ == "TypeHandle": buf.WriteUint32(0)
		case typ == "Any": buf.WriteAny(nil)
		case typ.IsArray(): buf.WriteUint32(0)
		default:
			if name, ok := typ.StructName(); ok {
				if sDef, ok := e.program.Structs[name]; ok {
					for _, fName := range sDef.FieldNames {
						_ = e.serializeVar(buf, nil, sDef.Fields[fName])
					}
				}
			}
		}
		return nil
	}

	// 特殊处理 Any：必须按照 WriteAny 协议写入 Tag
	if typ == "Any" {
		switch v.VType {
		case TypeInt: buf.WriteAny(v.I64)
		case TypeFloat: buf.WriteAny(v.F64)
		case TypeString: buf.WriteAny(v.Str)
		case TypeBytes: buf.WriteAny(v.B)
		case TypeBool: buf.WriteAny(v.Bool)
		case TypeHandle: buf.WriteAny(v.Handle)
		default: buf.WriteAny(nil)
		}
		return nil
	}

	// 正常强类型序列化
	switch v.VType {
	case TypeInt:
		buf.WriteInt64(v.I64)
	case TypeFloat:
		buf.WriteFloat64(v.F64)
	case TypeString:
		buf.WriteString(v.Str)
	case TypeBool:
		buf.WriteBool(v.Bool)
	case TypeBytes:
		buf.WriteBytes(v.B)
	case TypeHandle:
		buf.WriteUint32(v.Handle)
	case TypeArray:
		arr := v.Ref.(*VMArray)
		buf.WriteUint32(uint32(len(arr.Data)))
		itemType, _ := typ.ReadArrayItemType()
		if itemType == "" { itemType = "Any" }
		for _, item := range arr.Data {
			if err := e.serializeVar(buf, item, itemType); err != nil { return err }
		}
	case TypeMap: // 结构体模拟
		if name, ok := typ.StructName(); ok {
			if sDef, ok := e.program.Structs[name]; ok {
				m := v.Ref.(*VMMap)
				for _, fName := range sDef.FieldNames {
					fType := sDef.Fields[fName]
					if err := e.serializeVar(buf, m.Data[string(fName)], fType); err != nil { return err }
				}
			}
		} else {
			// 真正的 Map 序列化暂不支持，目前仅用于结构体模拟
			return fmt.Errorf("Map serialization not implemented yet")
		}
	default:
		return fmt.Errorf("unsupported serialization type: %v", v.VType)
	}
	return nil
}

func (e *Executor) deserializeVar(reader *ffigo.Reader, typ ast.GoMiniType, bridge ffigo.FFIBridge) (*Var, error) {
	if typ.IsVoid() {
		return nil, nil
	}
	if reader.Available() == 0 {
		return nil, nil
	}

	switch {
	case typ == "String":
		return NewString(reader.ReadString()), nil
	case typ == "Int64" || typ == "Int" || typ == "Uint32":
		return NewInt(reader.ReadInt64()), nil
	case typ == "Float64":
		return NewFloat(reader.ReadFloat64()), nil
	case typ == "Bool":
		return NewBool(reader.ReadBool()), nil
	case typ == "TypeBytes" || strings.Contains(string(typ), "Array<Uint8>"):
		return &Var{VType: TypeBytes, B: reader.ReadBytes()}, nil
	case strings.HasPrefix(string(typ), "Ptr<") || typ == "TypeHandle":
		id := reader.ReadUint32()
		if id != 0 {
			e.activeHandles = append(e.activeHandles, handleRef{Bridge: bridge, ID: id})
		}
		return &Var{VType: TypeHandle, Handle: id, Bridge: bridge}, nil
	case typ.IsArray():
		// 处理从 FFI 返回的数组（如果以后支持）
		count := int(reader.ReadUint32())
		itemType, _ := typ.ReadArrayItemType()
		res := make([]*Var, count)
		for i := 0; i < count; i++ {
			val, err := e.deserializeVar(reader, itemType, bridge)
			if err != nil { return nil, err }
			res[i] = val
		}
		return &Var{VType: TypeArray, Ref: &VMArray{Data: res}, Type: typ}, nil
	default:
		return nil, fmt.Errorf("unsupported FFI return type: %s", typ)
	}
}

func writeAnyToBuffer(buf *ffigo.Buffer, arg *Var) {
	if arg == nil {
		buf.WriteAny(nil)
		return
	}
	switch arg.VType {
	case TypeInt:
		buf.WriteAny(arg.I64)
	case TypeFloat:
		buf.WriteAny(arg.F64)
	case TypeString:
		buf.WriteAny(arg.Str)
	case TypeBytes:
		buf.WriteAny(arg.B)
	case TypeBool:
		buf.WriteAny(arg.Bool)
	case TypeHandle:
		buf.WriteAny(arg.Handle)
	default:
		// 暂不支持将其它复杂类型作为 Any 写入
		buf.WriteAny(nil)
	}
}

func (e *Executor) GetProgram() *ast.ProgramStmt { return e.program }
