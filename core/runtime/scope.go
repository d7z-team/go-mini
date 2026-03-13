package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"weak"

	"gopkg.d7z.net/go-mini/core/ast"
)

type Var struct {
	Type   ast.OPSType
	GoType reflect.Type
	Data   any
	Value  reflect.Value // 变量的真实存储槽位 (可寻址)

	stack weak.Pointer[Stack]
}

func (v *Var) IsPtr() bool {
	return v.Type.IsPtr()
}

func NewVar(typ ast.OPSType, goType reflect.Type, data any, stack *Stack) *Var {
	if goType == nil && data != nil {
		if rv, ok := data.(reflect.Value); ok {
			goType = rv.Type()
		} else {
			goType = reflect.TypeOf(data)
		}
	}

	res := &Var{
		Type:   typ,
		GoType: goType,
		Data:   data,
	}

	if goType != nil {
		// 创建一个该类型的可寻址副本
		ptr := reflect.New(goType)
		val := ptr.Elem()
		if data != nil {
			rv := reflect.ValueOf(data)
			if r, ok := data.(reflect.Value); ok {
				rv = r
			}

			if rv.Type().AssignableTo(val.Type()) {
				val.Set(rv)
			} else if rv.Kind() == reflect.Ptr && rv.Elem().Type().AssignableTo(val.Type()) {
				val.Set(rv.Elem())
			} else if rv.Type().ConvertibleTo(val.Type()) {
				val.Set(rv.Convert(val.Type()))
			} else {
				// Fallback if possible or error?
				// For now, try to set the value as is (might still panic if totally incompatible)
				val.Set(rv)
			}
		}
		res.Value = val
		res.Data = val.Interface()
	}

	if stack != nil {
		res.stack = weak.Make(stack)
	}
	return res
}

func NewVarWithValue(typ ast.OPSType, val reflect.Value, stack *Stack) *Var {
	v := &Var{
		Type:   typ,
		GoType: val.Type(),
		Data:   nil,
		Value:  val,
	}
	if val.IsValid() {
		v.Data = val.Interface()
	}
	if stack != nil {
		v.stack = weak.Make(stack)
	}
	return v
}

type Stack struct {
	Parent    *Stack
	MemoryPtr map[string]*Var // 内存地址
	Scope     string          // 作用域类型
	interrupt string
	Deferred  []ast.Expr
}

// IsChildOf other 是否为当前 stack 的父作用域
func (s *Stack) IsChildOf(other *Stack) bool {
	if s == other || other == nil {
		return false
	}
	parent := s.Parent
	for parent != nil {
		if parent == other {
			return true
		}
		parent = parent.Parent
	}
	return false
}

type (
	Program      struct{}
	StackContext struct {
		context.Context
		Program  *Program
		Stack    *Stack
		Executor interface {
			ExecExpr(ctx *StackContext, s ast.Expr) (*Var, error)
		}
	}
)

func (c *StackContext) ScopeApply(scope string) {
	if scope == "" {
		panic("empty scope")
	}
	c.Stack = &Stack{
		Parent:    c.Stack,
		MemoryPtr: make(map[string]*Var),
		Scope:     scope,
		Deferred:  make([]ast.Expr, 0),
	}
}

func (c *StackContext) WithScope(sType string, child func(ctx *StackContext)) {
	c.ScopeApply(sType)
	defer c.ScopeExit()
	child(c)
}

func (c *StackContext) ScopeExit() {
	if c.Stack == nil || c.Stack.Parent == nil {
		panic("stack is empty")
	}
	c.Stack = c.Stack.Parent
}

func (c *StackContext) ExecuteDeferred() {
	if c.Stack == nil {
		return
	}
	// LIFO 执行
	for i := len(c.Stack.Deferred) - 1; i >= 0; i-- {
		expr := c.Stack.Deferred[i]
		_, _ = c.Executor.ExecExpr(c, expr)
	}
	c.Stack.Deferred = nil
}

// Store 变量替换
func (c *StackContext) Store(variable string, expr *Var) error {
	if c.Stack == nil {
		return errors.New("stack is nil")
	}
	varPtr, err := c.loadVar(variable)
	if err != nil {
		return err
	}

	if !varPtr.Type.IsAny() && !varPtr.Type.Equals(expr.Type) {
		return fmt.Errorf("variable type mismatch: var(%s) != expr(%s)", varPtr.Type, expr.Type)
	}

	val := reflect.ValueOf(expr.Data)
	if !varPtr.Value.IsValid() {
		// 延迟初始化容器类型
		ptr := reflect.New(val.Type())
		varPtr.Value = ptr.Elem()
	}

	if val.Type().AssignableTo(varPtr.Value.Type()) {
		varPtr.Value.Set(val)
	} else if varPtr.Value.Kind() == reflect.Interface {
		varPtr.Value.Set(val)
	}

	varPtr.GoType = val.Type()
	varPtr.Data = expr.Data
	return nil
}

func (c *StackContext) Load(name string) (*Var, error) {
	return c.loadVar(name)
}

func (c *StackContext) LoadAddr(name string) (reflect.Value, error) {
	v, err := c.loadVar(name)
	if err != nil {
		return reflect.Value{}, err
	}
	if v.Value.CanAddr() {
		return v.Value.Addr(), nil
	}
	return reflect.Value{}, fmt.Errorf("variable %s is not addressable", name)
}

func (c *StackContext) loadVar(variable string) (*Var, error) {
	stack := c.Stack
	for {
		if stack == nil {
			return nil, fmt.Errorf("var not found: %s", variable)
		}
		if v, ok := stack.MemoryPtr[variable]; ok {
			return v, nil
		}
		stack = stack.Parent
	}
}

func (c *StackContext) Interrupt() bool {
	if c.Stack == nil {
		return false
	}
	return c.Stack.interrupt != ""
}

func (c *StackContext) SetInterrupt(scopeName, interruptType string) error {
	s := c.Stack
	if s == nil {
		return errors.New("stack is nil")
	}
	var stacks []*Stack
	for {
		if s == nil {
			return errors.New("stack not found :" + scopeName)
		}
		stacks = append(stacks, s)
		if s.Scope == scopeName {
			break
		}
		s = s.Parent
	}
	for _, stack := range stacks {
		stack.interrupt = interruptType
	}
	return nil
}

func (c *StackContext) NewVar(s string, kind ast.OPSType) error {
	if c.Stack == nil {
		return errors.New("stack is nil")
	}
	if c.Stack.MemoryPtr[s] != nil {
		return errors.New("variable already exists :" + s)
	}

	c.Stack.MemoryPtr[s] = &Var{
		Type: kind,
	}
	return nil
}

func (c *StackContext) WithFuncScope(name string, exec func(*Stack, *StackContext) error) error {
	if c.Stack == nil {
		return errors.New("stack is nil")
	}
	old := c.Stack
	defer func() {
		c.Stack = old
	}()
	root := c.Stack
	for root.Parent != nil {
		root = root.Parent
	}
	c.Stack = root
	c.ScopeApply(name)
	defer c.ExecuteDeferred()
	defer c.ScopeExit()
	return exec(old, c)
}
