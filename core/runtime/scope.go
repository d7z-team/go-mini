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
	Type   ast.GoMiniType
	GoType reflect.Type
	Data   any
	Value  reflect.Value // 变量的真实存储槽位 (可寻址)

	stack weak.Pointer[Stack]
}

func (v *Var) IsPtr() bool {
	return v.Type.IsPtr()
}

func cloneValue(typ ast.GoMiniType, data any) any {
	if data == nil {
		return nil
	}

	// 引用类型不进行克隆
	if typ.IsPtr() || typ.IsArray() || typ.IsMap() || typ.IsAny() || typ.IsTuple() {
		return data
	}

	// 原生类型的克隆（如果实现了 MiniClone）
	if cloner, ok := data.(ast.MiniClone); ok {
		return cloner.Clone()
	}

	// 自定义结构体的深拷贝
	if ds, ok := data.(DynStruct); ok {
		newBody := make(map[string]any)
		for k, v := range ds.Body {
			var fieldTyp ast.GoMiniType
			if ds.Define != nil {
				fieldTyp = ds.Define.Fields[ast.Ident(k)]
			}
			newBody[k] = cloneValue(fieldTyp, v)
		}
		return DynStruct{
			Define: ds.Define,
			Body:   newBody,
		}
	}

	return data
}

func NewVar(typ ast.GoMiniType, goType reflect.Type, data any, stack *Stack) *Var {
	data = cloneValue(typ, data)

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

func NewVarWithValue(typ ast.GoMiniType, val reflect.Value, stack *Stack) *Var {
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
	Depth     int
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

	newDepth := 1
	if c.Stack != nil {
		newDepth = c.Stack.Depth + 1
	}

	maxDepth := DefaultMaxStackDepth
	if c.Context != nil {
		if val := c.Context.Value(ContextKeyMaxStackDepth); val != nil {
			if d, ok := val.(int); ok {
				maxDepth = d
			}
		}
	}

	if newDepth > maxDepth {
		panic(fmt.Errorf("stack overflow: max stack depth %d reached", maxDepth))
	}

	c.Stack = &Stack{
		Parent:    c.Stack,
		MemoryPtr: make(map[string]*Var),
		Scope:     scope,
		Deferred:  make([]ast.Expr, 0),
		Depth:     newDepth,
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
		// 如果变量未找到，则在当前作用域创建它 (对应 := 语义)
		// 注册新变量，将其存入 runtime MemoryPtr
		return c.AddVariable(variable, expr)
	}

	if !varPtr.Type.IsAny() && !varPtr.Type.Equals(expr.Type) {
		return fmt.Errorf("variable type mismatch: var(%s) != expr(%s)", varPtr.Type, expr.Type)
	}

	clonedData := cloneValue(varPtr.Type, expr.Data)
	val := reflect.ValueOf(clonedData)

	if !varPtr.Value.IsValid() {
		// 延迟初始化容器类型
		if val.IsValid() {
			ptr := reflect.New(val.Type())
			varPtr.Value = ptr.Elem()
		}
	}

	if val.IsValid() {
		if val.Type().AssignableTo(varPtr.Value.Type()) {
			varPtr.Value.Set(val)
		} else if varPtr.Value.Kind() == reflect.Interface {
			varPtr.Value.Set(val)
		}
		varPtr.GoType = val.Type()
	}
	varPtr.Data = clonedData
	return nil
}

func (c *StackContext) AddVariable(name string, v *Var) error {
	if c.Stack == nil {
		return errors.New("stack is nil")
	}
	// 确保变量进入 MemoryPtr
	c.Stack.MemoryPtr[name] = NewVar(v.Type, v.GoType, v.Data, c.Stack)
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

func (c *StackContext) NewVar(s string, kind ast.GoMiniType) error {
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
