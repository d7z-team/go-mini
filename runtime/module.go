package runtime

import (
	"errors"
	"fmt"

	"github.com/d7z-team/mini-go/compiler/types"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

type moduleRegistry struct {
	modules  map[string]*moduleInstance
	revision uint64
}

type moduleInitState uint8

const (
	moduleUninitialized moduleInitState = iota
	moduleInitializing
	moduleReady
	moduleFailed
)

type moduleState struct {
	globals   map[string]*slot
	initState moduleInitState
	initDone  chan struct{}
	initErr   error
}

func (s *moduleState) beginInitialization() {
	s.initState = moduleInitializing
	s.initDone = make(chan struct{})
	s.initErr = nil
}

func (s *moduleState) finishInitialization(err error) {
	if s.initState != moduleInitializing {
		return
	}
	if err != nil {
		s.initState = moduleFailed
		s.initErr = err
	} else {
		s.initState = moduleReady
	}
	close(s.initDone)
	s.initDone = nil
}

type moduleInstance struct {
	executable             *executable
	registry               *moduleRegistry
	revision               *instanceRevision
	vm                     *vm
	state                  *moduleState
	constants              map[string]vmValue
	constantDefs           map[string]ir.Constant
	constantData           []vmValue
	constantSet            []bool
	globalCells            []*slot
	framePools             map[string][]*frame
	framePoolBytes         int64
	framePoolPhysicalBytes int64

	localizeTypeCache            map[string]string
	qualifyTypeCache             map[string]string
	qualifiedTypeCache           map[string]qualifiedTypeResolution
	underlyingTypeCache          map[string]typeTextResolution
	interfaceTypeCache           map[string]typeTextResolution
	typeIdentityCache            map[string]typeTextResolution
	runtimeTypeCache             map[string]vmType
	runtimeTypes                 map[types.TypeRef]vmType
	resolvedRuntimeTypes         map[types.TypeRef]vmType
	zeroValueCache               map[types.TypeRef]vmValue
	structFieldsCache            map[string]structFieldsResolution
	structSchemaCache            map[string]*structSchema
	interfaceMethodsCache        map[string]methodSetResolution
	declaredMethodSetCache       map[string]methodSetResolution
	valueMethodSetCache          map[string]methodSetResolution
	methodFunctionCache          map[string]methodFunctionResolution
	interfaceImplementationCache map[string]boolResolution
	typeInfoCache                map[types.TypeID]TypeInfo
	reflectTypeDescriptorCache   map[string]vmValue
}

type qualifiedTypeResolution struct {
	module   *moduleInstance
	name     string
	revision uint64
	found    bool
}

type typeTextResolution struct {
	text     string
	revision uint64
	found    bool
}

type structFieldsResolution struct {
	fields []TypeFieldInfo
	found  bool
}

type methodSetResolution struct {
	methods  map[string]string
	revision uint64
}

type methodFunctionResolution struct {
	module       *moduleInstance
	functionID   string
	signature    string
	receiverType string
	revision     uint64
	found        bool
}

type boolResolution struct {
	value    bool
	revision uint64
}

func (m *moduleInstance) formatType(ref types.TypeRef) string {
	return m.runtimeType(ref).String()
}

func (m *moduleInstance) runtimeType(ref types.TypeRef) vmType {
	if m == nil || m.executable == nil {
		return vmType{Ref: ref}
	}
	if runtimeType, ok := m.runtimeTypes[ref]; ok {
		return runtimeType
	}
	runtimeType := runtimeTypeWithTable(ref, &m.executable.Artifact.TypeTable)
	runtimeType.text = types.FormatWithTable(&m.executable.Artifact.TypeTable, ref)
	if m.runtimeTypes == nil {
		m.runtimeTypes = make(map[types.TypeRef]vmType)
	}
	m.runtimeTypes[ref] = runtimeType
	return runtimeType
}

type functionRef struct {
	ModulePath string
	FunctionID string
	exact      *moduleInstance
	upvalues   map[string]*slot
}

func newModuleRegistry() *moduleRegistry {
	return &moduleRegistry{modules: make(map[string]*moduleInstance)}
}

func (r *moduleRegistry) addExecutable(executable *executable) error {
	if executable == nil {
		return errors.New("nil module executable")
	}
	path := executable.Artifact.Module.Path
	if path == "" {
		return errors.New("module executable missing path")
	}
	if r.modules == nil {
		r.modules = make(map[string]*moduleInstance)
	}
	if _, exists := r.modules[path]; exists {
		return fmt.Errorf("duplicate module %q", path)
	}
	instance := newModuleInstance(executable)
	return r.addModule(instance)
}

func (r *moduleRegistry) addModule(instance *moduleInstance) error {
	if instance == nil || instance.executable == nil {
		return errors.New("nil module")
	}
	path := instance.executable.Artifact.Module.Path
	if path == "" {
		return errors.New("module missing path")
	}
	if r.modules == nil {
		r.modules = make(map[string]*moduleInstance)
	}
	if _, exists := r.modules[path]; exists {
		return fmt.Errorf("duplicate module %q", path)
	}
	instance.registry = r
	r.modules[path] = instance
	r.revision++
	return nil
}

func (r *moduleRegistry) module(path string) (*moduleInstance, bool) {
	if r == nil {
		return nil, false
	}
	module, ok := r.modules[path]
	return module, ok
}

func (r *moduleRegistry) clone() *moduleRegistry {
	out := newModuleRegistry()
	if r == nil {
		return out
	}
	for path, module := range r.modules {
		cloned := module.clone()
		cloned.registry = out
		out.modules[path] = cloned
	}
	out.revision = r.revision
	return out
}

func (m *moduleInstance) clone() *moduleInstance {
	if m == nil {
		return nil
	}
	constants := make(map[string]vmValue, len(m.constants))
	for id, value := range m.constants {
		constants[id] = value
	}
	constantDefs := make(map[string]ir.Constant, len(m.constantDefs))
	for id, constant := range m.constantDefs {
		constantDefs[id] = ir.Constant{
			ID:      constant.ID,
			Type:    constant.Type,
			Value:   append([]byte(nil), constant.Value...),
			Untyped: constant.Untyped,
		}
	}
	cloned := &moduleInstance{
		executable:   m.executable,
		registry:     m.registry,
		state:        &moduleState{globals: make(map[string]*slot, len(m.state.globals)), initState: m.state.initState, initErr: m.state.initErr},
		constants:    constants,
		constantDefs: constantDefs,
		constantData: append([]vmValue(nil), m.constantData...),
		constantSet:  append([]bool(nil), m.constantSet...),
		globalCells:  make([]*slot, len(m.globalCells)),
	}
	if m.state.initState == moduleInitializing {
		cloned.state.beginInitialization()
	}
	for id, globalSlot := range m.state.globals {
		if globalSlot == nil {
			cloned.state.globals[id] = nil
			continue
		}
		cell := &slot{
			typ:         globalSlot.typ,
			variadic:    globalSlot.variadic,
			module:      cloned,
			value:       globalSlot.value,
			initialized: globalSlot.initialized,
		}
		cloned.state.globals[id] = cell
		if index, ok := m.executable.Globals[id]; ok {
			cloned.globalCells[index] = cell
		}
	}
	return cloned
}

func newModuleInstance(executable *executable) *moduleInstance {
	instance := &moduleInstance{
		executable:                 executable,
		state:                      &moduleState{globals: make(map[string]*slot, len(executable.Artifact.Globals))},
		constants:                  make(map[string]vmValue, len(executable.Artifact.Constants)),
		constantDefs:               make(map[string]ir.Constant, len(executable.Artifact.Constants)),
		constantData:               make([]vmValue, len(executable.Artifact.Constants)),
		constantSet:                make([]bool, len(executable.Artifact.Constants)),
		globalCells:                make([]*slot, len(executable.Artifact.Globals)),
		framePools:                 make(map[string][]*frame),
		typeInfoCache:              make(map[types.TypeID]TypeInfo),
		reflectTypeDescriptorCache: make(map[string]vmValue),
	}
	for _, constant := range executable.Artifact.Constants {
		instance.constantDefs[constant.ID] = ir.Constant{
			ID:      constant.ID,
			Type:    constant.Type,
			Value:   append([]byte(nil), constant.Value...),
			Untyped: constant.Untyped,
		}
	}
	globals := make(map[string]*slot, len(executable.Artifact.Globals))
	for i, global := range executable.Artifact.Globals {
		_, variadic, _ := instance.functionTypeInfo(global.Type)
		cell := newSlot(instance.runtimeType(global.Type), instance, variadic)
		globals[global.ID] = cell
		instance.globalCells[i] = cell
	}
	instance.state.globals = globals
	return instance
}

func bindModuleState(executable *executable, state *moduleState) (*moduleInstance, error) {
	if executable == nil || state == nil {
		return nil, errors.New("nil module code or state")
	}
	bound := newModuleInstance(executable)
	bound.state = state
	bound.globalCells = make([]*slot, len(executable.Artifact.Globals))
	for index, global := range executable.Artifact.Globals {
		cell, ok := state.globals[global.ID]
		if !ok || cell == nil {
			return nil, fmt.Errorf("module state missing global %q", global.ID)
		}
		bound.globalCells[index] = cell
	}
	return bound, nil
}

func (m *moduleInstance) constantValueAt(index int) (vmValue, error) {
	if m.constantSet[index] {
		return m.constantData[index], nil
	}
	return m.decodeConstantAt(index)
}

func (m *moduleInstance) decodeConstantAt(index int) (vmValue, error) {
	constant := m.executable.Artifact.Constants[index]
	value, err := m.decodeConstant(m.runtimeType(constant.Type), constant.Value)
	if err != nil {
		return vmValue{}, fmt.Errorf("constant %s: %w", constant.ID, err)
	}
	m.constantData[index] = value
	m.constantSet[index] = true
	m.constants[constant.ID] = value
	return value, nil
}

func (m *moduleInstance) constantValue(id string) (vmValue, bool, error) {
	if m == nil {
		return vmValue{}, false, errors.New("nil module")
	}
	if value, ok := m.constants[id]; ok {
		return value, true, nil
	}
	constant, ok := m.constantDefs[id]
	if !ok {
		return vmValue{}, false, nil
	}
	value, err := m.decodeConstant(m.runtimeType(constant.Type), constant.Value)
	if err != nil {
		return vmValue{}, true, fmt.Errorf("constant %s: %w", id, err)
	}
	m.constants[id] = value
	return value, true, nil
}
