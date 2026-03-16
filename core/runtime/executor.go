package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strconv"

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
	}
	return nil, fmt.Errorf("todo: expr %T", s)
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
	return nil, fmt.Errorf("todo: composite %s", n.Type)
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
	if l == nil || r == nil { return nil, errors.New("binary op with nil operand") }

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

	// 序列化参数
	for _, arg := range args {
		if arg == nil {
			buf.WriteUint32(0) // Null marker
			continue
		}
		switch arg.VType {
		case TypeInt:
			buf.WriteInt64(arg.I64)
		case TypeFloat:
			buf.WriteFloat64(arg.F64)
		case TypeString:
			buf.WriteString(arg.Str)
		case TypeBytes:
			buf.WriteBytes(arg.B)
		case TypeBool:
			buf.WriteBool(arg.Bool)
		case TypeHandle:
			buf.WriteUint32(arg.Handle)
		case TypeArray:
			arr := arg.Ref.(*VMArray)
			buf.WriteUint32(uint32(len(arr.Data)))
			for _, item := range arr.Data {
				writeAnyToBuffer(buf, item)
			}
		default:
			return nil, fmt.Errorf("FFI unsupported arg type: %v", arg.VType)
		}
	}

	// 呼叫 Bridge
	retData, err := route.Bridge.Call(route.MethodID, buf.Bytes())
	if err != nil {
		return nil, err
	}

	// 解析返回值 (目前简化为支持单返回值或 Void)
	if len(retData) == 0 {
		return nil, nil
	}

	reader := ffigo.NewReader(retData)

	switch route.Returns {
	case "TypeHandle":
		id := reader.ReadUint32()
		e.activeHandles = append(e.activeHandles, handleRef{Bridge: route.Bridge, ID: id})
		return &Var{VType: TypeHandle, Handle: id, Bridge: route.Bridge}, nil
	case "TypeBytes":
		return &Var{VType: TypeBytes, B: reader.ReadBytes()}, nil
	case "String":
		return NewString(reader.ReadString()), nil
	case "Int64":
		return NewInt(reader.ReadInt64()), nil
	case "Bool":
		return &Var{VType: TypeBool, Bool: reader.ReadBool()}, nil
	default:
		// 临时方案：如果 retData 长度为 8，假设是 Int64
		if len(retData) == 8 {
			return NewInt(reader.ReadInt64()), nil
		}
		// 如果是字符串 (长度 uint32 + 字节)
		return NewString(string(retData)), nil
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
	default:
		// 暂不支持将 Handle 或其它复杂类型作为 Any 写入
		buf.WriteAny(nil)
	}
}

func (e *Executor) GetProgram() *ast.ProgramStmt { return e.program }
