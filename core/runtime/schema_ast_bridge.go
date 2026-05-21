package runtime

import "gopkg.d7z.net/go-mini/core/ast"

func FuncSigFromFunction(fn ast.FunctionType) (*RuntimeFuncSig, error) {
	params := make([]RuntimeType, 0, len(fn.Params))
	names := make([]string, 0, len(fn.Params))
	for _, p := range fn.Params {
		paramType, err := ParseRuntimeType(p.Type)
		if err != nil {
			return nil, err
		}
		params = append(params, paramType)
		names = append(names, string(p.Name))
	}
	retType, err := ParseRuntimeType(fn.Return)
	if err != nil {
		return nil, err
	}
	return &RuntimeFuncSig{
		Spec:       TypeSpec(fn.MiniType()),
		ParamNames: names,
		ParamTypes: params,
		ParamModes: defaultFFIParamModes(len(params)),
		ReturnType: retType,
		Variadic:   fn.Variadic,
	}, nil
}

func (s *RuntimeFuncSig) FunctionType() ast.FunctionType {
	if s == nil {
		return ast.FunctionType{}
	}
	params := make([]ast.FunctionParam, 0, len(s.ParamTypes))
	for i, paramType := range s.ParamTypes {
		name := ""
		if i < len(s.ParamNames) {
			name = s.ParamNames[i]
		}
		params = append(params, ast.FunctionParam{
			Name: ast.Ident(name),
			Type: ast.GoMiniType(paramType.Raw),
		})
	}
	return ast.FunctionType{
		Params:   params,
		Return:   ast.GoMiniType(s.ReturnType.Raw),
		Variadic: s.Variadic,
	}
}

func MustFuncSigFromFunction(fn ast.FunctionType) *RuntimeFuncSig {
	sig, err := FuncSigFromFunction(fn)
	if err != nil {
		panic(err)
	}
	return sig
}
