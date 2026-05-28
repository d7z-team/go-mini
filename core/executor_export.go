package engine

import (
	"encoding/json"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/internal/miniident"
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
	Doc        string                     `json:"doc,omitempty"`
}

type ExportedStruct struct {
	Fields  map[string]string `json:"fields"`  // 字段名 -> 类型
	Methods map[string]string `json:"methods"` // 方法名 -> 签名
	Doc     string            `json:"doc,omitempty"`
}

// ExportMetadata 导出当前执行器中注册的所有符号，供 IDE 和 LSP 提供代码补全
func (e *MiniExecutor) ExportMetadata() string {
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

	// 1. 导出 builtin 函数签名
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

	for name, spec := range e.interfacesMeta {
		sName := string(name)
		if !strings.Contains(sName, ".") {
			continue
		}
		parts := strings.SplitN(sName, ".", 2)
		getModule(parts[0]).Interfaces[parts[1]] = string(spec.Spec)
	}

	// 2. 处理 FFI Routes 和 Methods
	for routeName, route := range e.routes {
		sig := e.formatRouteSchema(route)
		// 处理新版 Type.Method 格式
		if strings.Contains(routeName, ".") && strings.Count(routeName, ".") == 1 {
			parts := strings.SplitN(routeName, ".", 2)
			typeName, methodName := parts[0], parts[1]
			// 如果 typeName 看起来像是一个已经注册的结构体
			if _, ok := e.structsMeta[ast.Ident(typeName)]; ok {
				getStruct("ffi", typeName).Methods[methodName] = sig
				continue
			}
		}

		if strings.Count(routeName, ".") >= 2 {
			parts := strings.SplitN(routeName, ".", 3)
			modName, typeName, methodName := parts[0], parts[1], parts[2]
			getStruct(modName, typeName).Methods[methodName] = sig
			continue
		}
		if strings.Contains(routeName, ".") {
			parts := strings.SplitN(routeName, ".", 2)
			modName, funcName := parts[0], parts[1]
			getModule(modName).Functions[funcName] = sig
		}
	}

	exportProgramModule := func(modName string, prog *ast.ProgramStmt) {
		if prog == nil {
			return
		}
		mod := getModule(modName)
		for fnName, fnStmt := range prog.Functions {
			if miniident.IsExported(string(fnName)) {
				sig := string(fnStmt.FunctionType.MiniType())
				if fnStmt.Doc != "" {
					sig = sig + " // " + strings.ReplaceAll(fnStmt.Doc, "\n", " ")
				}
				mod.Functions[string(fnName)] = sig
			}
		}
		for stName, stStmt := range prog.Structs {
			if miniident.IsExported(string(stName)) {
				st := getStruct(modName, string(stName))
				st.Doc = stStmt.Doc
				for fName, fType := range stStmt.Fields {
					st.Fields[string(fName)] = string(fType)
				}
			}
		}
		for ifaceName, ifaceStmt := range prog.Interfaces {
			if miniident.IsExported(string(ifaceName)) {
				mod.Interfaces[string(ifaceName)] = string(ifaceStmt.Type)
			}
		}
		for cName, cVal := range prog.Constants {
			if miniident.IsExported(cName) {
				mod.Constants[cName] = cVal
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
				target = exportName
			}
			switch export.Kind {
			case runtime.PreparedExportFunc:
				if fn := prepared.Functions[target]; fn != nil && fn.FunctionSig != nil {
					mod.Functions[export.Name] = string(fn.FunctionSig.Spec)
				} else {
					mod.Functions[export.Name] = export.Type.Raw.String()
				}
			case runtime.PreparedExportConst:
				if val, ok := prepared.Constants[target]; ok {
					mod.Constants[export.Name] = val.DisplayString()
				}
			case runtime.PreparedExportStruct:
				st := getStruct(modName, export.Name)
				if spec := prepared.StructSchemas[target]; spec != nil {
					for _, field := range spec.Fields {
						st.Fields[field.Name] = string(field.Type)
					}
				}
			case runtime.PreparedExportInterface:
				if spec := prepared.InterfaceSchemas[target]; spec != nil {
					mod.Interfaces[export.Name] = string(spec.Spec)
				}
			case runtime.PreparedExportGlobal, runtime.PreparedExportType:
				mod.Constants[export.Name] = export.Type.Raw.String()
			}
		}
	}

	// 3. 处理已加载的 Modules (脚本模块)
	for modName, prog := range e.moduleSources {
		exportProgramModule(modName, prog)
	}
	for modName := range e.sourceLibraries {
		prog, _, err := e.loadRegisteredModuleProgram(modName)
		if err != nil || prog == nil {
			continue
		}
		exportProgramModule(modName, prog)
	}
	for modName, prepared := range e.modules {
		if _, hasSourceModule := e.moduleSources[modName]; hasSourceModule || prepared == nil {
			continue
		}
		if _, hasSourceLibrary := e.sourceLibraries[modName]; hasSourceLibrary {
			continue
		}
		exportPreparedModule(modName, prepared)
	}

	data, _ := json.MarshalIndent(meta, "", "  ")
	return string(data)
}
