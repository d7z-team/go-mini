package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type MiniExecutor struct {
	funcs   map[ast.Ident]funcInfo
	structs []interface{}
	Loader  func(path string) (*ast.ProgramStmt, error)
}

type MiniProgram struct {
	Source   string
	executor *runtime.Executor
}

func (p *MiniProgram) Execute(ctx context.Context) error {
	return p.executor.Execute(ctx)
}

func (p *MiniProgram) GetProgram() *ast.ProgramStmt {
	return p.executor.GetProgram()
}

func NewMiniExecutor() *MiniExecutor {
	res := &MiniExecutor{
		funcs:   make(map[ast.Ident]funcInfo),
		structs: make([]interface{}, 0),
	}
	res.structs = append(res.structs, ast.StdlibStructs...)
	return res
}

func (o *MiniExecutor) SetLoader(loader func(path string) (*ast.ProgramStmt, error)) {
	o.Loader = loader
}

func (o *MiniExecutor) NewRuntimeByGoCode(code string) (*MiniProgram, error) {
	converter := ffigo.NewGoToASTConverter()
	if !strings.HasPrefix(strings.TrimSpace(code), "package ") {
		code = "package main\n" + code
	}
	astTree, err := converter.ConvertSource(code)
	if err != nil {
		return nil, err
	}
	return o.NewRuntimeByAst(astTree)
}

func (o *MiniExecutor) NewRuntimeByGoExpr(code string) (*MiniProgram, error) {
	return o.NewRuntimeByGoCode(`func main(){
` + code + `
}`)
}

func (o *MiniExecutor) NewRuntimeByJSON(data []byte) (*MiniProgram, error) {
	node, err := Unmarshal(data)
	if err != nil {
		return nil, err
	}
	return o.NewRuntimeByAst(node)
}

func (o *MiniExecutor) NewRuntimeByAst(tree ast.Node) (*MiniProgram, error) {
	var src bytes.Buffer
	encoder := json.NewEncoder(&src)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(tree)

	optimize, logs, err := ValidateAndOptimizeWithLoader(tree, o.Loader, func(v *ast.ValidContext) error {
		if err := v.AddNativeStructDefines(o.structs...); err != nil {
			return err
		}
		for s, info := range o.funcs {
			if err := v.AddFuncSpec(s, info.fType); err != nil {
				return err
			}
		}
		return nil
	})
	var astError *ast.MiniAstError
	if err != nil {
		if !errors.As(err, &astError) {
			return nil, &ast.MiniAstError{Err: err, Logs: logs, Node: tree}
		}
		return nil, err
	}

	var actualStructs []any
	for _, s := range o.structs {
		if ps, ok := s.(ast.PackageStructWrapper); ok {
			actualStructs = append(actualStructs, ps)
		} else {
			actualStructs = append(actualStructs, s)
		}
	}
	executor, err := runtime.NewExecutor(optimize, actualStructs...)
	if err != nil {
		if !errors.As(err, &astError) {
			return nil, &ast.MiniAstError{Err: err, Logs: logs, Node: tree}
		}
		return nil, err
	}
	for ident, info := range o.funcs {
		executor.AddGlobalFunc(ident, info.fType, info.fc)
	}
	return &MiniProgram{
		Source:   src.String(),
		executor: executor,
	}, nil
}

type funcInfo struct {
	fType ast.GoMiniType
	fc    any
	doc   string
}

func (o *MiniExecutor) MustAddFunc(name string, fc any, docs ...string) {
	err := o.AddFunc(name, fc, docs...)
	if err != nil {
		panic(err)
	}
}

func (o *MiniExecutor) AddPackageFunc(pkg, name string, fc any, docs ...string) error {
	mangledName := fmt.Sprintf("%s.%s", pkg, name)
	return o.AddFunc(mangledName, fc, docs...)
}

func (o *MiniExecutor) MustAddPackageFunc(pkg, name string, fc any, docs ...string) {
	err := o.AddPackageFunc(pkg, name, fc, docs...)
	if err != nil {
		panic(err)
	}
}

func (o *MiniExecutor) AddPackageStruct(pkg, name string, stru any) {
	o.structs = append(o.structs, ast.PackageStructWrapper{Pkg: pkg, Name: name, Stru: stru})
}

func (o *MiniExecutor) AddFunc(name string, fc any, docs ...string) error {
	of := reflect.TypeOf(fc)
	if of.Kind() != reflect.Func {
		return errors.New("fc must be a function")
	}
	method, b := ast.ParseMethod(of)
	if !b {
		return errors.New("fc must be a supported mini function")
	}
	var doc string
	if len(docs) > 0 {
		doc = docs[0]
	} else {
		// Attempt to extract doc from the source code
		doc = ast.GetFuncDoc(fc)
	}
	o.funcs[ast.Ident(name)] = funcInfo{
		fType: ast.GoMiniType(method.String()),
		fc:    fc,
		doc:   doc,
	}
	return nil
}

func (o *MiniExecutor) AddNativeStruct(stru any) {
	o.structs = append(o.structs, stru)
}

type Schema struct {
	Functions map[string]FuncSchema   `json:"functions"`
	Structs   map[string]StructSchema `json:"structs"`
}

type FuncSchema struct {
	Params []string `json:"params"`
	Return string   `json:"return"`
	Doc    string   `json:"doc"`
}

type StructSchema struct {
	Fields  map[string]string     `json:"fields"`
	Methods map[string]FuncSchema `json:"methods"`
}

func (o *MiniExecutor) GenerateSchema() (*Schema, error) {
	schema := &Schema{
		Functions: make(map[string]FuncSchema),
		Structs:   make(map[string]StructSchema),
	}

	// 模拟一次空验证来获取所有元数据
	_, _, err := ValidateAndOptimize(&ast.ProgramStmt{}, func(v *ast.ValidContext) error {
		if err := v.AddNativeStructDefines(o.structs...); err != nil {
			return err
		}
		for s, info := range o.funcs {
			if err := v.AddFuncSpec(s, info.fType); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	for name, info := range o.funcs {
		callFunc, _ := info.fType.ReadCallFunc()
		params := make([]string, len(callFunc.Params))
		for i, p := range callFunc.Params {
			params[i] = string(p)
		}
		schema.Functions[string(name)] = FuncSchema{
			Params: params,
			Return: string(callFunc.Returns),
			Doc:    info.doc,
		}
	}

	allStructs := append([]any{}, o.structs...)
	allStructs = append(allStructs, ast.StdlibStructs...)

	for _, s := range allStructs {
		var typ reflect.Type
		var structName string

		if ps, ok := s.(ast.PackageStructWrapper); ok {
			typ = reflect.TypeOf(ps.Stru)
			structName = fmt.Sprintf("%s.%s", ps.Pkg, ps.Name)
		} else {
			typ = reflect.TypeOf(s)
		}

		if typ.Kind() == reflect.Ptr {
			typ = typ.Elem()
		}
		native, err := ast.ParseNative(typ)
		if err != nil {
			continue
		}

		// If we had a specific name from PackageStructWrapper, use it
		if structName == "" {
			structName = string(native.StructName)
		}

		structSchema := StructSchema{
			Fields:  make(map[string]string),
			Methods: make(map[string]FuncSchema),
		}
		for fName, fType := range native.Fields {
			structSchema.Fields[string(fName)] = string(fType)
		}
		for mName, mType := range native.Methods {
			params := make([]string, len(mType.Params))
			for i, p := range mType.Params {
				params[i] = string(p)
			}
			structSchema.Methods[string(mName)] = FuncSchema{
				Params: params,
				Return: string(mType.Returns),
				Doc:    mType.Doc,
			}
		}
		schema.Structs[structName] = structSchema
	}

	return schema, nil
}
