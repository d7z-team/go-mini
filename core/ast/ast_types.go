package ast

import (
	"fmt"
	"strings"
)

// GoMiniType 类型的表达形式
type GoMiniType string

const (
	TypeAny GoMiniType = "Any"
)

func (o GoMiniType) IsEmpty() bool { return o == "" }

func (o GoMiniType) IsVoid() bool {
	return o == "Void" || o == ""
}

func (o GoMiniType) ReadCallFunc() (*CallFunctionType, bool) {
	fn, ok := o.ReadFunc()
	if !ok {
		return nil, false
	}
	res := fn.ToCallFunctionType()
	return &res, true
}

func (o GoMiniType) IsAny() bool { return o == TypeAny }

func (o GoMiniType) IsString() bool { return o == "String" || o == "string" }
func (o GoMiniType) IsBool() bool   { return o == "Bool" || o == "bool" }

func (o GoMiniType) IsNumeric() bool {
	s := string(o)
	switch s {
	case "Int64", "Float64", "Uint8", "Int", "Int8", "Int16", "Int32",
		"Uint", "Uint16", "Uint32", "Uint64", "Uintptr", "Float32", "Complex64", "Complex128":
		return true
	}
	return false
}

func (o GoMiniType) IsPtr() bool {
	s := string(o)
	return strings.HasPrefix(s, "Ptr<") && strings.HasSuffix(s, ">")
}

func (o GoMiniType) IsArray() bool {
	s := string(o)
	return strings.HasPrefix(s, "Array<") && strings.HasSuffix(s, ">")
}

func (o GoMiniType) IsResult() bool {
	s := string(o)
	return strings.HasPrefix(s, "Result<") && strings.HasSuffix(s, ">")
}

func (o GoMiniType) IsMap() bool {
	s := string(o)
	return strings.HasPrefix(s, "Map<") && strings.HasSuffix(s, ">")
}

func (o GoMiniType) ReadArrayItemType() (GoMiniType, bool) {
	s := string(o)
	if o.IsArray() {
		return GoMiniType(s[6 : len(s)-1]), true
	}
	if strings.HasPrefix(s, "...") { // 仅用于兼容处理 Spec 解析
		return GoMiniType(s[3:]), true
	}
	return "", false
}

func CreateArrayType(elementType GoMiniType) GoMiniType {
	return GoMiniType(fmt.Sprintf("Array<%s>", elementType))
}

func (o GoMiniType) GetPtrElementType() (GoMiniType, bool) {
	if !o.IsPtr() {
		return "", false
	}
	s := string(o)
	return GoMiniType(s[4 : len(s)-1]), true
}

func (o GoMiniType) ToPtr() GoMiniType {
	return GoMiniType(fmt.Sprintf("Ptr<%s>", o))
}

func (o GoMiniType) GetMapKeyValueTypes() (keyType, valueType GoMiniType, ok bool) {
	if !o.IsMap() {
		return "", "", false
	}
	s := string(o)
	inner := s[4 : len(s)-1]
	parts := splitByComma(inner)
	if len(parts) != 2 {
		return "", "", false
	}
	return GoMiniType(strings.TrimSpace(parts[0])), GoMiniType(strings.TrimSpace(parts[1])), true
}

func CreateMapType(keyType, valueType GoMiniType) GoMiniType {
	return GoMiniType(fmt.Sprintf("Map<%s, %s>", keyType, valueType))
}

func (o GoMiniType) ReadResult() (GoMiniType, bool) {
	if !o.IsResult() {
		return "", false
	}
	s := string(o)
	return GoMiniType(s[7 : len(s)-1]), true
}

func CreateResultType(elementType GoMiniType) GoMiniType {
	return GoMiniType(fmt.Sprintf("Result<%s>", elementType))
}

func (o GoMiniType) ReadFunc() (*FunctionType, bool) {
	s := string(o)
	if !strings.HasPrefix(s, "function(") {
		return nil, false
	}
	start := len("function(")
	parenCount := 1
	paramEnd := -1
	for i := start; i < len(s); i++ {
		if s[i] == '(' {
			parenCount++
		} else if s[i] == ')' {
			parenCount--
			if parenCount == 0 {
				paramEnd = i
				break
			}
		}
	}
	if paramEnd == -1 {
		return nil, false
	}
	paramsStr := s[start:paramEnd]
	returnsStr := strings.TrimSpace(s[paramEnd+1:])
	params, isVariadic := parseParams(paramsStr)
	var returns GoMiniType = "Void"
	if returnsStr != "" {
		returns = parseReturnType(returnsStr)
	}
	return &FunctionType{Params: params, Return: returns, Variadic: isVariadic}, true
}

type FunctionType struct {
	Params   []FunctionParam `json:"params,omitempty"`
	Return   GoMiniType      `json:"return"`
	Variadic bool            `json:"variadic,omitempty"`
}

func (ft *FunctionType) MiniType() GoMiniType {
	var params []string
	for i, p := range ft.Params {
		prefix := ""
		if ft.Variadic && i == len(ft.Params)-1 {
			prefix = "..."
		}
		params = append(params, prefix+string(p.Type))
	}
	return GoMiniType(fmt.Sprintf("function(%s) %s", strings.Join(params, ","), ft.Return))
}

type FunctionParam struct {
	Name Ident
	Type GoMiniType
}

type CallFunctionType struct {
	Params   []GoMiniType
	Returns  GoMiniType
	Doc      string
	Variadic bool
}

func (c CallFunctionType) String() string {
	var params []string
	for i, p := range c.Params {
		prefix := ""
		if c.Variadic && i == len(c.Params)-1 {
			prefix = "..."
		}
		params = append(params, prefix+string(p))
	}
	return fmt.Sprintf("function(%s) %s", strings.Join(params, ","), c.Returns)
}

func (c CallFunctionType) MiniType() GoMiniType { return GoMiniType(c.String()) }

func parseParams(paramsStr string) ([]FunctionParam, bool) {
	paramsStr = strings.TrimSpace(paramsStr)
	if paramsStr == "" {
		return nil, false
	}
	isVariadic := false
	var params []FunctionParam
	parts := splitByComma(paramsStr)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "...") {
			isVariadic = true
			part = strings.TrimPrefix(part, "...")
		}
		partRunes := []rune(part)
		end := len(partRunes) - 1
		for end >= 0 && partRunes[end] == ' ' {
			end--
		}
		nameEnd := end
		for nameEnd >= 0 && isIdentChar(partRunes[nameEnd]) {
			nameEnd--
		}
		nameEnd++
		var paramName Ident
		var typeStr string
		if nameEnd <= end && nameEnd > 0 && partRunes[nameEnd-1] == ' ' {
			paramName = Ident(partRunes[nameEnd : end+1])
			typeStr = strings.TrimSpace(string(partRunes[:nameEnd-1]))
		} else {
			typeStr = strings.TrimSpace(part)
		}
		params = append(params, FunctionParam{Name: paramName, Type: GoMiniType(typeStr)})
	}
	return params, isVariadic
}

func isIdentChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func parseReturnType(returnStr string) GoMiniType {
	returnStr = strings.TrimSpace(returnStr)
	if returnStr == "" {
		return ""
	}
	if len(returnStr) > 0 && returnStr[0] == '(' {
		returnStr = strings.TrimSpace(returnStr[1 : len(returnStr)-1])
		var types []string
		typeParts := splitByComma(returnStr)
		for _, part := range typeParts {
			if part = strings.TrimSpace(part); part != "" {
				types = append(types, part)
			}
		}
		if len(types) > 1 {
			return GoMiniType("tuple(" + strings.Join(types, ", ") + ")")
		}
		if len(types) == 1 {
			return GoMiniType(types[0])
		}
	}
	return GoMiniType(returnStr)
}

func (o GoMiniType) ReadTuple() ([]GoMiniType, bool) {
	s := string(o)
	if !strings.HasPrefix(s, "tuple(") || !strings.HasSuffix(s, ")") {
		return nil, false
	}
	inner := strings.TrimSpace(s[6 : len(s)-1])
	if inner == "" {
		return []GoMiniType{}, true
	}
	var types []GoMiniType
	for _, part := range splitByComma(inner) {
		if part != "" {
			types = append(types, GoMiniType(part))
		}
	}
	return types, true
}

func splitByComma(s string) []string {
	var parts []string
	var current strings.Builder
	pDepth, bDepth, aDepth := 0, 0, 0
	for _, ch := range s {
		switch ch {
		case '(':
			pDepth++
		case ')':
			pDepth--
		case '[':
			bDepth++
		case ']':
			bDepth--
		case '<':
			aDepth++
		case '>':
			aDepth--
		case ',':
			if pDepth == 0 && bDepth == 0 && aDepth == 0 {
				parts = append(parts, strings.TrimSpace(current.String()))
				current.Reset()
				continue
			}
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}
	return parts
}

func (ft *FunctionType) ToCallFunctionType() CallFunctionType {
	var callParams []GoMiniType
	for _, param := range ft.Params {
		callParams = append(callParams, param.Type)
	}
	return CallFunctionType{Params: callParams, Returns: ft.Return, Variadic: ft.Variadic}
}

func (o GoMiniType) Equals(other GoMiniType) bool {
	if o == other || o.IsAny() || other.IsAny() {
		return true
	}
	if o.IsArray() && other.IsArray() {
		oElem, _ := o.ReadArrayItemType()
		otherElem, _ := other.ReadArrayItemType()
		return oElem.Equals(otherElem)
	}
	if o.IsPtr() && other.IsPtr() {
		oElem, _ := o.GetPtrElementType()
		otherElem, _ := other.GetPtrElementType()
		return oElem.Equals(otherElem)
	}
	return string(o) == string(other)
}

func (o GoMiniType) IsTuple() bool {
	return strings.HasPrefix(string(o), "tuple(") && strings.HasSuffix(string(o), ")")
}

func (o GoMiniType) StructName() (Ident, bool) {
	s := string(o)
	if strings.Contains(s, "(") || strings.Contains(s, "<") {
		return "", false
	}
	// 排除基础类型
	switch s {
	case "Any", "String", "Int64", "Float64", "Bool", "Void", "TypeBytes":
		return "", false
	}
	return Ident(s), true
}

func CreateTupleType(types ...GoMiniType) GoMiniType {
	if len(types) == 0 {
		return "Void"
	}
	if len(types) == 1 {
		return types[0]
	}
	var s []string
	for _, t := range types {
		s = append(s, string(t))
	}
	return GoMiniType("tuple(" + strings.Join(s, ", ") + ")")
}

func (o GoMiniType) Resolve(v *ValidContext) GoMiniType {
	if o.IsEmpty() {
		return o
	}
	if o.IsAny() || o == "Void" || o == "Error" || o.IsNumeric() || o.IsString() || o.IsBool() || o == "TypeBytes" {
		return o
	}
	if o.IsArray() {
		elem, _ := o.ReadArrayItemType()
		return CreateArrayType(elem.Resolve(v))
	}
	if o.IsPtr() {
		elem, _ := o.GetPtrElementType()
		return elem.Resolve(v).ToPtr()
	}
	s := string(o)
	if strings.Contains(s, ".") {
		parts := strings.SplitN(s, ".", 2)
		if realPkg, ok := v.root.Imports[parts[0]]; ok {
			return GoMiniType(fmt.Sprintf("%s.%s", realPkg, parts[1]))
		}
	}
	if v.root.Package != "" && v.root.Package != "main" && !strings.Contains(s, ".") {
		return GoMiniType(fmt.Sprintf("%s.%s", v.root.Package, s))
	}
	return o
}

func (o GoMiniType) Valid(v *ValidContext) bool {
	if o.IsAny() || o == "Void" || o == "Error" || o.IsNumeric() || o.IsString() || o.IsBool() {
		return true
	}
	if o.IsArray() {
		elem, ok := o.ReadArrayItemType()
		return ok && elem.Resolve(v).Valid(v)
	}
	if o.IsPtr() {
		elem, ok := o.GetPtrElementType()
		return ok && elem.Resolve(v).Valid(v)
	}
	_, ok := v.GetStruct(Ident(o))
	return ok
}

func (ft *FunctionType) String() string {
	var pStrs []string
	for i, p := range ft.Params {
		prefix := ""
		if ft.Variadic && i == len(ft.Params)-1 {
			prefix = "..."
		}
		pStrs = append(pStrs, fmt.Sprintf("%s%s %s", prefix, p.Type, p.Name))
	}
	return fmt.Sprintf("function(%s) %s", strings.Join(pStrs, ", "), ft.Return)
}

func (o GoMiniType) AutoPtr(pVar Expr) (Expr, bool) {
	vType := pVar.GetBase().Type
	if o.IsAny() {
		return pVar, true
	}
	if o.Equals(vType) {
		return pVar, true
	}
	if o.IsPtr() && !vType.IsPtr() {
		unPtr, _ := o.GetPtrElementType()
		if unPtr.Equals(vType) {
			// 在隔离架构下不使用真实的指针，直接返回原表达式，复合类型按引用传递
			return pVar, true
		}
	}
	return pVar, true
}

func (o GoMiniType) IsAssignableTo(target GoMiniType) bool { return o.Equals(target) }
