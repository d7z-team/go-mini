package runtime

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"weak"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/debugger"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type VarType byte

func (v VarType) String() string {
	switch v {
	case TypeInt:
		return "Int64"
	case TypeFloat:
		return "Float64"
	case TypeString:
		return "String"
	case TypeBytes:
		return "TypeBytes"
	case TypeBool:
		return "Bool"
	case TypeMap:
		return "Map"
	case TypeArray:
		return "Array"
	case TypeHandle:
		return "Handle"
	case TypeModule:
		return "Module"
	case TypeClosure:
		return "Closure"
	case TypeCell:
		return "Cell"
	case TypeAny:
		return "Any"
	case TypeInterface:
		return "Interface"
	case TypeError:
		return "Error"
	}
	return "Unknown"
}

// StackFrame represents a single frame in the virtual machine's stack trace.
type StackFrame struct {
	Filename string `json:"filename"`
	Function string `json:"function"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

// VMError is the unified error type for all go-mini runtime failures and panics.
type VMError struct {
	Message string          `json:"message"`
	Value   *Var            `json:"value,omitempty"` // Present if it's a panic(value)
	Frames  []StackFrame    `json:"frames"`
	IsPanic bool            `json:"is_panic"`
	Cause   error           `json:"-"` // Underlying Go error if any
	Handle  uint32          `json:"handle,omitempty"`
	Bridge  ffigo.FFIBridge `json:"-"`
}

func (e *VMError) Error() string {
	var sb strings.Builder
	if e.IsPanic {
		sb.WriteString("panic: ")
	}
	sb.WriteString(e.Message)
	if len(e.Frames) > 0 {
		sb.WriteString("\n\ngoroutine (mini) [running]:")
		for _, f := range e.Frames {
			// VSCode 终端匹配模式： path:line:col
			fmt.Fprintf(&sb, "\n%s()\n\t%s:%d:%d", f.Function, f.Filename, f.Line, f.Column)
		}
	}
	return sb.String()
}

func (e *VMError) Unwrap() error {
	return e.Cause
}

const (
	TypeInt   VarType = iota // Always int64
	TypeFloat                // Always float64
	TypeString
	TypeBytes // Raw buffer
	TypeBool
	TypeMap     // Internal VM Map (string keys only)
	TypeArray   // Internal VM Array ([]*Var)
	TypeHandle  // Host resource ID (uint32)
	TypeModule  // Dynamic module object
	TypeClosure // Anonymous function with captured environment
	TypeCell    // Boxed variable for closure capture
	TypeAny     // Placeholder for unknown/dynamic
	TypeInterface
	TypeError
)

type VMInterface struct {
	Target  *Var
	Methods map[string]*ast.FunctionType // Allowed methods with their signatures
}

type VMModule struct {
	Name    string
	Data    map[string]*Var
	Context *StackContext
}

type Cell struct {
	Value *Var
}

type VMClosure struct {
	FunctionType ast.FunctionType
	BodyTasks    []Task
	Upvalues     map[string]*Var // Captured environment variables (should be TypeCell)
	Context      *StackContext   // 闭包所属的母上下文
}

type VMMethodValue struct {
	Receiver *Var
	Method   string // Full FFI method name or internal function name
}

type Var struct {
	Type   ast.GoMiniType
	VType  VarType
	I64    int64
	F64    float64
	Str    string
	B      []byte
	Bool   bool
	Handle uint32
	Bridge ffigo.FFIBridge
	Ref    interface{} // Internal structures only: *VMArray, *VMMap, *VMHandle, *VMModule, *VMClosure, *Cell

	stack weak.Pointer[Stack]
}

// VMHandle wraps a handle ID and its bridge, providing automatic cleanup via finalizer.
type VMHandle struct {
	ID     uint32
	Bridge ffigo.FFIBridge
}

type cleanupArgs struct {
	ID     uint32
	Bridge ffigo.FFIBridge
}

func NewVMHandle(id uint32, bridge ffigo.FFIBridge) *VMHandle {
	if id == 0 {
		return nil
	}
	h := &VMHandle{ID: id, Bridge: bridge}
	runtime.AddCleanup(h, func(args cleanupArgs) {
		if args.ID != 0 && args.Bridge != nil {
			_ = args.Bridge.DestroyHandle(args.ID)
		}
	}, cleanupArgs{ID: id, Bridge: bridge})
	return h
}

type VMArray struct {
	Data []*Var
}

type VMMap struct {
	Data map[string]*Var
}

func (v *Var) ToInt() (int64, error) {
	if v == nil {
		return 0, errors.New("accessing nil variable")
	}
	if v.VType != TypeInt {
		return 0, fmt.Errorf("type mismatch: expected Int64, got %v", v.VType)
	}
	return v.I64, nil
}

func (v *Var) ToFloat() (float64, error) {
	if v == nil {
		return 0, errors.New("accessing nil variable")
	}
	if v.VType == TypeFloat {
		return v.F64, nil
	}
	if v.VType == TypeInt {
		return float64(v.I64), nil
	}
	return 0, fmt.Errorf("type mismatch: expected Numeric, got %v", v.VType)
}

func (v *Var) ToBool() (bool, error) {
	if v == nil {
		return false, errors.New("accessing nil variable")
	}
	if v.VType != TypeBool {
		return false, fmt.Errorf("type mismatch: expected Bool, got %v", v.VType)
	}
	return v.Bool, nil
}

func (v *Var) ToBytes() ([]byte, error) {
	if v == nil {
		return nil, errors.New("accessing nil variable")
	}
	if v.VType != TypeBytes {
		return nil, fmt.Errorf("type mismatch: expected TypeBytes, got %v", v.VType)
	}
	return v.B, nil
}

func (v *Var) ToHandle() (uint32, error) {
	if v == nil {
		return 0, errors.New("accessing nil variable")
	}
	if v.VType != TypeHandle {
		return 0, fmt.Errorf("type mismatch: expected TypeHandle, got %v", v.VType)
	}
	return v.Handle, nil
}

func (v *Var) ToError() (string, error) {
	if v == nil {
		return "", errors.New("accessing nil variable")
	}
	switch v.VType {
	case TypeError:
		if err, ok := v.Ref.(*VMError); ok {
			return err.Message, nil
		}
	case TypeString:
		return v.Str, nil
	case TypeAny:
		if v.Ref != nil {
			if ed, ok := v.Ref.(ffigo.ErrorData); ok {
				return ed.Message, nil
			}
			return fmt.Sprintf("%v", v.Interface()), nil
		}
	case TypeInt:
		return strconv.FormatInt(v.I64, 10), nil
	case TypeFloat:
		return strconv.FormatFloat(v.F64, 'f', -1, 64), nil
	case TypeBool:
		return strconv.FormatBool(v.Bool), nil
	}
	return "", fmt.Errorf("type mismatch: expected Error or String compatible type, got %v", v.VType)
}

func (v *Var) String() string {
	if v == nil {
		return "nil"
	}
	switch v.VType {
	case TypeInt:
		return strconv.FormatInt(v.I64, 10)
	case TypeFloat:
		return strconv.FormatFloat(v.F64, 'g', -1, 64)
	case TypeString:
		return fmt.Sprintf("\"%s\"", v.Str)
	case TypeBool:
		return strconv.FormatBool(v.Bool)
	case TypeBytes:
		return fmt.Sprintf("bytes(%d)", len(v.B))
	case TypeHandle:
		return fmt.Sprintf("handle(%d)", v.Handle)
	case TypeArray:
		if arr, ok := v.Ref.(*VMArray); ok {
			return fmt.Sprintf("array(%d)", len(arr.Data))
		}
	case TypeMap:
		if m, ok := v.Ref.(*VMMap); ok {
			return fmt.Sprintf("map(%d)", len(m.Data))
		}
	case TypeModule:
		if m, ok := v.Ref.(*VMModule); ok {
			return fmt.Sprintf("module(%s)", m.Name)
		}
	case TypeClosure:
		return "closure"
	case TypeInterface:
		return "interface"
	case TypeError:
		return "error"
	}
	return "unknown"
}

func (v *Var) Interface() interface{} {
	return v.interfaceWithDepth(0)
}

func (v *Var) interfaceWithDepth(depth int) interface{} {
	if v == nil || depth > 100 {
		return nil
	}
	switch v.VType {
	case TypeInt:
		return v.I64
	case TypeFloat:
		return v.F64
	case TypeString:
		return v.Str
	case TypeBytes:
		return v.B
	case TypeBool:
		return v.Bool
	case TypeHandle:
		return v.Handle
	case TypeArray:
		if arr, ok := v.Ref.(*VMArray); ok {
			res := make([]interface{}, len(arr.Data))
			for i, item := range arr.Data {
				res[i] = item.interfaceWithDepth(depth + 1)
			}
			return res
		}
	case TypeMap:
		if m, ok := v.Ref.(*VMMap); ok {
			res := make(map[string]interface{})
			for k, val := range m.Data {
				res[k] = val.interfaceWithDepth(depth + 1)
			}
			return res
		}
	case TypeError:
		if err, ok := v.Ref.(*VMError); ok {
			return err
		}
	}
	return nil
}

func (v *Var) Copy() *Var {
	if v == nil {
		return nil
	}
	res := &Var{
		Type:   v.Type,
		VType:  v.VType,
		I64:    v.I64,
		F64:    v.F64,
		Str:    v.Str,
		Bool:   v.Bool,
		Handle: v.Handle,
		Bridge: v.Bridge,
		Ref:    v.Ref, // 共享内部引用
	}
	if v.B != nil {
		res.B = make([]byte, len(v.B))
		copy(res.B, v.B)
	}
	// 如果是句柄类型，确保 Ref 始终持有 VMHandle 对象以维持生命周期
	if v.VType == TypeHandle && v.Handle != 0 && v.Ref == nil {
		// 容错：如果 Ref 丢失但 Handle 还在，重新构造一个受控的 VMHandle
		h := NewVMHandle(v.Handle, v.Bridge)
		res.Ref = h
	}
	if v.VType == TypeInterface {
		if inter, ok := v.Ref.(*VMInterface); ok {
			res.Ref = &VMInterface{
				Target:  inter.Target.Copy(),
				Methods: inter.Methods,
			}
		}
	}

	if v.stack.Value() != nil {
		res.stack = weak.Make(v.stack.Value())
	}
	return res
}

func NewVar(typ ast.GoMiniType, vType VarType) *Var {
	return &Var{
		Type:  typ,
		VType: vType,
	}
}

// 快速构造工厂方法，统一标量类型
func NewInt(v int64) *Var {
	res := NewVar("Int64", TypeInt)
	res.I64 = v
	return res
}

func NewFloat(v float64) *Var {
	res := NewVar("Float64", TypeFloat)
	res.F64 = v
	return res
}

func NewString(v string) *Var {
	res := NewVar("String", TypeString)
	res.Str = v
	return res
}

func NewBool(v bool) *Var {
	res := NewVar("Bool", TypeBool)
	res.Bool = v
	return res
}

func NewBytes(v []byte) *Var {
	res := NewVar("TypeBytes", TypeBytes)
	res.B = v
	return res
}

type Stack struct {
	Parent    *Stack
	MemoryPtr map[string]*Var
	Scope     string
	interrupt string
	Depth     int

	DeferStack []func()
}

func (s *Stack) AddDefer(fn func()) {
	s.DeferStack = append(s.DeferStack, fn)
}

func (s *Stack) RunDefers() {
	// 逆序执行 defer
	for i := len(s.DeferStack) - 1; i >= 0; i-- {
		s.DeferStack[i]()
	}
	s.DeferStack = nil
}

type ExecutorAPI interface {
	ExecExpr(ctx *StackContext, s ast.Expr) (*Var, error)
	CheckSatisfaction(val *Var, interfaceType ast.GoMiniType) (*Var, error)
	InvokeCallable(ctx *StackContext, callable *Var, methodName string, args []*Var) (*Var, error)
	ToVar(ctx *StackContext, val interface{}, bridge ffigo.FFIBridge) *Var
}

type StackContext struct {
	// Context is the host-provided context, strictly for FFI use.
	// VM kernel should check 'status' instead of Context.Err() for performance.
	Context context.Context
	Stack   *Stack

	// status represents the execution state (Fake Context)
	// 0: Running, 1: Aborted/Cancelled, 2: Paused
	status int32

	PanicVar     *Var         // 用于存储当前 goroutine/执行上下文中正在冒泡的 panic 对象
	PanicMessage string       // 存储发生 panic 时的文本消息
	PanicTrace   []StackFrame // 存储发生 panic 时的原始堆栈信息，避免 unwind 期间 TaskStack 被清空导致丢失
	Executor     ExecutorAPI

	// 运行时状态 (Session State)

	StepCount      int64
	StepLimit      int64
	ModuleCache    map[string]*Var
	LoadingModules map[string]bool

	Debugger *debugger.Session

	// 迭代执行器状态 (Iterative Executor State)
	TaskStack  []Task
	ValueStack *ValueStack
	UnwindMode UnwindMode

	// resumeSignal is used to unblock the execution loop after a pause.
	resumeSignal chan struct{}
}

func (ctx *StackContext) Abort() {
	atomic.StoreInt32(&ctx.status, 1)
}

func (ctx *StackContext) Aborted() bool {
	return atomic.LoadInt32(&ctx.status) == 1
}

func (ctx *StackContext) Pause() {
	atomic.CompareAndSwapInt32(&ctx.status, 0, 2)
}

func (ctx *StackContext) Resume() {
	if atomic.CompareAndSwapInt32(&ctx.status, 2, 0) {
		select {
		case ctx.resumeSignal <- struct{}{}:
		default:
		}
	}
}

func (ctx *StackContext) IsPaused() bool {
	return atomic.LoadInt32(&ctx.status) == 2
}

func (ctx *StackContext) Done() <-chan struct{} {
	if ctx.Context != nil {
		return ctx.Context.Done()
	}
	return nil
}

func (ctx *StackContext) Err() error {
	if ctx.Aborted() {
		if ctx.Context != nil {
			return ctx.Context.Err()
		}
		return context.Canceled
	}
	return nil
}

const (
	DefaultMaxStackDepth = 50000
)

func (s *Stack) DumpVariables() map[string]string {
	result := make(map[string]string)
	curr := s
	for curr != nil {
		for name, variable := range curr.MemoryPtr {
			if _, exists := result[name]; !exists {
				result[name] = fmt.Sprintf("%v", variable.Interface())
			}
		}
		curr = curr.Parent
	}
	return result
}

func (ctx *StackContext) ScopeApply(scope string) {
	newDepth := 1
	if ctx.Stack != nil {
		newDepth = ctx.Stack.Depth + 1
	}
	if newDepth > DefaultMaxStackDepth {
		panic(errors.New("stack overflow"))
	}
	ctx.Stack = &Stack{
		Parent:    ctx.Stack,
		MemoryPtr: make(map[string]*Var),
		Scope:     scope,
		Depth:     newDepth,
	}
}

func (ctx *StackContext) WithScope(sType string, child func(ctx *StackContext)) {
	ctx.ScopeApply(sType)
	defer ctx.ScopeExit()
	child(ctx)
}

func (ctx *StackContext) ScopeExit() {
	ctx.Stack = ctx.Stack.Parent
}

func (ctx *StackContext) Store(variable string, expr *Var) error {
	v, err := ctx.loadVar(variable)
	if err != nil {
		return ctx.AddVariable(variable, expr)
	}
	if v != nil && v.VType == TypeCell {
		v = v.Ref.(*Cell).Value
	}
	if expr == nil {
		if v != nil {
			v.VType = TypeAny
			v.I64 = 0
			v.F64 = 0
			v.Str = ""
			v.B = nil
			v.Bool = false
			v.Handle = 0
			v.Ref = nil
		} else {
			return ctx.AddVariable(variable, nil)
		}
		return nil
	}

	if v == nil {
		return ctx.AddVariable(variable, expr)
	}

	if v.Type == "Any" && expr.Type != "Any" {
		v.Type = expr.Type
	}

	if v.Type.IsInterface() && !expr.Type.IsInterface() {
		// Perform satisfaction check and wrapping
		wrapped, err := ctx.Executor.CheckSatisfaction(expr, v.Type)
		if err != nil {
			return err
		}
		expr = wrapped
	}

	v.VType = expr.VType
	v.Type = expr.Type
	v.I64 = expr.I64
	v.F64 = expr.F64
	v.Str = expr.Str
	v.B = expr.B
	v.Bool = expr.Bool
	v.Handle = expr.Handle
	v.Bridge = expr.Bridge
	v.Ref = expr.Ref
	return nil
}

func (ctx *StackContext) AddVariable(name string, v *Var) error {
	ctx.Stack.MemoryPtr[name] = v.Copy()
	return nil
}

func (ctx *StackContext) Load(name string) (*Var, error) {
	v, err := ctx.loadVar(name)
	if err != nil {
		return nil, err
	}
	if v != nil && v.VType == TypeCell {
		val := v.Ref.(*Cell).Value
		return val, nil
	}
	return v, nil
}

func (ctx *StackContext) loadVar(variable string) (*Var, error) {
	s := ctx.Stack
	for s != nil {
		if v, ok := s.MemoryPtr[variable]; ok {
			return v, nil
		}
		s = s.Parent
	}
	return nil, fmt.Errorf("undefined: %s", variable)
}

func (ctx *StackContext) CaptureVar(name string) (*Var, error) {
	s := ctx.Stack
	for s != nil {
		if v, ok := s.MemoryPtr[name]; ok {
			if v != nil && v.VType != TypeCell {
				cellValue := v.Copy()
				v.VType = TypeCell
				v.Ref = &Cell{Value: cellValue}
				v.I64, v.F64, v.Str, v.B, v.Bool, v.Handle, v.Bridge = 0, 0, "", nil, false, 0, nil
			}
			return v, nil
		}
		s = s.Parent
	}
	return nil, fmt.Errorf("undefined capture: %s", name)
}

func (ctx *StackContext) Interrupt() bool {
	return ctx.Stack != nil && ctx.Stack.interrupt != ""
}

func (ctx *StackContext) SetInterrupt(scopeName, interruptType string) error {
	s := ctx.Stack
	for s != nil {
		s.interrupt = interruptType
		if s.Scope == scopeName {
			return nil
		}
		s = s.Parent
	}
	return fmt.Errorf("scope %s not found", scopeName)
}

func (ctx *StackContext) NewVar(name string, kind ast.GoMiniType) error {
	if _, ok := ctx.Stack.MemoryPtr[name]; ok {
		return nil
	}
	// 确保变量被正确初始化为零值
	var v *Var
	if exec, ok := ctx.Executor.(*Executor); ok {
		v = exec.initializeType(ctx, kind, 0)
	} else {
		v = &Var{Type: kind, VType: TypeAny}
	}
	ctx.Stack.MemoryPtr[name] = v
	return nil
}

func (ctx *StackContext) WithFuncScope(name string, exec func(*Stack, *StackContext) error) error {
	old := ctx.Stack
	root := old
	for root != nil && root.Parent != nil {
		root = root.Parent
	}
	ctx.Stack = root
	ctx.ScopeApply(name)
	defer func() { ctx.Stack = old }()
	return exec(old, ctx)
}

func copyVarData(dest, src *Var) {
	// CRITICAL: Type metadata is shared and can be modified by the script.
	// FFI bridges MUST perform strict VType and content assertions instead of trusting Type.
	dest.Type = src.Type
	dest.VType = src.VType
	dest.I64 = src.I64
	dest.F64 = src.F64
	dest.Str = src.Str
	dest.B = src.B
	dest.Bool = src.Bool
	dest.Handle = src.Handle
	dest.Bridge = src.Bridge
	dest.Ref = src.Ref
}

func (ctx *StackContext) GenerateStackTrace(current *Task) []StackFrame {
	var frames []StackFrame

	// 1. Add current frame
	if current != nil && current.Source != nil {
		funcName := "main"
		if ctx.Stack != nil && ctx.Stack.Scope != "" {
			funcName = ctx.Stack.Scope
		}
		frames = append(frames, StackFrame{
			Filename: current.Source.File,
			Function: funcName,
			Line:     current.Source.Line,
			Column:   current.Source.Col,
		})
	}

	// 2. Reconstruct previous frames from TaskStack
	for i := len(ctx.TaskStack) - 1; i >= 0; i-- {
		task := ctx.TaskStack[i]
		if task.Op == OpCallBoundary && task.Source != nil {
			callerName := "main"
			for j := i - 1; j >= 0; j-- {
				if ctx.TaskStack[j].Op == OpCallBoundary {
					if d2, ok := ctx.TaskStack[j].Data.(map[string]interface{}); ok {
						if name, ok := d2["name"].(string); ok && name != "" {
							callerName = name
						}
						break
					}
				}
			}
			frames = append(frames, StackFrame{
				Filename: task.Source.File,
				Function: callerName,
				Line:     task.Source.Line,
				Column:   task.Source.Col,
			})
		}
		if len(frames) > 20 {
			break
		}
	}

	return frames
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
	case TypeHandle:
		return v.Handle == 0
	case TypeAny:
		return v.Ref == nil
	}
	return false
}

type Program struct{}
