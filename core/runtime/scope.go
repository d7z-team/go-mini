package runtime

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
	Target *Var
	Spec   *RuntimeInterfaceSpec
	VTable []*Var
}

type SharedState struct {
	mu             sync.RWMutex
	globals        map[string]*Var
	moduleCache    map[string]*Var
	loadingModules map[string]bool
	initialized    bool
}

type SharedStateSnapshot struct {
	initialized    bool
	globals        map[string]*Var
	moduleCache    map[string]*Var
	loadingModules map[string]bool
}

func (s *SharedStateSnapshot) IsInitialized() bool {
	if s == nil {
		return false
	}
	return s.initialized
}

func (s *SharedStateSnapshot) LoadGlobal(name string) (*Var, bool) {
	if s == nil {
		return nil, false
	}
	v, ok := s.globals[name]
	return v, ok
}

func (s *SharedStateSnapshot) HasGlobal(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.globals[name]
	return ok
}

func (s *SharedStateSnapshot) HasModule(path string) bool {
	if s == nil {
		return false
	}
	_, ok := s.moduleCache[path]
	return ok
}

func (s *SharedStateSnapshot) Module(path string) (*Var, bool) {
	if s == nil {
		return nil, false
	}
	v, ok := s.moduleCache[path]
	return v, ok
}

func (s *SharedStateSnapshot) IsModuleLoading(path string) bool {
	if s == nil {
		return false
	}
	return s.loadingModules[path]
}

func (s *SharedStateSnapshot) Globals() map[string]*Var {
	if s == nil {
		return nil
	}
	out := make(map[string]*Var, len(s.globals))
	for k, v := range s.globals {
		out[k] = v
	}
	return out
}

func (s *SharedStateSnapshot) ModuleCache() map[string]*Var {
	if s == nil {
		return nil
	}
	out := make(map[string]*Var, len(s.moduleCache))
	for k, v := range s.moduleCache {
		out[k] = v
	}
	return out
}

func (s *SharedStateSnapshot) LoadingModules() map[string]bool {
	if s == nil {
		return nil
	}
	out := make(map[string]bool, len(s.loadingModules))
	for k, v := range s.loadingModules {
		out[k] = v
	}
	return out
}

func NewSharedState() *SharedState {
	return &SharedState{
		globals:        make(map[string]*Var),
		moduleCache:    make(map[string]*Var),
		loadingModules: make(map[string]bool),
	}
}

type LexicalContext struct {
	Executor *Executor
	Shared   *SharedState
	Stack    *Stack
}

type VMModule struct {
	Name    string
	Data    map[string]*Var
	Context *LexicalContext
}

type Cell struct {
	Value *Var
}

type VMClosure struct {
	FunctionSig  *RuntimeFuncSig
	BodyTasks    []Task
	UpvalueSlots []*Var
	UpvalueNames []string
	Context      *LexicalContext // 闭包所属的词法上下文
}

type VMMethodValue struct {
	Receiver *Var
	Method   string // Full FFI method name or internal function name
}

type Var struct {
	TypeInfo RuntimeType
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

func (v *Var) RuntimeType() RuntimeType {
	if v == nil {
		return RuntimeType{}
	}
	return v.TypeInfo
}

func (v *Var) RawType() ast.GoMiniType {
	if v == nil {
		return ""
	}
	return v.TypeInfo.Raw
}

func (v *Var) SetRuntimeType(typ RuntimeType) {
	if v == nil {
		return
	}
	v.TypeInfo = typ
}

func (v *Var) SetRawType(typ ast.GoMiniType) {
	if v == nil {
		return
	}
	if typ.IsEmpty() {
		v.TypeInfo = RuntimeType{}
		return
	}
	parsed, err := ParseRuntimeType(typ)
	if err == nil {
		v.TypeInfo = parsed
		return
	}
	v.TypeInfo = RuntimeType{Raw: typ}
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
		TypeInfo: v.TypeInfo,
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
				Target: inter.Target.Copy(),
				Spec:   inter.Spec,
			}
		}
	}

	if v.stack.Value() != nil {
		res.stack = weak.Make(v.stack.Value())
	}
	return res
}

func NewVar(typ ast.GoMiniType, vType VarType) *Var {
	res := &Var{VType: vType}
	res.SetRawType(typ)
	return res
}

func NewVarWithRuntimeType(typ RuntimeType, vType VarType) *Var {
	res := &Var{VType: vType}
	res.SetRuntimeType(typ)
	return res
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

type SlotFrame struct {
	Locals       []*Var
	LocalNames   []string
	LocalIndex   map[string]int
	Upvalues     []*Var
	UpvalueNames []string
	UpvalueIndex map[string]int
	Return       *Var
	ReturnName   string
}

type Stack struct {
	Parent    *Stack
	MemoryPtr map[string]*Var
	Frame     *SlotFrame
	FrameBase *SlotFrame
	FrameSync int
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
	Executor     *Executor
	Shared       *SharedState

	// 运行时状态 (Session State)

	StepCount int64
	StepLimit int64

	Debugger *debugger.Session

	// 迭代执行器状态 (Iterative Executor State)
	TaskStack  []Task
	ValueStack *ValueStack
	LHSStack   *LHSStack
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

func (s *SharedState) IsInitialized() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.initialized
}

func (s *SharedState) MarkInitialized() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initialized = true
}

func (s *SharedState) Snapshot() *SharedStateSnapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	globals := make(map[string]*Var, len(s.globals))
	for k, v := range s.globals {
		globals[k] = v
	}
	moduleCache := make(map[string]*Var, len(s.moduleCache))
	for k, v := range s.moduleCache {
		moduleCache[k] = v
	}
	loadingModules := make(map[string]bool, len(s.loadingModules))
	for k, v := range s.loadingModules {
		loadingModules[k] = v
	}
	return &SharedStateSnapshot{
		initialized:    s.initialized,
		globals:        globals,
		moduleCache:    moduleCache,
		loadingModules: loadingModules,
	}
}

func (s *SharedState) LoadGlobal(name string) (*Var, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.globals[name]
	return v, ok
}

func (s *SharedState) HasGlobal(name string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.globals[name]
	return ok
}

func (s *SharedState) StoreGlobal(name string, v *Var) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v == nil {
		s.globals[name] = NewVarWithRuntimeType(MustParseRuntimeType("Any"), TypeAny)
		return
	}
	s.globals[name] = v
}

func (s *SharedState) ApplyEnv(env map[string]*Var) {
	if s == nil || len(env) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range env {
		s.globals[k] = v.Copy()
	}
}

func (s *SharedState) Module(path string) (*Var, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.moduleCache[path]
	return v, ok
}

func (s *SharedState) HasModule(path string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.moduleCache[path]
	return ok
}

func (s *SharedState) StoreModule(path string, v *Var) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.moduleCache[path] = v
}

func (s *SharedState) DeleteModule(path string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.moduleCache, path)
}

func (s *SharedState) IsModuleLoading(path string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadingModules[path]
}

func (s *SharedState) SetModuleLoading(path string, loading bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if loading {
		s.loadingModules[path] = true
		return
	}
	delete(s.loadingModules, path)
}

func (lc *LexicalContext) Load(name string) (*Var, error) {
	if lc == nil {
		return nil, errors.New("missing lexical context")
	}
	return loadVarFromScope(lc.Executor, lc.Shared, lc.Stack, name)
}

func (lc *LexicalContext) Store(name string, v *Var) error {
	if lc == nil {
		return errors.New("missing lexical context")
	}
	return storeVarToScope(lc.Executor, lc.Shared, lc.Stack, name, v)
}

const (
	DefaultMaxStackDepth = 50000
)

func (s *Stack) DumpVariables() map[string]string {
	result := make(map[string]string)
	curr := s
	for curr != nil {
		if curr.Frame != nil {
			for i, name := range curr.Frame.LocalNames {
				if name == "" {
					continue
				}
				if _, exists := result[name]; exists {
					continue
				}
				if i < len(curr.Frame.Locals) && curr.Frame.Locals[i] != nil {
					result[name] = fmt.Sprintf("%v", unwrapCell(curr.Frame.Locals[i]).Interface())
				}
			}
			for i, name := range curr.Frame.UpvalueNames {
				if name == "" {
					continue
				}
				if _, exists := result[name]; exists {
					continue
				}
				if i < len(curr.Frame.Upvalues) && curr.Frame.Upvalues[i] != nil {
					result[name] = fmt.Sprintf("%v", unwrapCell(curr.Frame.Upvalues[i]).Interface())
				}
			}
			if curr.Frame.Return != nil && curr.Frame.ReturnName != "" {
				if _, exists := result[curr.Frame.ReturnName]; !exists {
					result[curr.Frame.ReturnName] = fmt.Sprintf("%v", unwrapCell(curr.Frame.Return).Interface())
				}
			}
		}
		for name, variable := range curr.MemoryPtr {
			if _, exists := result[name]; !exists {
				result[name] = fmt.Sprintf("%v", variable.Interface())
			}
		}
		curr = curr.Parent
	}
	return result
}

func (f *SlotFrame) ensureLocalSlot(slot int, name string) {
	if f == nil || slot < 0 {
		return
	}
	if f.LocalIndex == nil {
		f.LocalIndex = make(map[string]int)
	}
	for len(f.Locals) <= slot {
		f.Locals = append(f.Locals, nil)
	}
	for len(f.LocalNames) <= slot {
		f.LocalNames = append(f.LocalNames, "")
	}
	if name != "" && f.LocalNames[slot] == "" {
		f.LocalNames[slot] = name
		f.LocalIndex[name] = slot
	}
}

func (f *SlotFrame) ensureUpvalueSlot(slot int, name string) {
	if f == nil || slot < 0 {
		return
	}
	if f.UpvalueIndex == nil {
		f.UpvalueIndex = make(map[string]int)
	}
	for len(f.Upvalues) <= slot {
		f.Upvalues = append(f.Upvalues, nil)
	}
	for len(f.UpvalueNames) <= slot {
		f.UpvalueNames = append(f.UpvalueNames, "")
	}
	if name != "" && f.UpvalueNames[slot] == "" {
		f.UpvalueNames[slot] = name
		f.UpvalueIndex[name] = slot
	}
}

func unwrapCell(v *Var) *Var {
	if v != nil && v.VType == TypeCell {
		return v.Ref.(*Cell).Value
	}
	return v
}

func lookupFrameVarByName(frame *SlotFrame, name string) *Var {
	if frame == nil || name == "" {
		return nil
	}
	if slot, ok := frame.LocalIndex[name]; ok && slot >= 0 && slot < len(frame.Locals) {
		return frame.Locals[slot]
	}
	if slot, ok := frame.UpvalueIndex[name]; ok && slot >= 0 && slot < len(frame.Upvalues) {
		return frame.Upvalues[slot]
	}
	if frame.ReturnName == name {
		return frame.Return
	}
	return nil
}

func lookupFrameSymbolByName(frame *SlotFrame, name string) (SymbolRef, bool) {
	if frame == nil || name == "" {
		return SymbolRef{}, false
	}
	if slot, ok := frame.LocalIndex[name]; ok {
		return SymbolRef{Name: name, Kind: SymbolLocal, Slot: slot}, true
	}
	if slot, ok := frame.UpvalueIndex[name]; ok {
		return SymbolRef{Name: name, Kind: SymbolUpvalue, Slot: slot}, true
	}
	return SymbolRef{}, false
}

func loadVarFromScope(exec *Executor, shared *SharedState, stack *Stack, variable string) (*Var, error) {
	if variable == "nil" {
		return nil, nil
	}
	s := stack
	for s != nil {
		if v, ok := s.MemoryPtr[variable]; ok {
			return v, nil
		}
		if v := lookupFrameVarByName(s.Frame, variable); v != nil {
			return v, nil
		}
		s = s.Parent
	}
	if shared != nil {
		if v, ok := shared.LoadGlobal(variable); ok {
			return v, nil
		}
	}
	if exec != nil {
		exec.mu.RLock()
		defer exec.mu.RUnlock()
		if fn, ok := exec.functions[ast.Ident(variable)]; ok {
			return &Var{
				VType: TypeClosure,
				Ref: &VMClosure{
					FunctionSig:  cloneRuntimeFuncSig(fn.FunctionSig),
					BodyTasks:    cloneTasks(fn.BodyTasks),
					Context:      &LexicalContext{Executor: exec, Shared: shared, Stack: stack},
				},
				TypeInfo: MustParseRuntimeType(ast.TypeClosure),
			}, nil
		}
		if route, ok := exec.routes[variable]; ok {
			return &Var{
				VType: TypeAny,
				Ref:   route,
				TypeInfo: MustParseRuntimeType(ast.TypeClosure),
			}, nil
		}
	}
	return nil, fmt.Errorf("undefined: %s", variable)
}

func storeVarToScope(exec *Executor, shared *SharedState, stack *Stack, variable string, expr *Var) error {
	if variable == "nil" {
		return nil
	}
	s := stack
	for s != nil {
		if sym, ok := lookupFrameSymbolByName(s.Frame, variable); ok {
			ctx := &StackContext{Executor: exec, Shared: shared, Stack: stack}
			return ctx.StoreSymbol(sym, expr)
		}
		if v, ok := s.MemoryPtr[variable]; ok {
			if v != nil && v.VType == TypeCell {
				v = v.Ref.(*Cell).Value
			}
			if expr == nil {
				if v != nil {
					v.TypeInfo, v.VType, v.I64, v.F64, v.Str, v.B, v.Bool, v.Handle, v.Bridge, v.Ref = MustParseRuntimeType("Any"), TypeAny, 0, 0, "", nil, false, 0, nil, nil
					return nil
				}
				break
			}
			if v.RuntimeType().IsAny() && !expr.RuntimeType().IsAny() {
				v.TypeInfo = expr.RuntimeType()
			}
			if v.RuntimeType().IsInterface() && !expr.RuntimeType().IsInterface() {
				wrapped, err := exec.CheckSatisfaction(expr, v.RawType())
				if err != nil {
					return err
				}
				expr = wrapped
			}
			copyVarData(v, expr)
			return nil
		}
		s = s.Parent
	}
	if shared != nil && shared.HasGlobal(variable) {
		ctx := &StackContext{Executor: exec, Shared: shared, Stack: stack}
		return ctx.StoreSymbol(SymbolRef{Name: variable, Kind: SymbolGlobal, Slot: -1}, expr)
	}
	if stack != nil && stack.Depth == 1 && stack.Scope == "global" && shared != nil {
		if expr == nil {
			shared.StoreGlobal(variable, NewVarWithRuntimeType(MustParseRuntimeType("Any"), TypeAny))
		} else {
			shared.StoreGlobal(variable, expr.Copy())
		}
		return nil
	}
	if stack == nil {
		return errors.New("missing lexical stack")
	}
	if expr == nil {
		stack.MemoryPtr[variable] = NewVarWithRuntimeType(MustParseRuntimeType("Any"), TypeAny).Copy()
		return nil
	}
	stack.MemoryPtr[variable] = expr.Copy()
	return nil
}

func (ctx *StackContext) ScopeApply(scope string) {
	newDepth := 1
	var frame *SlotFrame
	if ctx.Stack != nil {
		newDepth = ctx.Stack.Depth + 1
		frame = ctx.Stack.Frame
	}
	if newDepth > DefaultMaxStackDepth {
		panic(errors.New("stack overflow"))
	}
	ctx.Stack = &Stack{
		Parent:    ctx.Stack,
		MemoryPtr: make(map[string]*Var),
		Frame:     frame,
		FrameBase: frame,
		Scope:     scope,
		Depth:     newDepth,
	}
}

func cloneSlotFrame(frame *SlotFrame) *SlotFrame {
	if frame == nil {
		return &SlotFrame{}
	}
	locals := make([]*Var, len(frame.Locals))
	for i, v := range frame.Locals {
		if v != nil {
			locals[i] = v.Copy()
		}
	}
	cloned := &SlotFrame{
		Locals:       locals,
		LocalNames:   append([]string(nil), frame.LocalNames...),
		Upvalues:     append([]*Var(nil), frame.Upvalues...),
		UpvalueNames: append([]string(nil), frame.UpvalueNames...),
		Return:       frame.Return,
		ReturnName:   frame.ReturnName,
	}
	if len(frame.LocalIndex) > 0 {
		cloned.LocalIndex = make(map[string]int, len(frame.LocalIndex))
		for k, v := range frame.LocalIndex {
			cloned.LocalIndex[k] = v
		}
	}
	if len(frame.UpvalueIndex) > 0 {
		cloned.UpvalueIndex = make(map[string]int, len(frame.UpvalueIndex))
		for k, v := range frame.UpvalueIndex {
			cloned.UpvalueIndex[k] = v
		}
	}
	return cloned
}

func (ctx *StackContext) ScopeApplyLoopBody(scope string) {
	newDepth := 1
	var parentFrame *SlotFrame
	if ctx.Stack != nil {
		newDepth = ctx.Stack.Depth + 1
		parentFrame = ctx.Stack.Frame
	}
	if newDepth > DefaultMaxStackDepth {
		panic(errors.New("stack overflow"))
	}
	clonedFrame := cloneSlotFrame(parentFrame)
	syncLimit := 0
	if parentFrame != nil {
		syncLimit = len(parentFrame.Locals)
	}
	ctx.Stack = &Stack{
		Parent:    ctx.Stack,
		MemoryPtr: make(map[string]*Var),
		Frame:     clonedFrame,
		FrameBase: parentFrame,
		FrameSync: syncLimit,
		Scope:     scope,
		Depth:     newDepth,
	}
}

func (ctx *StackContext) SyncLoopScope() {
	if ctx.Stack == nil || ctx.Stack.FrameBase == nil || ctx.Stack.Frame == nil {
		return
	}
	base := ctx.Stack.FrameBase
	loop := ctx.Stack.Frame
	limit := ctx.Stack.FrameSync
	if limit > len(loop.Locals) {
		limit = len(loop.Locals)
	}
	for i := 0; i < limit; i++ {
		src := loop.Locals[i]
		if src == nil {
			continue
		}
		base.ensureLocalSlot(i, "")
		dst := base.Locals[i]
		if dst == nil {
			base.Locals[i] = src.Copy()
			continue
		}
		if src.VType == TypeCell {
			src = src.Ref.(*Cell).Value
		}
		if dst.VType == TypeCell {
			dst = dst.Ref.(*Cell).Value
		}
		copyVarData(dst, src)
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
	if ctx.Stack != nil {
		if sym, ok := lookupFrameSymbolByName(ctx.Stack.Frame, variable); ok {
			return ctx.StoreSymbol(sym, expr)
		}
	}
	return storeVarToScope(ctx.Executor, ctx.Shared, ctx.Stack, variable, expr)
}

func (ctx *StackContext) AddVariable(name string, v *Var) error {
	if ctx.Stack != nil && ctx.Stack.Depth == 1 && ctx.Stack.Scope == "global" && ctx.Shared != nil {
		ctx.Shared.StoreGlobal(name, v.Copy())
		return nil
	}
	ctx.Stack.MemoryPtr[name] = v.Copy()
	return nil
}

func (ctx *StackContext) DeclareSymbol(sym SymbolRef, kind RuntimeType) error {
	if sym.Kind != SymbolLocal || ctx.Stack == nil {
		return ctx.NewVar(sym.Name, kind)
	}
	if ctx.Stack.Frame == nil {
		ctx.Stack.Frame = &SlotFrame{}
	}
	ctx.Stack.Frame.ensureLocalSlot(sym.Slot, sym.Name)
	var v *Var
	if ctx.Executor != nil {
		v = ctx.Executor.initializeType(ctx, kind, 0)
	} else {
		v = NewVarWithRuntimeType(kind, TypeAny)
	}
	ctx.Stack.Frame.Locals[sym.Slot] = v
	return nil
}

func (ctx *StackContext) Load(name string) (*Var, error) {
	if name == "nil" {
		return nil, nil
	}
	if ctx.Stack != nil {
		if sym, ok := lookupFrameSymbolByName(ctx.Stack.Frame, name); ok {
			return ctx.LoadSymbol(sym)
		}
	}
	v, err := loadVarFromScope(ctx.Executor, ctx.Shared, ctx.Stack, name)
	if err != nil {
		return nil, err
	}
	return unwrapCell(v), nil
}

func (ctx *StackContext) LoadSymbol(sym SymbolRef) (*Var, error) {
	switch sym.Kind {
	case SymbolLocal:
		if ctx.Stack != nil && ctx.Stack.Frame != nil && sym.Slot >= 0 && sym.Slot < len(ctx.Stack.Frame.Locals) {
			if v := ctx.Stack.Frame.Locals[sym.Slot]; v != nil {
				return unwrapCell(v), nil
			}
		}
	case SymbolUpvalue:
		if ctx.Stack != nil && ctx.Stack.Frame != nil && sym.Slot >= 0 && sym.Slot < len(ctx.Stack.Frame.Upvalues) {
			if v := ctx.Stack.Frame.Upvalues[sym.Slot]; v != nil {
				return unwrapCell(v), nil
			}
		}
	case SymbolGlobal:
		if ctx.Shared != nil {
			if v, ok := ctx.Shared.LoadGlobal(sym.Name); ok {
				return unwrapCell(v), nil
			}
		}
	case SymbolBuiltin, SymbolUnknown:
	}
	return ctx.Load(sym.Name)
}

func (ctx *StackContext) CaptureVar(name string) (*Var, error) {
	if ctx.Stack != nil {
		if sym, ok := lookupFrameSymbolByName(ctx.Stack.Frame, name); ok {
			return ctx.CaptureSymbol(sym)
		}
	}
	s := ctx.Stack
	for s != nil {
		v, ok := s.MemoryPtr[name]
		if !ok {
			v = lookupFrameVarByName(s.Frame, name)
			ok = v != nil
		}
		if ok {
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

	if ctx.Shared != nil {
		if v, ok := ctx.Shared.LoadGlobal(name); ok {
			if v != nil && v.VType != TypeCell {
				cellValue := v.Copy()
				v.VType = TypeCell
				v.Ref = &Cell{Value: cellValue}
				v.I64, v.F64, v.Str, v.B, v.Bool, v.Handle, v.Bridge = 0, 0, "", nil, false, 0, nil
			}
			return v, nil
		}
	}

	// 检查全局函数定义 (命名函数作为值被捕获)
	if ctx.Executor != nil {
		exec := ctx.Executor
		exec.mu.RLock()
		defer exec.mu.RUnlock()

		// 1. 尝试查找脚本定义的函数
		if fn, ok := exec.functions[ast.Ident(name)]; ok {
			return &Var{
				VType: TypeClosure,
				Ref: &VMClosure{
					FunctionSig:  cloneRuntimeFuncSig(fn.FunctionSig),
					BodyTasks:    cloneTasks(fn.BodyTasks),
					Context:      &LexicalContext{Executor: ctx.Executor, Shared: ctx.Shared, Stack: ctx.Stack},
				},
				TypeInfo: MustParseRuntimeType(ast.TypeClosure),
			}, nil
		}

		// 2. 尝试查找 FFI 路由
		if route, ok := exec.routes[name]; ok {
			return &Var{
				VType: TypeAny,
				Ref:   route,
				TypeInfo: MustParseRuntimeType(ast.TypeClosure),
			}, nil
		}
	}

	return nil, fmt.Errorf("undefined capture: %s", name)
}

func (ctx *StackContext) CaptureSymbol(sym SymbolRef) (*Var, error) {
	switch sym.Kind {
	case SymbolLocal:
		if ctx.Stack != nil && ctx.Stack.Frame != nil && sym.Slot >= 0 {
			ctx.Stack.Frame.ensureLocalSlot(sym.Slot, sym.Name)
			v := ctx.Stack.Frame.Locals[sym.Slot]
			if v == nil {
				return nil, fmt.Errorf("undefined local capture: %s", sym.Name)
			}
			if v.VType != TypeCell {
				cellValue := v.Copy()
				v.VType = TypeCell
				v.Ref = &Cell{Value: cellValue}
				v.I64, v.F64, v.Str, v.B, v.Bool, v.Handle, v.Bridge = 0, 0, "", nil, false, 0, nil
			}
			return v, nil
		}
	case SymbolUpvalue:
		if ctx.Stack != nil && ctx.Stack.Frame != nil && sym.Slot >= 0 && sym.Slot < len(ctx.Stack.Frame.Upvalues) {
			if v := ctx.Stack.Frame.Upvalues[sym.Slot]; v != nil {
				return v, nil
			}
			return nil, fmt.Errorf("undefined upvalue capture: %s", sym.Name)
		}
	case SymbolGlobal:
		if ctx.Shared != nil {
			if v, ok := ctx.Shared.LoadGlobal(sym.Name); ok {
				if v != nil && v.VType != TypeCell {
					cellValue := v.Copy()
					v.VType = TypeCell
					v.Ref = &Cell{Value: cellValue}
					v.I64, v.F64, v.Str, v.B, v.Bool, v.Handle, v.Bridge = 0, 0, "", nil, false, 0, nil
				}
				return v, nil
			}
		}
	}
	return ctx.CaptureVar(sym.Name)
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

func (ctx *StackContext) NewVar(name string, kind RuntimeType) error {
	if ctx.Stack != nil {
		if _, ok := lookupFrameSymbolByName(ctx.Stack.Frame, name); ok {
			return nil
		}
	}
	if _, ok := ctx.Stack.MemoryPtr[name]; ok {
		return nil
	}
	if ctx.Stack != nil && ctx.Stack.Depth == 1 && ctx.Stack.Scope == "global" && ctx.Shared != nil {
		if _, ok := ctx.Shared.LoadGlobal(name); ok {
			return nil
		}
	}
	// 确保变量被正确初始化为零值
	var v *Var
	if ctx.Executor != nil {
		v = ctx.Executor.initializeType(ctx, kind, 0)
	} else {
		v = NewVarWithRuntimeType(kind, TypeAny)
	}
	if ctx.Stack != nil && ctx.Stack.Depth == 1 && ctx.Stack.Scope == "global" && ctx.Shared != nil {
		ctx.Shared.StoreGlobal(name, v)
		return nil
	}
	ctx.Stack.MemoryPtr[name] = v
	return nil
}

func (ctx *StackContext) InitReturn(kind RuntimeType) error {
	if ctx.Stack == nil {
		return errors.New("missing stack for return slot")
	}
	if ctx.Stack.Frame == nil {
		ctx.Stack.Frame = &SlotFrame{}
	}
	if ctx.Stack.Frame.Return != nil {
		return nil
	}
	var v *Var
	if ctx.Executor != nil {
		v = ctx.Executor.initializeType(ctx, kind, 0)
	} else {
		v = NewVarWithRuntimeType(kind, TypeAny)
	}
	ctx.Stack.Frame.Return = v
	ctx.Stack.Frame.ReturnName = "__return__"
	return nil
}

func (ctx *StackContext) LoadReturn() (*Var, error) {
	if ctx.Stack != nil && ctx.Stack.Frame != nil && ctx.Stack.Frame.Return != nil {
		return unwrapCell(ctx.Stack.Frame.Return), nil
	}
	return nil, errors.New("missing return slot")
}

func (ctx *StackContext) StoreReturn(expr *Var) error {
	if ctx.Stack == nil || ctx.Stack.Frame == nil || ctx.Stack.Frame.Return == nil {
		return errors.New("missing return slot")
	}
	v := ctx.Stack.Frame.Return
	if v.VType == TypeCell {
		v = v.Ref.(*Cell).Value
	}
	if expr == nil {
		v.TypeInfo, v.VType, v.I64, v.F64, v.Str, v.B, v.Bool, v.Handle, v.Bridge, v.Ref = MustParseRuntimeType("Any"), TypeAny, 0, 0, "", nil, false, 0, nil, nil
		return nil
	}
	copyVarData(v, expr)
	return nil
}

func (ctx *StackContext) coerceAssignedValue(target, expr *Var) (*Var, error) {
	if target == nil || expr == nil {
		return expr, nil
	}
	if target.RuntimeType().IsInterface() && !expr.RuntimeType().IsInterface() {
		wrapped, err := ctx.Executor.CheckSatisfaction(expr, target.RawType())
		if err != nil {
			return nil, err
		}
		return wrapped, nil
	}
	return expr, nil
}

func (ctx *StackContext) StoreSymbol(sym SymbolRef, expr *Var) error {
	switch sym.Kind {
	case SymbolLocal:
		if ctx.Stack == nil {
			return ctx.Store(sym.Name, expr)
		}
		if ctx.Stack.Frame == nil {
			ctx.Stack.Frame = &SlotFrame{}
		}
		ctx.Stack.Frame.ensureLocalSlot(sym.Slot, sym.Name)
		v := ctx.Stack.Frame.Locals[sym.Slot]
		if v == nil {
			if expr == nil {
				v = NewVarWithRuntimeType(MustParseRuntimeType("Any"), TypeAny)
			} else {
				v = expr.Copy()
			}
			ctx.Stack.Frame.Locals[sym.Slot] = v
			return nil
		}
		if v.VType == TypeCell {
			v = v.Ref.(*Cell).Value
		}
		if expr == nil {
			v.TypeInfo, v.VType, v.I64, v.F64, v.Str, v.B, v.Bool, v.Handle, v.Bridge, v.Ref = MustParseRuntimeType("Any"), TypeAny, 0, 0, "", nil, false, 0, nil, nil
			return nil
		}
		var err error
		expr, err = ctx.coerceAssignedValue(v, expr)
		if err != nil {
			return err
		}
		copyVarData(v, expr)
		return nil
	case SymbolUpvalue:
		if ctx.Stack != nil && ctx.Stack.Frame != nil {
			ctx.Stack.Frame.ensureUpvalueSlot(sym.Slot, sym.Name)
			v := ctx.Stack.Frame.Upvalues[sym.Slot]
			if v == nil {
				if expr == nil {
					v = NewVarWithRuntimeType(MustParseRuntimeType("Any"), TypeAny)
				} else {
					v = expr.Copy()
				}
				ctx.Stack.Frame.Upvalues[sym.Slot] = v
				return nil
			}
			if v.VType == TypeCell {
				v = v.Ref.(*Cell).Value
			}
			if expr == nil {
				v.TypeInfo, v.VType, v.I64, v.F64, v.Str, v.B, v.Bool, v.Handle, v.Bridge, v.Ref = MustParseRuntimeType("Any"), TypeAny, 0, 0, "", nil, false, 0, nil, nil
				return nil
			}
			var err error
			expr, err = ctx.coerceAssignedValue(v, expr)
			if err != nil {
				return err
			}
			copyVarData(v, expr)
			return nil
		}
	case SymbolGlobal:
		if ctx.Shared != nil {
			v, _ := ctx.Shared.LoadGlobal(sym.Name)
			if v == nil {
				if expr == nil {
					ctx.Shared.StoreGlobal(sym.Name, NewVarWithRuntimeType(MustParseRuntimeType("Any"), TypeAny))
				} else {
					ctx.Shared.StoreGlobal(sym.Name, expr.Copy())
				}
				return nil
			}
			if v.VType == TypeCell {
				v = v.Ref.(*Cell).Value
			}
			if expr == nil {
				v.TypeInfo, v.VType, v.I64, v.F64, v.Str, v.B, v.Bool, v.Handle, v.Bridge, v.Ref = MustParseRuntimeType("Any"), TypeAny, 0, 0, "", nil, false, 0, nil, nil
				return nil
			}
			var err error
			expr, err = ctx.coerceAssignedValue(v, expr)
			if err != nil {
				return err
			}
			copyVarData(v, expr)
			return nil
		}
	}
	return ctx.Store(sym.Name, expr)
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
	dest.TypeInfo = src.TypeInfo
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
					if d2, ok := ctx.TaskStack[j].Data.(*CallBoundaryData); ok && d2 != nil && d2.Name != "" {
						callerName = d2.Name
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
