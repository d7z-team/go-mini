package runtime

import (
	"errors"
	"fmt"
	goruntime "runtime"
	"strconv"
	"sync"
	"weak"

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
	case TypeHostRef:
		return "HostRef"
	case TypeModule:
		return "Module"
	case TypeClosure:
		return "Closure"
	case TypeAny:
		return "Any"
	case TypeInterface:
		return "Interface"
	case TypeStruct:
		return "Struct"
	case TypeError:
		return "Error"
	}
	return "Unknown"
}

const (
	TypeInt   VarType = iota // Always int64
	TypeFloat                // Always float64
	TypeString
	TypeBytes // Raw buffer
	TypeBool
	TypeMap     // Internal VM Map (string keys only)
	TypeArray   // Internal VM Array ([]*Var)
	TypeHandle  // Internal VM pointer
	TypeHostRef // Host resource ID (uint32)
	TypeModule  // Dynamic module object
	TypeClosure // Anonymous function with captured environment
	TypeAny     // Placeholder for unknown/dynamic
	TypeInterface
	TypeStruct
	TypeError
)

type VMInterface struct {
	Target *Var
	Spec   *RuntimeInterfaceSpec
	VTable []*Var
}

func cloneVarForAssign(v *Var) *Var {
	if v == nil {
		return nil
	}
	res := &Var{
		TypeInfo: v.TypeInfo,
		VType:    v.VType,
		I64:      v.I64,
		F64:      v.F64,
		Str:      v.Str,
		B:        v.B,
		Bool:     v.Bool,
		Handle:   v.Handle,
		Bridge:   v.Bridge,
		Ref:      v.Ref, // 共享内部引用
	}
	switch v.VType {
	case TypeInterface:
		if inter, ok := v.Ref.(*VMInterface); ok {
			target := cloneVarForAssign(inter.Target)
			vtable := make([]*Var, len(inter.VTable))
			for i, item := range inter.VTable {
				vtable[i] = cloneVarForAssign(item)
				if vtable[i] != nil {
					if method, ok := vtable[i].Ref.(*VMMethodValue); ok {
						method.Receiver = target
					}
				}
			}
			res.Ref = &VMInterface{
				Target: target,
				Spec:   inter.Spec,
				VTable: vtable,
			}
		}
	case TypeStruct:
		if st, ok := v.Ref.(*VMStruct); ok {
			res.Ref = st.CloneForAssign()
		}
	case TypeClosure:
		if method, ok := v.Ref.(*VMMethodValue); ok {
			res.Ref = &VMMethodValue{
				Receiver:      cloneVarForAssign(method.Receiver),
				Method:        method.Method,
				FuncSig:       CloneRuntimeFuncSig(method.FuncSig),
				DynamicInvoke: method.DynamicInvoke,
			}
		}
	}

	if v.stack.Value() != nil {
		res.stack = weak.Make(v.stack.Value())
	}
	return res
}

func (v *Var) DeepCopy() *Var {
	seen := make(map[*Var]*Var)
	return v.deepCopy(seen)
}

func (v *Var) deepCopy(seen map[*Var]*Var) *Var {
	if v == nil {
		return nil
	}
	if cloned, ok := seen[v]; ok {
		return cloned
	}
	res := &Var{
		TypeInfo: v.TypeInfo,
		VType:    v.VType,
		I64:      v.I64,
		F64:      v.F64,
		Str:      v.Str,
		Bool:     v.Bool,
		Handle:   v.Handle,
		Bridge:   v.Bridge,
	}
	seen[v] = res
	if v.B != nil {
		res.B = make([]byte, len(v.B))
		copy(res.B, v.B)
	}
	switch ref := v.Ref.(type) {
	case *VMArray:
		items := ref.Snapshot()
		cloned := make([]*Var, len(items))
		for i, item := range items {
			cloned[i] = item.deepCopy(seen)
		}
		res.Ref = &VMArray{Data: cloned}
	case *VMMap:
		snapshot := ref.Snapshot()
		cloned := make(map[string]*Var, len(snapshot))
		for k, item := range snapshot {
			cloned[k] = item.deepCopy(seen)
		}
		res.Ref = &VMMap{Data: cloned}
	case *VMStruct:
		res.Ref = ref.DeepCopy(seen)
	case *Slot:
		res.Ref = ref.DeepCopy(seen)
	case *VMModule:
		cloned := make(map[string]*Var, len(ref.Data))
		for k, item := range ref.Data {
			cloned[k] = item.deepCopy(seen)
		}
		res.Ref = &VMModule{Name: ref.Name, Data: cloned}
	case *VMInterface:
		vtable := make([]*Var, len(ref.VTable))
		for i, item := range ref.VTable {
			vtable[i] = item.deepCopy(seen)
		}
		res.Ref = &VMInterface{
			Target: ref.Target.deepCopy(seen),
			Spec:   CloneRuntimeInterfaceSpec(ref.Spec),
			VTable: vtable,
		}
	case *VMError:
		frames := append([]StackFrame(nil), ref.Frames...)
		res.Ref = &VMError{
			Message: ref.Message,
			Value:   ref.Value.deepCopy(seen),
			Frames:  frames,
			IsPanic: ref.IsPanic,
			Cause:   ref.Cause,
			Handle:  ref.Handle,
			Bridge:  ref.Bridge,
		}
	case *VMClosure:
		upvalues := make([]*Slot, len(ref.UpvalueSlots))
		for i, item := range ref.UpvalueSlots {
			upvalues[i] = item.DeepCopy(seen)
		}
		res.Ref = &VMClosure{
			FunctionSig:  CloneRuntimeFuncSig(ref.FunctionSig),
			BodyTasks:    cloneTasks(ref.BodyTasks),
			UpvalueSlots: upvalues,
			UpvalueNames: append([]string(nil), ref.UpvalueNames...),
			Context:      nil,
		}
	case *VMHandle:
		res.Ref = ref
	default:
		res.Ref = ref
	}
	return res
}

type VMClosure struct {
	FunctionSig  *RuntimeFuncSig
	BodyTasks    []Task
	UpvalueSlots []*Slot
	UpvalueNames []string
	Context      *LexicalContext // 闭包所属的词法上下文
}

type VMMethodValue struct {
	Receiver      *Var
	Method        string // Full FFI method name or internal function name
	FuncSig       *RuntimeFuncSig
	DynamicInvoke bool
}

type Var struct {
	TypeInfo RuntimeType
	VType    VarType
	I64      int64
	F64      float64
	Str      string
	B        []byte
	Bool     bool
	Handle   uint32
	Bridge   ffigo.FFIBridge
	Ref      interface{} // Internal structures only: *VMArray, *VMMap, *VMStruct, *VMHandle, *Slot, *VMModule, *VMClosure, *VMInterface

	stack weak.Pointer[Stack]
}

type Slot struct {
	Decl  RuntimeType
	Value *Var
}

func NewSlot(decl RuntimeType, value *Var) *Slot {
	if decl.IsEmpty() {
		if value != nil && !value.RuntimeType().IsEmpty() {
			decl = value.RuntimeType()
		} else {
			decl = MustParseRuntimeType("Any")
		}
	}
	return &Slot{Decl: decl, Value: value}
}

func (s *Slot) Load() *Var {
	if s == nil {
		return nil
	}
	return s.Value
}

func (s *Slot) DeepCopy(seen map[*Var]*Var) *Slot {
	if s == nil {
		return nil
	}
	var v *Var
	if s.Value != nil {
		v = s.Value.deepCopy(seen)
	}
	return &Slot{Decl: s.Decl, Value: v}
}

type VMStruct struct {
	Spec   *RuntimeStructSpec
	Fields []*Slot
	ByName map[string]int
}

func (s *VMStruct) Field(name string) (*Slot, bool) {
	if s == nil {
		return nil, false
	}
	idx, ok := s.ByName[name]
	if !ok || idx < 0 || idx >= len(s.Fields) {
		return nil, false
	}
	return s.Fields[idx], true
}

func (s *VMStruct) CloneForAssign() *VMStruct {
	if s == nil {
		return nil
	}
	fields := make([]*Slot, len(s.Fields))
	for i, field := range s.Fields {
		if field != nil {
			fields[i] = &Slot{Decl: field.Decl, Value: cloneVarForAssign(field.Value)}
		}
	}
	byName := make(map[string]int, len(s.ByName))
	for k, v := range s.ByName {
		byName[k] = v
	}
	return &VMStruct{Spec: s.Spec, Fields: fields, ByName: byName}
}

func (s *VMStruct) DeepCopy(seen map[*Var]*Var) *VMStruct {
	if s == nil {
		return nil
	}
	fields := make([]*Slot, len(s.Fields))
	for i, field := range s.Fields {
		fields[i] = field.DeepCopy(seen)
	}
	byName := make(map[string]int, len(s.ByName))
	for k, v := range s.ByName {
		byName[k] = v
	}
	return &VMStruct{Spec: s.Spec, Fields: fields, ByName: byName}
}

func (v *Var) RuntimeType() RuntimeType {
	if v == nil {
		return RuntimeType{}
	}
	return v.TypeInfo
}

func (v *Var) RawType() TypeSpec {
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

func (v *Var) SetRawType(typ string) {
	if v == nil {
		return
	}
	typeSpec := TypeSpec(typ)
	if typeSpec.IsEmpty() {
		v.TypeInfo = RuntimeType{}
		return
	}
	parsed, err := ParseRuntimeType(typeSpec)
	if err == nil {
		v.TypeInfo = parsed
		return
	}
	v.TypeInfo = RuntimeType{
		Kind:   RuntimeTypeAny,
		Raw:    SpecAny,
		TypeID: CanonicalTypeID(SpecAny.String()),
	}
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
	goruntime.AddCleanup(h, func(args cleanupArgs) {
		if args.ID != 0 && args.Bridge != nil {
			_ = args.Bridge.DestroyHandle(args.ID)
		}
	}, cleanupArgs{ID: id, Bridge: bridge})
	return h
}

func NewPinnedHostRefVar(id uint32, bridge ffigo.FFIBridge, typ RuntimeType) *Var {
	v := &Var{VType: TypeHostRef, Handle: id, Bridge: bridge}
	if id != 0 {
		v.Ref = &VMHandle{ID: id, Bridge: bridge}
	}
	v.SetRuntimeType(typ)
	return v
}

type VMArray struct {
	mu   sync.RWMutex
	Data []*Var
}

func (a *VMArray) Len() int {
	if a == nil {
		return 0
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.Data)
}

func (a *VMArray) Cap() int {
	if a == nil {
		return 0
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return cap(a.Data)
}

func (a *VMArray) Load(i int) (*Var, bool) {
	if a == nil {
		return nil, false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if i < 0 || i >= len(a.Data) {
		return nil, false
	}
	return a.Data[i], true
}

func (a *VMArray) Store(i int, v *Var) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if i < 0 || i >= len(a.Data) {
		return false
	}
	a.Data[i] = v
	return true
}

func (a *VMArray) Snapshot() []*Var {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*Var, len(a.Data))
	copy(out, a.Data)
	return out
}

func (a *VMArray) Slice(low, high int) []*Var {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*Var, high-low)
	copy(out, a.Data[low:high])
	return out
}

func (a *VMArray) ReplaceSlice(low, high int, items []*Var) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if low < 0 || high < low || high > len(a.Data) {
		return false
	}
	next := make([]*Var, 0, low+len(items)+len(a.Data)-high)
	next = append(next, a.Data[:low]...)
	next = append(next, items...)
	next = append(next, a.Data[high:]...)
	a.Data = next
	return true
}

type VMMap struct {
	mu   sync.RWMutex
	Data map[string]*Var
}

func (m *VMMap) Load(key string) (*Var, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.Data[key]
	return v, ok
}

func (m *VMMap) Store(key string, v *Var) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Data == nil {
		m.Data = make(map[string]*Var)
	}
	m.Data[key] = v
}

func (m *VMMap) Delete(key string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Data, key)
}

func (m *VMMap) Len() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Data)
}

func (m *VMMap) Keys() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.Data))
	for k := range m.Data {
		keys = append(keys, k)
	}
	return keys
}

func (m *VMMap) Snapshot() map[string]*Var {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]*Var, len(m.Data))
	for k, v := range m.Data {
		out[k] = v
	}
	return out
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
	if v.VType != TypeHandle && v.VType != TypeHostRef {
		return 0, fmt.Errorf("type mismatch: expected handle-compatible value, got %v", v.VType)
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
	case TypeHostRef:
		return fmt.Sprintf("hostref(%d)", v.Handle)
	case TypeArray:
		if arr, ok := v.Ref.(*VMArray); ok {
			return fmt.Sprintf("array(%d)", arr.Len())
		}
	case TypeMap:
		if m, ok := v.Ref.(*VMMap); ok {
			return fmt.Sprintf("map(%d)", m.Len())
		}
	case TypeStruct:
		if st, ok := v.Ref.(*VMStruct); ok {
			return fmt.Sprintf("struct(%d)", len(st.Fields))
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
	case TypeHostRef:
		return v.Handle
	case TypeArray:
		if arr, ok := v.Ref.(*VMArray); ok {
			items := arr.Snapshot()
			res := make([]interface{}, len(items))
			for i, item := range items {
				res[i] = item.interfaceWithDepth(depth + 1)
			}
			return res
		}
	case TypeMap:
		if m, ok := v.Ref.(*VMMap); ok {
			res := make(map[string]interface{})
			for k, val := range m.Snapshot() {
				res[k] = val.interfaceWithDepth(depth + 1)
			}
			return res
		}
	case TypeStruct:
		if st, ok := v.Ref.(*VMStruct); ok {
			res := make(map[string]interface{}, len(st.Fields))
			if st.Spec != nil {
				for i, field := range st.Spec.Fields {
					if i < len(st.Fields) && st.Fields[i] != nil {
						res[field.Name] = st.Fields[i].Value.interfaceWithDepth(depth + 1)
					}
				}
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

func NewVar[S ~string](typ S, vType VarType) *Var {
	res := &Var{VType: vType}
	res.SetRawType(string(typ))
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
