package runtime

import (
	"context"
	"fmt"
	"runtime"
	"weak"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type VarType byte

const (
	TypeInt   VarType = iota // Always int64
	TypeFloat                // Always float64
	TypeString
	TypeBytes // Raw buffer
	TypeBool
	TypeMap    // Internal VM Map (string keys only)
	TypeArray  // Internal VM Array ([]*Var)
	TypeHandle // Host resource ID (uint32)
	TypeResult // Standard result type (val, err)
	TypeAny    // Placeholder for unknown/dynamic
)

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
	Ref    interface{} // Internal structures only: *VMArray, *VMMap, *VMHandle

	// Result fields
	ResultVal *Var
	ResultErr string

	stack weak.Pointer[Stack]
}

// VMHandle wraps a handle ID and its bridge, providing automatic cleanup via finalizer.
type VMHandle struct {
	ID     uint32
	Bridge ffigo.FFIBridge
}

func (h *VMHandle) release() {
	if h.ID != 0 && h.Bridge != nil {
		_ = h.Bridge.DestroyHandle(h.ID)
		h.ID = 0
	}
}

func NewVMHandle(id uint32, bridge ffigo.FFIBridge) *VMHandle {
	if id == 0 {
		return nil
	}
	h := &VMHandle{ID: id, Bridge: bridge}
	runtime.SetFinalizer(h, func(obj *VMHandle) {
		obj.release()
	})
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
		return 0, fmt.Errorf("accessing nil variable")
	}
	if v.VType != TypeInt {
		return 0, fmt.Errorf("type mismatch: expected Int64, got %v", v.VType)
	}
	return v.I64, nil
}

func (v *Var) ToFloat() (float64, error) {
	if v == nil {
		return 0, fmt.Errorf("accessing nil variable")
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
		return false, fmt.Errorf("accessing nil variable")
	}
	if v.VType != TypeBool {
		return false, fmt.Errorf("type mismatch: expected Bool, got %v", v.VType)
	}
	return v.Bool, nil
}

func (v *Var) ToBytes() ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("accessing nil variable")
	}
	if v.VType != TypeBytes {
		return nil, fmt.Errorf("type mismatch: expected TypeBytes, got %v", v.VType)
	}
	return v.B, nil
}

func (v *Var) ToHandle() (uint32, error) {
	if v == nil {
		return 0, fmt.Errorf("accessing nil variable")
	}
	if v.VType != TypeHandle {
		return 0, fmt.Errorf("type mismatch: expected TypeHandle, got %v", v.VType)
	}
	return v.Handle, nil
}

func cloneVar(v *Var) *Var {
	if v == nil {
		return nil
	}
	res := &Var{
		Type:      v.Type,
		VType:     v.VType,
		I64:       v.I64,
		F64:       v.F64,
		Str:       v.Str,
		Bool:      v.Bool,
		Handle:    v.Handle,
		Bridge:    v.Bridge,
		Ref:       v.Ref, // Reference structures are shared by pointer
		ResultVal: cloneVar(v.ResultVal),
		ResultErr: v.ResultErr,
	}
	if v.B != nil {
		res.B = make([]byte, len(v.B))
		copy(res.B, v.B)
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
	res := NewVar("[]byte", TypeBytes)
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

type StackContext struct {
	context.Context
	Program  *Program
	Stack    *Stack
	Executor interface {
		ExecExpr(ctx *StackContext, s ast.Expr) (*Var, error)
	}
}

const (
	ContextKeyMaxStackDepth = "ContextKeyMaxStackDepth"
	DefaultMaxStackDepth    = 50000
)

func (c *StackContext) ScopeApply(scope string) {
	newDepth := 1
	if c.Stack != nil {
		newDepth = c.Stack.Depth + 1
	}
	if newDepth > DefaultMaxStackDepth {
		panic(fmt.Errorf("stack overflow"))
	}
	c.Stack = &Stack{
		Parent:    c.Stack,
		MemoryPtr: make(map[string]*Var),
		Scope:     scope,
		Depth:     newDepth,
	}
}

func (c *StackContext) WithScope(sType string, child func(ctx *StackContext)) {
	c.ScopeApply(sType)
	defer c.ScopeExit()
	child(c)
}

func (c *StackContext) ScopeExit() {
	c.Stack = c.Stack.Parent
}

func (c *StackContext) Store(variable string, expr *Var) error {
	v, err := c.loadVar(variable)
	if err != nil {
		return c.AddVariable(variable, expr)
	}
	if expr == nil {
		v.VType = TypeAny
		v.I64 = 0
		v.F64 = 0
		v.Str = ""
		v.B = nil
		v.Bool = false
		v.Handle = 0
		v.Ref = nil
		v.ResultVal = nil
		v.ResultErr = ""
		return nil
	}
	// Copy data only, keep original metadata if strictly typed
	v.VType = expr.VType
	v.I64 = expr.I64
	v.F64 = expr.F64
	v.Str = expr.Str
	v.B = expr.B
	v.Bool = expr.Bool
	v.Handle = expr.Handle
	v.Bridge = expr.Bridge
	v.Ref = expr.Ref
	v.ResultVal = expr.ResultVal
	v.ResultErr = expr.ResultErr
	return nil
}

func (c *StackContext) AddVariable(name string, v *Var) error {
	c.Stack.MemoryPtr[name] = cloneVar(v)
	return nil
}

func (c *StackContext) Load(name string) (*Var, error) {
	return c.loadVar(name)
}

func (c *StackContext) loadVar(variable string) (*Var, error) {
	s := c.Stack
	for s != nil {
		if v, ok := s.MemoryPtr[variable]; ok {
			return v, nil
		}
		s = s.Parent
	}
	return nil, fmt.Errorf("undefined: %s", variable)
}

func (c *StackContext) Interrupt() bool {
	return c.Stack != nil && c.Stack.interrupt != ""
}

func (c *StackContext) SetInterrupt(scopeName, interruptType string) error {
	s := c.Stack
	for s != nil {
		s.interrupt = interruptType
		if s.Scope == scopeName {
			return nil
		}
		s = s.Parent
	}
	return fmt.Errorf("scope %s not found", scopeName)
}

func (c *StackContext) NewVar(name string, kind ast.GoMiniType) error {
	c.Stack.MemoryPtr[name] = &Var{Type: kind}
	return nil
}

func (c *StackContext) WithFuncScope(name string, exec func(*Stack, *StackContext) error) error {
	old := c.Stack
	root := old
	for root != nil && root.Parent != nil {
		root = root.Parent
	}
	c.Stack = root
	c.ScopeApply(name)
	defer func() { c.Stack = old }()
	return exec(old, c)
}

type Program struct{}
