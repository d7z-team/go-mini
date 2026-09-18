package runtime

import (
	"errors"
	"fmt"
	"strings"
)

func (vm *vm) loadExport(modulePath, exportName string) (vmValue, error) {
	module, err := vm.loadModule(modulePath)
	if err != nil {
		return vmValue{}, err
	}
	return loadInitializedExport(module, exportName)
}

func loadInitializedExport(module *moduleInstance, exportName string) (vmValue, error) {
	if module == nil || module.executable == nil {
		return vmValue{}, errors.New("nil module")
	}
	modulePath := module.executable.Artifact.Module.Path
	export, ok := module.executable.Exports[exportName]
	if !ok {
		return vmValue{}, fmt.Errorf("module %q missing export %q", modulePath, exportName)
	}
	switch export.Kind {
	case "function":
		return newVMValue("Function", functionRef{ModulePath: modulePath, FunctionID: export.ID}), nil
	case "const":
		value, ok, err := module.constantValue(export.ID)
		if err != nil {
			return vmValue{}, err
		}
		if !ok {
			return vmValue{}, fmt.Errorf("module %q missing constant %q", modulePath, export.ID)
		}
		return module.qualifyValueForExport(module.cloneValueForStore(value)), nil
	case "global":
		slot, ok := module.state.globals[export.ID]
		if !ok {
			return vmValue{}, fmt.Errorf("module %q missing global %q", modulePath, export.ID)
		}
		return module.qualifyValueForExport(slot.load()), nil
	default:
		return vmValue{}, fmt.Errorf("module export %q is %s, not loadable yet", exportName, export.Kind)
	}
}

func (vm *vm) loadModule(modulePath string) (*moduleInstance, error) {
	module, ok := vm.moduleRegistry().module(modulePath)
	if !ok {
		return nil, fmt.Errorf("module %q is not loaded", modulePath)
	}
	if err := vm.ensureModuleInitialized(module); err != nil {
		return nil, err
	}
	return module, nil
}

func (vm *vm) directCallModule(current *moduleInstance, modulePath string) (*moduleInstance, error) {
	modulePath = strings.TrimSpace(modulePath)
	if modulePath == "" || modulePath == current.executable.Artifact.Module.Path {
		return current, nil
	}
	if current.registry == nil {
		return nil, fmt.Errorf("module %q has no revision registry", current.modulePath())
	}
	module, ok := current.registry.module(modulePath)
	if !ok {
		return nil, fmt.Errorf("module %q is not loaded", modulePath)
	}
	return module, nil
}

func (vm *vm) ensureModuleInitialized(module *moduleInstance) error {
	if module == nil || module.executable == nil {
		return errors.New("nil module")
	}
	if module.state.initState == moduleReady {
		return nil
	}
	if module.state.initState == moduleFailed {
		return module.state.initErr
	}
	if module.state.initState == moduleInitializing {
		return fmt.Errorf("module %q initialization cycle", module.executable.Artifact.Module.Path)
	}
	if _, ok := module.executable.Functions[moduleInitFunctionID]; !ok {
		module.state.initState = moduleReady
		return nil
	}
	module.state.beginInitialization()
	values, err := vm.runFunction(module, moduleInitFunctionID, nil)
	if err != nil {
		module.state.finishInitialization(err)
		return err
	}
	if len(values) != 0 {
		err = fmt.Errorf("module %q init returned %d values", module.executable.Artifact.Module.Path, len(values))
		module.state.finishInitialization(err)
		return err
	}
	module.state.finishInitialization(nil)
	return nil
}

func (vm *vm) moduleForFunctionRef(current *moduleInstance, ref functionRef) (*moduleInstance, error) {
	if ref.exact != nil {
		return ref.exact, nil
	}
	modulePath := strings.TrimSpace(ref.ModulePath)
	if modulePath == "" {
		modulePath = current.modulePath()
	}
	module, ok := vm.moduleRegistry().module(modulePath)
	if !ok {
		return nil, fmt.Errorf("module %q is not loaded", ref.ModulePath)
	}
	return module, nil
}

func (vm *vm) resolveFunctionValue(current *moduleInstance, value vmValue) (*moduleInstance, functionRef, error) {
	if value.Data == nil && current.isFunctionType(value.Type) {
		return nil, functionRef{}, errNilFunctionCall
	}
	ref, ok := value.Data.(functionRef)
	if !ok {
		return nil, functionRef{}, fmt.Errorf("call expects Function value, got %s", value.Type)
	}
	if strings.TrimSpace(ref.FunctionID) == "" {
		return nil, functionRef{}, errNilFunctionCall
	}
	target, err := vm.moduleForFunctionRef(current, ref)
	if err != nil {
		return nil, functionRef{}, err
	}
	if _, ok := target.executable.Functions[ref.FunctionID]; !ok {
		return nil, functionRef{}, fmt.Errorf("unknown function %q", ref.FunctionID)
	}
	return target, ref, nil
}
