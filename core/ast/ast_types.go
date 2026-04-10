package ast

import (
	"fmt"
	"strings"
)

// GoMiniType 类型的表达形式
type GoMiniType string

const (
	TypeAny     GoMiniType = "Any"
	TypeModule  GoMiniType = "TypeModule" // 动态模块对象 (JS/Python 风格)
	TypeClosure GoMiniType = "TypeClosure"
)

func (o GoMiniType) IsEmpty() bool { return o == "" }

// BaseName 提取类型的基准名称 (例如 Ptr<MyStruct> -> MyStruct)
func (o GoMiniType) BaseName() string {
	s := string(o)
	if strings.HasPrefix(s, "Ptr<") {
		return GoMiniType(s[4 : len(s)-1]).BaseName()
	}
	if strings.HasPrefix(s, "Array<") {
		return GoMiniType(s[6 : len(s)-1]).BaseName()
	}
	if strings.HasPrefix(s, "Map<") {
		// Just take the value type for base
		_, v, _ := o.GetMapKeyValueTypes()
		return v.BaseName()
	}
	if strings.HasPrefix(s, "Ptr<") {
		return GoMiniType(s[4 : len(s)-1]).BaseName()
	}
	return s
}

func (o GoMiniType) IsVoid() bool {
	return o == "" || o == "Void" || o == "void"
}

func (o GoMiniType) IsPrimitive() bool {
	return o.IsAny() || o.IsString() || o.IsNumeric() || o.IsBool() || o == "TypeBytes" || o == "Error"
}

func (o GoMiniType) ReadCallFunc() (*CallFunctionType, bool) {
	fn, ok := o.ReadFunc()
	if !ok {
		return nil, false
	}
	res := fn.ToCallFunctionType()
	return &res, true
}

func (o GoMiniType) IsAny() bool { return o == TypeAny || o == TypeModule || o == TypeClosure }

func (o GoMiniType) IsString() bool { return o == "String" }
func (o GoMiniType) IsInt() bool    { return o == "Int64" }
func (o GoMiniType) IsBool() bool   { return o == "Bool" }
func (o GoMiniType) IsNumeric() bool {
	s := string(o)
	switch s {
	case "Int64", "Float64":
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
	return strings.HasPrefix(s, "Array<")
}

func (o GoMiniType) IsInterface() bool {
	s := strings.TrimSpace(string(o))
	return strings.HasPrefix(s, "interface") && strings.Contains(s, "{") && strings.HasSuffix(s, "}")
}

func (o GoMiniType) IsStruct() bool {
	s := strings.TrimSpace(string(o))
	return strings.HasPrefix(s, "struct") && strings.Contains(s, "{") && strings.HasSuffix(s, "}")
}

func (o GoMiniType) ReadStructFields() (map[string]GoMiniType, bool) {
	if !o.IsStruct() {
		return nil, false
	}
	s := strings.TrimSpace(string(o))
	start := strings.Index(s, "{")
	inner := s[start+1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		return map[string]GoMiniType{}, true
	}
	parts := strings.Split(inner, ";")
	res := make(map[string]GoMiniType)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fParts := strings.SplitN(p, " ", 2)
		if len(fParts) == 2 {
			res[strings.TrimSpace(fParts[0])] = GoMiniType(strings.TrimSpace(fParts[1]))
		}
	}
	return res, true
}

func (o GoMiniType) ReadInterfaceMethods() (map[string]*FunctionType, bool) {
	if !o.IsInterface() {
		return nil, false
	}
	s := strings.TrimSpace(string(o))
	start := strings.Index(s, "{")
	inner := s[start+1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		return map[string]*FunctionType{}, true
	}
	// 支持分号作为主要分隔符，内部可能含有逗号
	parts := strings.Split(inner, ";")
	methods := make(map[string]*FunctionType)
	for _, p := range parts {
		m := strings.TrimSpace(p)
		if m != "" {
			// 解析方法名和签名：Read(Array<Uint8>, Int64) String
			name := m
			var sig *FunctionType
			if idx := strings.Index(m, "("); idx != -1 {
				name = strings.TrimSpace(m[:idx])
				// 寻找括号配对
				pCount, aCount := 0, 0
				endIdx := -1
				for i := idx; i < len(m); i++ {
					switch m[i] {
					case '(':
						pCount++
					case ')':
						pCount--
					case '<':
						aCount++
					case '>':
						aCount--
					}
					if pCount == 0 && aCount == 0 {
						endIdx = i
						break
					}
				}
				if endIdx != -1 {
					sigStr := "function" + m[idx:endIdx+1]
					retPart := strings.TrimSpace(m[endIdx+1:])
					if retPart != "" {
						sigStr += " " + retPart
					}
					if f, ok := GoMiniType(sigStr).ReadFunc(); ok {
						sig = f
					}
				}
			}
			// 如果没有显式签名，视为通用函数
			if sig == nil {
				sig = &FunctionType{Return: "Any"}
			} else if sig.Return == "Void" {
				// 对于接口方法，如果没有写返回值，通常期望是 Any (兼容动态对象)
				sig.Return = "Any"
			}
			methods[name] = sig
		}
	}
	return methods, true
}

func (o GoMiniType) IsMap() bool {
	s := string(o)
	return strings.HasPrefix(s, "Map<")
}

func (o GoMiniType) ReadArrayItemType() (GoMiniType, bool) {
	s := string(o)
	if strings.HasPrefix(s, "Array<") && len(s) > 7 {
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
	if strings.HasPrefix(s, "Map<") {
		inner := s[4 : len(s)-1]
		parts := splitByComma(inner)
		if len(parts) != 2 {
			return "", "", false
		}
		return GoMiniType(strings.TrimSpace(parts[0])), GoMiniType(strings.TrimSpace(parts[1])), true
	}
	return "", "", false
}

func CreateMapType(keyType, valueType GoMiniType) GoMiniType {
	return GoMiniType(fmt.Sprintf("Map<%s, %s>", keyType, valueType))
}

func (o GoMiniType) ReadFunc() (*FunctionType, bool) {
	s := string(o)
	if !strings.HasPrefix(s, "function(") {
		return nil, false
	}
	start := len("function(")
	pCount, aCount := 1, 0
	paramEnd := -1
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '(':
			pCount++
		case ')':
			pCount--
		case '<':
			aCount++
		case '>':
			aCount--
		}
		if pCount == 0 && aCount == 0 {
			paramEnd = i
			break
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
		part = strings.TrimSpace(part)
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
	if o.IsTuple() && other.IsTuple() {
		oTypes, _ := o.ReadTuple()
		otherTypes, _ := other.ReadTuple()
		if len(oTypes) != len(otherTypes) {
			return false
		}
		for i := range oTypes {
			if !oTypes[i].Equals(otherTypes[i]) {
				return false
			}
		}
		return true
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

	// 1. 处理已有的规范化逻辑
	if o.IsAny() || o == "Void" || o == "Error" || o.IsNumeric() || o.IsString() || o.IsBool() || o == "TypeBytes" {
		return o
	}
	if o.IsArray() {
		elem, _ := o.ReadArrayItemType()
		return CreateArrayType(elem.Resolve(v))
	}
	if o.IsMap() {
		k, val, _ := o.GetMapKeyValueTypes()
		return GoMiniType(fmt.Sprintf("Map<%s, %s>", k.Resolve(v), val.Resolve(v)))
	}
	if o.IsPtr() {
		elem, _ := o.GetPtrElementType()
		return elem.Resolve(v).ToPtr()
	}
	if o.IsTuple() {
		types, _ := o.ReadTuple()
		resolved := make([]GoMiniType, len(types))
		for i, t := range types {
			resolved[i] = t.Resolve(v)
		}
		return CreateTupleType(resolved...)
	}

	s := string(o)
	if strings.Contains(s, ".") {
		resolved := o
		parts := strings.SplitN(s, ".", 2)
		if v != nil && v.root != nil {
			if realPkg, ok := v.root.Imports[parts[0]]; ok {
				resolved = GoMiniType(fmt.Sprintf("%s.%s", realPkg, parts[1]))
			}
			if actual, ok := v.root.types[Ident(resolved)]; ok && !actual.IsStruct() {
				return actual.Resolve(v)
			}
		}
		// 如果前缀不在导入表中，可能是 FFI 或动态注入的包名，保留原始形式
		return resolved
	}
	if v != nil && v.root != nil {
		if actual, ok := v.root.types[Ident(o)]; ok {
			return actual.Resolve(v)
		}
	}
	return o
}

func (o GoMiniType) Valid(v *ValidContext) bool {
	if o.IsAny() || o == "Void" || o == "Error" || o.IsNumeric() || o.IsString() || o.IsBool() || o == "TypeBytes" {
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
	if o.IsTuple() {
		types, ok := o.ReadTuple()
		if !ok {
			return false
		}
		for _, t := range types {
			if !t.Resolve(v).Valid(v) {
				return false
			}
		}
		return true
	}
	if o.IsInterface() {
		return true
	}
	if _, ok := v.GetType(Ident(o)); ok {
		return true
	}
	if _, ok := v.GetInterface(Ident(o)); ok {
		return true
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
	if o.IsAny() || vType.IsAny() {
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
	// 接口兼容性由 IsAssignableTo 在赋值或参数校验时更深入判断
	if o.IsInterface() {
		return pVar, true
	}
	return pVar, o.Equals(vType)
}

func (o GoMiniType) IsAssignableTo(target GoMiniType) bool {
	return o.isAssignableToRecursive(target, 0, DefaultMaxTypeDepth)
}

var DefaultMaxTypeDepth = 256

func (o GoMiniType) isAssignableToRecursive(target GoMiniType, depth, maxDepth int) bool {
	if depth > maxDepth {
		return false // Prevent DoS via recursive types
	}
	if target.IsAny() || o.IsAny() || o == "Constant" {
		return true
	}
	if o.Equals(target) {
		return true
	}
	if target.IsString() && o == "Error" {
		return true // Error 自动转 String
	}
	if target.IsNumeric() && o.IsNumeric() {
		return true // 数值类型互转 (Int64 <-> Float64)
	}
	if o.IsMap() && !target.IsPrimitive() && !target.IsArray() && !target.IsMap() && !target.IsPtr() && !target.IsInterface() {
		// 允许 Map 赋值给命名的结构体类型
		return true
	}
	if target.IsInterface() {
		if o.IsInterface() {
			// 如果双方都是接口，检查 o 是否包含了 target 的所有方法
			oMethods, _ := o.ReadInterfaceMethods()
			targetMethods, _ := target.ReadInterfaceMethods()
			for name := range targetMethods {
				if _, ok := oMethods[name]; !ok {
					return false
				}
				// 暂时只检查方法名存在，未来可进一步检查签名兼容性
			}
			return true
		}
		// 允许非接口类型赋值给接口（由运行时进一步校验鸭子类型）
		return true
	}
	// 处理指针自动解引用/取地址兼容性
	if target.IsPtr() && !o.IsPtr() {
		unPtr, _ := target.GetPtrElementType()
		if unPtr.isAssignableToRecursive(o, depth+1, maxDepth) {
			return true
		}
	}
	if o.IsPtr() && !target.IsPtr() {
		unPtr, _ := o.GetPtrElementType()
		if unPtr.isAssignableToRecursive(target, depth+1, maxDepth) {
			return true
		}
	}
	return o.Equals(target)
}

// IsValid 检查类型字符串是否符合规范（基础类型、规范容器格式或带点包路径）
func (o GoMiniType) IsValid() bool {
	t := string(o)
	if t == "" || t == "Void" {
		return true
	}
	switch t {
	case "Any", "String", "Int64", "Float64", "Bool", "TypeBytes", "Uint8", "Int32", "Float32", "Int", "Int8", "Int16", "Uint16", "Uint32", "Uint", "Error":
		return true
	}
	if strings.HasPrefix(t, "tuple(") && strings.HasSuffix(t, ")") {
		types, ok := o.ReadTuple()
		if !ok {
			return false
		}
		for _, t := range types {
			if !t.IsValid() {
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(t, "Ptr<") && strings.HasSuffix(t, ">") {
		inner, _ := o.GetPtrElementType()
		return inner.IsValid()
	}
	if strings.HasPrefix(t, "Array<") && strings.HasSuffix(t, ">") {
		inner, _ := o.ReadArrayItemType()
		return inner.IsValid()
	}
	if strings.HasPrefix(t, "Map<") && strings.HasSuffix(t, ">") {
		k, v, ok := o.GetMapKeyValueTypes()
		return ok && k.IsValid() && v.IsValid()
	}
	if o.IsInterface() {
		return true
	}
	// 允许带包路径的标识符 (pkg.Type)
	if strings.Contains(t, ".") {
		return true
	}
	// 可能是本地结构体，由 SemanticContext 进一步验证
	return true
}

// IsStrictValid 严格检查类型是否为预定义的原语或规范容器格式
func (o GoMiniType) IsStrictValid() bool {
	t := string(o)
	switch t {
	case "Any", "String", "Int64", "Float64", "Bool", "TypeBytes", "Uint8", "Int32", "Float32", "Int", "Int8", "Int16", "Uint16", "Uint32", "Uint":
		return true
	}
	if strings.HasPrefix(t, "tuple(") && strings.HasSuffix(t, ")") {
		types, ok := o.ReadTuple()
		if !ok {
			return false
		}
		for _, t := range types {
			if !t.IsStrictValid() {
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(t, "Ptr<") && strings.HasSuffix(t, ">") {
		inner, _ := o.GetPtrElementType()
		return inner.IsStrictValid()
	}
	if strings.HasPrefix(t, "Array<") && strings.HasSuffix(t, ">") {
		inner, _ := o.ReadArrayItemType()
		return inner.IsStrictValid()
	}
	if strings.HasPrefix(t, "Map<") && strings.HasSuffix(t, ">") {
		k, v, ok := o.GetMapKeyValueTypes()
		return ok && k.IsStrictValid() && v.IsStrictValid()
	}
	return false
}

// ZeroVar 返回该类型的默认零值（以 Var 形式）
// 注意：该方法返回的是一个简单的值对象，复合类型返回 nil Ref 的 Var
func (o GoMiniType) ZeroVar() interface{} {
	t := string(o)
	if strings.HasPrefix(t, "Ptr<") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "Array<") || strings.HasPrefix(t, "Map<") || t == "Any" || t == "TypeBytes" {
		return nil
	}
	switch t {
	case "Int64", "Uint32", "Int32", "Int":
		return int64(0)
	case "Float64", "Float32":
		return 0.0
	case "String":
		return ""
	case "Bool":
		return false
	}
	return nil
}
