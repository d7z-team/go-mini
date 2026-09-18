package runtime

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/d7z-team/mini-go/compiler/types"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

type patchTypeFieldShape struct {
	name, typ, tag     string
	variadic, embedded bool
}

type patchTypeMethodShape struct {
	name, receiver, signature, functionID, modulePath string
	variadic                                          bool
}

type patchTypeShape struct {
	id, name, typ, underlying string
	alias, variadic           bool
	fields                    []patchTypeFieldShape
	methods                   []patchTypeMethodShape
}

type patchUpvalueShape struct {
	id, typ  string
	variadic bool
}

type patchExportShape struct {
	kind, id, typ string
	untyped       bool
}

func buildPatchPlan(current *instanceRevision, next *Program) (*PatchPlan, error) {
	if current == nil || current.code == nil || next == nil || next.code == nil {
		return nil, PatchError{Code: "invalid_program", Message: "current and target programs are required"}
	}
	if current.code.image.Hash == next.code.image.Hash && current.symbolsHash() == next.SymbolsHash() {
		return nil, PatchError{Code: "unchanged", Message: "target program is already current"}
	}
	if current.code.image.Hash == next.code.image.Hash {
		return &PatchPlan{baseHash: current.code.image.Hash, target: next}, nil
	}
	if current.code.image.Root != next.code.image.Root {
		return nil, PatchError{Code: "root_changed", Message: "root module changed"}
	}
	if !slices.Equal(current.code.image.Target.Tags, next.code.image.Target.Tags) {
		return nil, PatchError{Code: "target_changed", Message: "build target changed"}
	}
	changed := make([]string, 0, len(next.code.modules))
	for path, oldModule := range current.code.modules {
		newModule, ok := next.code.modules[path]
		if !ok {
			return nil, PatchError{Code: "module_removed", Message: fmt.Sprintf("module %q was removed", path)}
		}
		if err := compareModuleShape(oldModule, newModule); err != nil {
			return nil, err
		}
		if oldModule.Hash != newModule.Hash {
			changed = append(changed, path)
		}
	}
	for path := range next.code.modules {
		if _, exists := current.code.modules[path]; !exists {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return &PatchPlan{
		baseHash:       current.code.image.Hash,
		target:         next,
		changedModules: changed,
	}, nil
}

func compareModuleShape(oldModule, newModule *executable) error {
	path := oldModule.Artifact.Module.Path
	if oldModule.Artifact.Module.Package != newModule.Artifact.Module.Package {
		return PatchError{Code: "package_changed", Message: fmt.Sprintf("module %q changed package name", path)}
	}
	if err := compareGlobals(oldModule, newModule); err != nil {
		return PatchError{Code: "global_shape_changed", Message: fmt.Sprintf("module %q: %v", path, err)}
	}
	if err := compareExports(oldModule, newModule); err != nil {
		return PatchError{Code: "export_shape_changed", Message: fmt.Sprintf("module %q: %v", path, err)}
	}
	if err := compareNamedTypes(oldModule, newModule); err != nil {
		return PatchError{Code: "type_shape_changed", Message: fmt.Sprintf("module %q: %v", path, err)}
	}
	if err := compareLogicalFunctions(oldModule, newModule); err != nil {
		return PatchError{Code: "call_shape_changed", Message: fmt.Sprintf("module %q: %v", path, err)}
	}
	return nil
}

func compareExports(oldModule, newModule *executable) error {
	newExports := make(map[string]patchExportShape, len(newModule.Artifact.Exports))
	for _, export := range newModule.Artifact.Exports {
		newExports[export.Name] = patchExportShape{
			kind: export.Kind, id: export.ID,
			typ: types.FormatWithTable(&newModule.Artifact.TypeTable, export.Type), untyped: export.Untyped,
		}
	}
	for _, export := range oldModule.Artifact.Exports {
		oldShape := patchExportShape{
			kind: export.Kind, id: export.ID,
			typ: types.FormatWithTable(&oldModule.Artifact.TypeTable, export.Type), untyped: export.Untyped,
		}
		if newShape, ok := newExports[export.Name]; !ok || newShape != oldShape {
			return fmt.Errorf("export %q changed", export.Name)
		}
	}
	return nil
}

func compareGlobals(oldModule, newModule *executable) error {
	if len(oldModule.Artifact.Globals) != len(newModule.Artifact.Globals) {
		return errors.New("global declarations changed")
	}
	newGlobals := make(map[string]ir.Global, len(newModule.Artifact.Globals))
	for _, global := range newModule.Artifact.Globals {
		newGlobals[global.ID] = global
	}
	for _, oldGlobal := range oldModule.Artifact.Globals {
		newGlobal, ok := newGlobals[oldGlobal.ID]
		if !ok {
			return fmt.Errorf("global %q was removed", oldGlobal.ID)
		}
		oldType := types.FormatWithTable(&oldModule.Artifact.TypeTable, oldGlobal.Type)
		newType := types.FormatWithTable(&newModule.Artifact.TypeTable, newGlobal.Type)
		if oldType != newType {
			return fmt.Errorf("global %q changed from %s to %s", oldGlobal.ID, oldType, newType)
		}
	}
	return nil
}

func compareNamedTypes(oldModule, newModule *executable) error {
	newDeclarations := newModule.Artifact.TypeTable.DefinedNamed(newModule.Artifact.Module.Path)
	newTypes := make(map[types.DeclID]types.TypeNode, len(newDeclarations))
	for _, declaration := range newDeclarations {
		newTypes[declaration.Identity.DeclID] = declaration
	}
	for _, oldType := range oldModule.Artifact.TypeTable.DefinedNamed(oldModule.Artifact.Module.Path) {
		newType, ok := newTypes[oldType.Identity.DeclID]
		if !ok {
			return fmt.Errorf("type %q was removed", oldType.Identity.DeclID)
		}
		if !reflect.DeepEqual(typeDeclarationShape(oldModule, oldType), typeDeclarationShape(newModule, newType)) {
			return fmt.Errorf("type %q changed", oldType.Identity.DeclID)
		}
	}
	return nil
}

func typeDeclarationShape(module *executable, declaration types.TypeNode) patchTypeShape {
	ref := types.Ref(declaration)
	fields := module.typeFields(declaration)
	variadic := false
	if signature, ok := module.Artifact.TypeTable.IsFunction(ref); ok {
		variadic = signature.Variadic
	}
	shape := patchTypeShape{
		id:         string(declaration.ID),
		name:       string(declaration.Identity.DeclID),
		typ:        types.FormatWithTable(&module.Artifact.TypeTable, ref),
		underlying: types.FormatWithTable(&module.Artifact.TypeTable, declaration.Underlying),
		alias:      declaration.Alias,
		variadic:   variadic,
		fields:     make([]patchTypeFieldShape, 0, len(fields)),
		methods:    make([]patchTypeMethodShape, 0, len(declaration.Methods)),
	}
	for _, field := range fields {
		fieldVariadic := false
		if signature, ok := module.Artifact.TypeTable.IsFunction(field.Type); ok {
			fieldVariadic = signature.Variadic
		}
		shape.fields = append(shape.fields, patchTypeFieldShape{
			name: field.Name, typ: types.FormatWithTable(&module.Artifact.TypeTable, field.Type),
			tag: field.Tag, variadic: fieldVariadic, embedded: field.Embedded,
		})
	}
	for _, method := range declaration.Methods {
		shape.methods = append(shape.methods, patchTypeMethodShape{
			name: method.Name, receiver: types.FormatWithTable(&module.Artifact.TypeTable, method.Receiver),
			signature:  types.FormatSignature(&module.Artifact.TypeTable, method.Signature),
			functionID: method.FunctionID, modulePath: method.ModulePath, variadic: method.Signature.Variadic,
		})
	}
	return shape
}

func compareLogicalFunctions(oldModule, newModule *executable) error {
	for id, oldFunction := range oldModule.Functions {
		if !isLogicalFunction(oldFunction) {
			continue
		}
		newFunction, ok := newModule.Functions[id]
		if !ok || !isLogicalFunction(newFunction) {
			return fmt.Errorf("function %q was removed", id)
		}
		oldSignature := types.FormatSignature(&oldModule.Artifact.TypeTable, oldFunction.Decl.Signature)
		newSignature := types.FormatSignature(&newModule.Artifact.TypeTable, newFunction.Decl.Signature)
		if oldSignature != newSignature {
			return fmt.Errorf("function %q changed from %s to %s", id, oldSignature, newSignature)
		}
		if !reflect.DeepEqual(functionUpvalueShape(oldModule, oldFunction), functionUpvalueShape(newModule, newFunction)) {
			return fmt.Errorf("function %q changed captured values", id)
		}
	}
	return nil
}

func functionUpvalueShape(module *executable, function loadedFunction) []patchUpvalueShape {
	shape := make([]patchUpvalueShape, len(function.Decl.Upvalues))
	for index, upvalue := range function.Decl.Upvalues {
		signature, functionType := module.Artifact.TypeTable.IsFunction(upvalue.Type)
		variadic := functionType && signature.Variadic
		shape[index] = patchUpvalueShape{id: upvalue.ID, typ: types.FormatWithTable(&module.Artifact.TypeTable, upvalue.Type), variadic: variadic}
	}
	return shape
}

func isLogicalFunction(function loadedFunction) bool {
	return !function.Decl.RevisionLocal
}
