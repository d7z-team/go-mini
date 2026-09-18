package engine

import (
	"encoding/json"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// LSP Metadata Export

type ExportedMetadata struct {
	Builtins  map[string]string          `json:"builtins"`  // 内置函数名 -> 签名
	Modules   map[string]*ExportedModule `json:"modules"`   // 模块名 -> 模块信息
	Constants map[string]string          `json:"constants"` // 全局常量
}

type ExportedModule struct {
	Functions  map[string]string          `json:"functions"`  // 函数名 -> 签名
	Structs    map[string]*ExportedStruct `json:"structs"`    // 结构体名 -> 结构体信息
	Interfaces map[string]string          `json:"interfaces"` // 接口名 -> 签名
	Constants  map[string]string          `json:"constants"`  // 模块内常量
	Values     map[string]string          `json:"values"`     // 模块内变量名 -> 类型
	Types      map[string]string          `json:"types"`      // 类型名 -> 类型
	Doc        string                     `json:"doc,omitempty"`
}

type ExportedStruct struct {
	Fields  map[string]string `json:"fields"`  // 字段名 -> 类型
	Methods map[string]string `json:"methods"` // 方法名 -> 签名
	Doc     string            `json:"doc,omitempty"`
}

// ExportMetadata 导出当前执行器中注册的所有符号，供 IDE 和 LSP 提供代码补全
func (e *MiniExecutor) ExportMetadata() string {
	modulePlans := make(map[string]*runtime.PreparedProgram)
	e.mu.RLock()
	libraryPaths := make([]string, 0, len(e.sourceLibraries))
	for path := range e.sourceLibraries {
		libraryPaths = append(libraryPaths, path)
	}
	e.mu.RUnlock()
	for _, path := range libraryPaths {
		prepared, err := e.prepareModuleFromSource(path)
		if err == nil && prepared != nil {
			modulePlans[path] = prepared
		}
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	meta := &ExportedMetadata{
		Builtins:  make(map[string]string),
		Modules:   make(map[string]*ExportedModule),
		Constants: make(map[string]string),
	}

	getModule := func(name string) *ExportedModule {
		if _, ok := meta.Modules[name]; !ok {
			meta.Modules[name] = &ExportedModule{
				Functions:  make(map[string]string),
				Structs:    make(map[string]*ExportedStruct),
				Interfaces: make(map[string]string),
				Constants:  make(map[string]string),
				Values:     make(map[string]string),
				Types:      make(map[string]string),
			}
		}
		return meta.Modules[name]
	}

	getStruct := func(modName, structName string) *ExportedStruct {
		mod := getModule(modName)
		if _, ok := mod.Structs[structName]; !ok {
			mod.Structs[structName] = &ExportedStruct{
				Fields:  make(map[string]string),
				Methods: make(map[string]string),
			}
		}
		return mod.Structs[structName]
	}

	for name, spec := range e.funcSchemas {
		sName := string(name)
		if !strings.Contains(sName, ".") && !strings.HasPrefix(sName, "__") {
			meta.Builtins[sName] = e.formatSchemaWithDoc(ast.GoMiniType(spec.Spec), "", spec)
		}
	}
	if e.templates != nil {
		for name, spec := range e.templates.CompletionSchemas() {
			meta.Builtins[name] = spec
		}
	}

	if e.boundSurface != nil {
		for path, pkg := range e.boundSurface.Packages {
			if pkg == nil {
				continue
			}
			mod := getModule(path)
			for name, member := range pkg.Members {
				if member == nil {
					continue
				}
				switch member.Kind {
				case runtime.FFIMemberFunc:
					if route, ok := e.routes[member.RouteName]; ok {
						mod.Functions[name] = e.formatRouteSchema(route)
					}
				case runtime.FFIMemberConst:
					mod.Constants[name] = member.Const.DisplayString()
				case runtime.FFIMemberValue:
					mod.Values[name] = member.Type.Raw.String()
				case runtime.FFIMemberType:
					mod.Types[name] = member.Type.Raw.String()
				}
			}
		}
		if e.boundSurface.Schema != nil {
			for _, typ := range e.boundSurface.Schema.Types {
				if typ == nil {
					continue
				}
				pkg, member := typ.PackagePath, typ.MemberName
				if pkg == "" || member == "" {
					continue
				}
				if typ.Struct != nil {
					st := getStruct(pkg, member)
					for _, field := range typ.Struct.Fields {
						st.Fields[field.Name] = string(field.Type)
					}
					for methodName, method := range typ.Methods {
						if method != nil && method.Sig != nil {
							st.Methods[methodName] = e.formatSchemaWithDoc(ast.GoMiniType(method.Sig.Spec), method.Doc, method.Sig)
						}
					}
				}
				if typ.Interface != nil {
					getModule(pkg).Interfaces[member] = string(typ.Interface.Spec)
				}
			}
		}
	}

	exportPreparedModule := func(modName string, prepared *runtime.PreparedProgram) {
		if prepared == nil {
			return
		}
		mod := getModule(modName)
		for exportName, export := range prepared.Exports {
			target := export.TargetName
			if target == "" {
				target = export.Name
			}
			switch export.Kind {
			case runtime.PreparedExportFunc:
				if fn := prepared.Functions[target]; fn != nil && fn.FunctionSig != nil {
					mod.Functions[exportName] = fn.FunctionSig.SignatureString()
				}
			case runtime.PreparedExportConst:
				if value, ok := prepared.Constants[target]; ok {
					mod.Constants[exportName] = value.DisplayString()
				}
			case runtime.PreparedExportStruct:
				if spec := prepared.StructSchemas[target]; spec != nil {
					st := getStruct(modName, exportName)
					for _, field := range spec.Fields {
						st.Fields[field.Name] = string(field.Type)
					}
				}
			case runtime.PreparedExportInterface:
				if spec := prepared.InterfaceSchemas[target]; spec != nil {
					mod.Interfaces[exportName] = string(spec.Spec)
				}
			case runtime.PreparedExportGlobal:
				if global := prepared.Globals[target]; global != nil {
					mod.Values[exportName] = global.Kind.Raw.String()
				}
			case runtime.PreparedExportType:
				if typ, ok := prepared.NamedTypes[target]; ok {
					mod.Types[exportName] = typ.Raw.String()
				}
			}
		}
	}
	for modName, prepared := range modulePlans {
		exportPreparedModule(modName, prepared)
	}

	data, _ := json.MarshalIndent(meta, "", "  ")
	return string(data)
}
