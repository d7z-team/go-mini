package ast

import (
	"strings"

	"gopkg.d7z.net/go-mini/core/internal/miniident"
)

type ModuleExports struct {
	Path          string
	Functions     map[Ident]*FunctionStmt
	Methods       map[Ident]*FunctionStmt
	Vars          map[Ident]GoMiniType
	VarDefs       map[Ident]Expr
	Constants     map[string]string
	ConstantTypes map[string]GoMiniType
	ConstantLocs  map[string]*Position
	Types         map[Ident]GoMiniType
	TypeLocs      map[Ident]*Position
	Structs       map[Ident]*StructStmt
	StructDefs    map[Ident]*ValidStruct
	Interfaces    map[Ident]*InterfaceStmt
}

func NewModuleExportsFromRoot(path string, root *ValidRoot) *ModuleExports {
	exports := &ModuleExports{
		Path:          strings.TrimSpace(path),
		Functions:     make(map[Ident]*FunctionStmt),
		Methods:       make(map[Ident]*FunctionStmt),
		Vars:          make(map[Ident]GoMiniType),
		VarDefs:       make(map[Ident]Expr),
		Constants:     make(map[string]string),
		ConstantTypes: make(map[string]GoMiniType),
		ConstantLocs:  make(map[string]*Position),
		Types:         make(map[Ident]GoMiniType),
		TypeLocs:      make(map[Ident]*Position),
		Structs:       make(map[Ident]*StructStmt),
		StructDefs:    make(map[Ident]*ValidStruct),
		Interfaces:    make(map[Ident]*InterfaceStmt),
	}
	if root == nil || root.program == nil {
		return exports
	}
	prog := root.program
	for name, fn := range prog.Functions {
		if fn == nil {
			continue
		}
		if fn.ReceiverType != "" || strings.Contains(string(name), ".") {
			if isExportedModuleMethod(name) {
				exports.Methods[name] = fn
			}
			continue
		}
		if !isExportedModuleMember(name) {
			continue
		}
		exports.Functions[name] = fn
		exports.Vars[name] = fn.FunctionType.MiniType()
	}
	for name, expr := range prog.Variables {
		if _, ok := expr.(*ImportExpr); ok {
			continue
		}
		if !isExportedModuleMember(name) {
			continue
		}
		if t, ok := root.vars[name]; ok {
			exports.Vars[name] = t
			exports.VarDefs[name] = expr
		}
	}
	for name, val := range prog.Constants {
		if !isExportedModuleMember(Ident(name)) {
			continue
		}
		exports.Constants[name] = val
		if prog.ConstantTypes != nil {
			if typ := prog.ConstantTypes[name]; typ != "" {
				exports.ConstantTypes[name] = typ
				exports.Vars[Ident(name)] = typ
			} else {
				exports.Vars[Ident(name)] = "Constant"
			}
		} else {
			exports.Vars[Ident(name)] = "Constant"
		}
		if prog.ConstantLocs != nil {
			exports.ConstantLocs[name] = prog.ConstantLocs[name]
		}
	}
	for name := range prog.Types {
		if !isExportedModuleMember(name) {
			continue
		}
		if t, ok := root.types[name]; ok {
			exports.Types[name] = t
		} else {
			exports.Types[name] = prog.Types[name]
		}
		if prog.TypeLocs != nil {
			exports.TypeLocs[name] = prog.TypeLocs[name]
		}
	}
	for name, st := range prog.Structs {
		if st == nil {
			continue
		}
		if !isExportedModuleMember(name) {
			continue
		}
		exports.Structs[name] = st
		qualified := Ident(CreateQualifiedType(exports.Path, string(name)))
		if st.QualifiedName != "" {
			qualified = st.QualifiedName
		}
		if def, ok := root.structs[qualified]; ok {
			exports.StructDefs[name] = def
		} else if def, ok := root.structs[name]; ok {
			exports.StructDefs[name] = def
		}
	}
	for name, it := range prog.Interfaces {
		if it == nil {
			continue
		}
		if !isExportedModuleMember(name) {
			continue
		}
		exports.Interfaces[name] = it
		exports.Types[name] = CreateQualifiedType(exports.Path, string(name))
	}
	return exports
}

func isExportedModuleMember(name Ident) bool {
	return miniident.IsExported(string(name))
}

func isExportedModuleMethod(name Ident) bool {
	return miniident.IsExportedQualifiedMember(string(name))
}

func (m *ModuleExports) MemberType(name Ident) (GoMiniType, bool) {
	if m == nil || name == "" {
		return "", false
	}
	if t, ok := m.Vars[name]; ok {
		return t, true
	}
	if _, ok := m.Structs[name]; ok {
		return CreateQualifiedType(m.Path, string(name)), true
	}
	if it, ok := m.Interfaces[name]; ok && it != nil {
		return CreateQualifiedType(m.Path, string(name)), true
	}
	if t, ok := m.Types[name]; ok {
		return t, true
	}
	return "", false
}

func (m *ModuleExports) MethodDefinition(typeName GoMiniType, property Ident) Node {
	if m == nil || property == "" {
		return nil
	}
	baseName := typeName.BaseName()
	for _, key := range []Ident{
		Ident(baseName + "." + string(property)),
		Ident(string(typeName) + "." + string(property)),
	} {
		if fn := m.Methods[key]; fn != nil {
			return fn
		}
	}
	return nil
}

func (m *ModuleExports) Definition(name Ident) Node {
	if m == nil || name == "" {
		return nil
	}
	if fn := m.Functions[name]; fn != nil {
		return fn
	}
	if expr := m.VarDefs[name]; expr != nil {
		return expr
	}
	if st := m.Structs[name]; st != nil {
		return st
	}
	if it := m.Interfaces[name]; it != nil {
		return it
	}
	if val, ok := m.Constants[string(name)]; ok {
		typ := GoMiniType("Constant")
		if m.ConstantTypes != nil {
			if known := m.ConstantTypes[string(name)]; known != "" {
				typ = known
			}
		}
		return &LiteralExpr{
			BaseNode: BaseNode{
				ID:   "module_const_" + m.Path + "." + string(name),
				Meta: "constant",
				Type: typ,
				Loc:  m.ConstantLocs[string(name)],
			},
			Value: val,
		}
	}
	if t, ok := m.Types[name]; ok {
		return &IdentifierExpr{
			BaseNode: BaseNode{
				ID:   "module_type_" + m.Path + "." + string(name),
				Meta: "type",
				Type: t,
				Loc:  m.TypeLocs[name],
			},
			Name: name,
		}
	}
	return nil
}
