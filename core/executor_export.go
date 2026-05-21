package engine

import (
	"encoding/json"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
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

	// 3. 处理已加载的 Modules (脚本模块)
	for modName, prog := range e.moduleSources {
		mod := getModule(modName)
		// 导出函数
		for fnName, fnStmt := range prog.Functions {
			if len(fnName) > 0 && fnName[0] >= 'A' && fnName[0] <= 'Z' {
				// 我们需要一种方式导出函数文档，目前 ExportedModule.Functions 只是 map[string]string (name -> sig)

				sig := string(fnStmt.FunctionType.MiniType())
				if fnStmt.Doc != "" {
					sig = sig + " // " + strings.ReplaceAll(fnStmt.Doc, "\n", " ")
				}
				mod.Functions[string(fnName)] = sig
			}
		}
		// 导出结构体
		for stName, stStmt := range prog.Structs {
			if len(stName) > 0 && stName[0] >= 'A' && stName[0] <= 'Z' {
				st := getStruct(modName, string(stName))
				st.Doc = stStmt.Doc
				for fName, fType := range stStmt.Fields {
					st.Fields[string(fName)] = string(fType)
				}
			}
		}
		for ifaceName, ifaceStmt := range prog.Interfaces {
			if len(ifaceName) > 0 && ifaceName[0] >= 'A' && ifaceName[0] <= 'Z' {
				mod.Interfaces[string(ifaceName)] = string(ifaceStmt.Type)
			}
		}
		// 导出常量
		for cName, cVal := range prog.Constants {
			if len(cName) > 0 && cName[0] >= 'A' && cName[0] <= 'Z' {
				mod.Constants[cName] = cVal
			}
		}
	}
	for modName, prepared := range e.modules {
		if _, hasSourceModule := e.moduleSources[modName]; hasSourceModule || prepared == nil {
			continue
		}
		mod := getModule(modName)
		for fnName, fn := range prepared.Functions {
			if len(fnName) > 0 && fnName[0] >= 'A' && fnName[0] <= 'Z' && fn != nil && fn.FunctionSig != nil {
				mod.Functions[fnName] = string(fn.FunctionSig.Spec)
			}
		}
		for structName, spec := range prepared.StructSchemas {
			if len(structName) == 0 || structName[0] < 'A' || structName[0] > 'Z' || spec == nil {
				continue
			}
			st := getStruct(modName, structName)
			for _, field := range spec.Fields {
				st.Fields[field.Name] = string(field.Type)
			}
		}
		for ifaceName, spec := range prepared.InterfaceSchemas {
			if len(ifaceName) > 0 && ifaceName[0] >= 'A' && ifaceName[0] <= 'Z' && spec != nil {
				mod.Interfaces[ifaceName] = string(spec.Spec)
			}
		}
		for constName, constVal := range prepared.Constants {
			if len(constName) > 0 && constName[0] >= 'A' && constName[0] <= 'Z' {
				mod.Constants[constName] = constVal
			}
		}
	}

	data, _ := json.MarshalIndent(meta, "", "  ")
	return string(data)
}
