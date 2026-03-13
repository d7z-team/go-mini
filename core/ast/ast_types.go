package ast

import (
	"fmt"
	"strconv"
	"strings"
)

// OPSType 类型的粗略表达形式
type OPSType string

const (
	TypeAny OPSType = "Any"
)

func (o OPSType) IsEmpty() bool {
	return o == ""
}

func (o OPSType) IsVoid() bool {
	return o == "Void"
}

func (o OPSType) IsPtr() bool {
	s := string(o)
	return strings.HasPrefix(s, "Ptr<") && strings.HasSuffix(s, ">")
}

func (o OPSType) IsArray() bool {
	s := string(o)
	return strings.HasPrefix(s, "Array<") && strings.HasSuffix(s, ">")
}

func (o OPSType) IsMap() bool {
	s := string(o)
	return strings.HasPrefix(s, "Map<") && strings.HasSuffix(s, ">")
}

func (o OPSType) IsAny() bool {
	return o == TypeAny
}

// ReadArrayItemType 获取数组元素类型
func (o OPSType) ReadArrayItemType() (OPSType, bool) {
	if !o.IsArray() {
		return "", false
	}
	s := string(o)
	inner := s[6 : len(s)-1]
	return OPSType(inner), true
}

// CreateArrayType 创建数组类型
func CreateArrayType(elementType OPSType) OPSType {
	return OPSType(fmt.Sprintf("Array<%s>", elementType))
}

// GetPtrElementType 获取指针指向的类型
func (o OPSType) GetPtrElementType() (OPSType, bool) {
	if !o.IsPtr() {
		return "", false
	}
	s := string(o)
	inner := s[4 : len(s)-1]
	return OPSType(inner), true
}

func (o OPSType) ToPtr() OPSType {
	return OPSType(fmt.Sprintf("Ptr<%s>", o))
}

// GetMapKeyValueTypes 获取Map的键和值类型
func (o OPSType) GetMapKeyValueTypes() (keyType, valueType OPSType, ok bool) {
	if !o.IsMap() {
		return "", "", false
	}
	s := string(o)
	inner := s[4 : len(s)-1]

	// 分割键值类型
	parts := splitByComma(inner)
	if len(parts) != 2 {
		return "", "", false
	}

	return OPSType(strings.TrimSpace(parts[0])), OPSType(strings.TrimSpace(parts[1])), true
}

// CreateMapType 创建Map类型
func CreateMapType(keyType, valueType OPSType) OPSType {
	return OPSType(fmt.Sprintf("Map<%s, %s>", keyType, valueType))
}

func (o OPSType) ReadFunc() (*FunctionType, bool) {
	s := string(o)
	if !strings.HasPrefix(s, "function(") {
		return nil, false
	}

	// 找到参数列表的开始和结束
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

	// 解析参数
	params, isVariadic := parseParams(paramsStr)

	// 解析返回值
	var returns OPSType = "Void"
	if returnsStr != "" {
		returns = parseReturnType(returnsStr)
	}

	return &FunctionType{
		Params:   params,
		Return:   returns,
		Variadic: isVariadic,
	}, true
}

type FunctionType struct {
	Params   []FunctionParam `json:"params,omitempty"`
	Return   OPSType         `json:"return"`
	Variadic bool            `json:"variadic,omitempty"`
}

type FunctionParam struct {
	Name Ident
	Type OPSType
}

type CallFunctionType struct {
	Params   []OPSType
	Returns  OPSType
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

func (c CallFunctionType) MiniType() OPSType {
	return OPSType(c.String())
}

// 解析参数列表字符串
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

		// 查找类型和参数名的分界
		// 类型在前，参数名在后（可选）
		// 例如: "Int a" 或 "Array<Int> items"
		// 参数名只能是标识符，不包含特殊字符

		// 反向查找：从末尾开始找到第一个标识符作为参数名
		// 其余部分作为类型
		partRunes := []rune(part)
		end := len(partRunes) - 1

		// 跳过末尾空格
		for end >= 0 && partRunes[end] == ' ' {
			end--
		}

		// 查找参数名的结束位置
		nameEnd := end
		for nameEnd >= 0 && isIdentChar(partRunes[nameEnd]) {
			nameEnd--
		}
		nameEnd++ // 调整到参数名的开始位置

		var paramName Ident
		var typeStr string

		if nameEnd <= end && nameEnd > 0 && partRunes[nameEnd-1] == ' ' {
			// 有参数名
			paramName = Ident(partRunes[nameEnd : end+1])
			typeStr = strings.TrimSpace(string(partRunes[:nameEnd-1]))
		} else {
			// 没有参数名，整个部分都是类型
			typeStr = strings.TrimSpace(part)
		}

		params = append(params, FunctionParam{
			Name: paramName,
			Type: OPSType(typeStr),
		})
	}

	return params, isVariadic
}

func isIdentChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') || ch == '_'
}

// 解析返回值类型字符串
func parseReturnType(returnStr string) OPSType {
	returnStr = strings.TrimSpace(returnStr)
	if returnStr == "" {
		return ""
	}

	// 如果返回值有括号，说明是多返回值
	if len(returnStr) > 0 && returnStr[0] == '(' {
		returnStr = strings.TrimSpace(returnStr[1 : len(returnStr)-1])

		// 解析多个返回类型
		var types []string
		typeParts := splitByComma(returnStr)

		for _, part := range typeParts {
			part = strings.TrimSpace(part)
			if part != "" {
				types = append(types, part)
			}
		}

		// 如果有多个返回值，包装成tuple(...)
		if len(types) > 1 {
			return OPSType("tuple(" + strings.Join(types, ", ") + ")")
		} else if len(types) == 1 {
			// 单个返回值，直接返回
			return OPSType(types[0])
		}
	}

	// 单个返回值，直接返回
	return OPSType(returnStr)
}

func (o OPSType) ReadTuple() ([]OPSType, bool) {
	s := string(o)
	if o.IsArray() {
		elemType, ok := o.ReadArrayItemType()
		if !ok {
			return nil, false
		}
		return []OPSType{elemType}, false
	}
	if !strings.HasPrefix(s, "tuple(") || !strings.HasSuffix(s, ")") {
		return nil, false
	}
	inner := s[6 : len(s)-1]
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return []OPSType{}, true // 空元组
	}

	var types []OPSType
	typeParts := splitByComma(inner)

	for _, part := range typeParts {
		part = strings.TrimSpace(part)
		if part != "" {
			types = append(types, OPSType(part))
		}
	}

	return types, true
}

// 按照最外层的逗号分割字符串，处理嵌套的函数类型和tuple类型
func splitByComma(s string) []string {
	var parts []string
	var current strings.Builder
	parenDepth := 0
	bracketDepth := 0
	angleDepth := 0 // 新增：处理尖括号 <>

	for _, ch := range s {
		switch ch {
		case '(':
			parenDepth++
			current.WriteRune(ch)
		case ')':
			parenDepth--
			current.WriteRune(ch)
		case '[':
			bracketDepth++
			current.WriteRune(ch)
		case ']':
			bracketDepth--
			current.WriteRune(ch)
		case '<':
			angleDepth++
			current.WriteRune(ch)
		case '>':
			angleDepth--
			current.WriteRune(ch)
		case ',':
			if parenDepth == 0 && bracketDepth == 0 && angleDepth == 0 {
				parts = append(parts, strings.TrimSpace(current.String()))
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	return parts
}

func (ft *FunctionType) ToCallFunctionType() CallFunctionType {
	var callParams []OPSType
	for _, param := range ft.Params {
		callParams = append(callParams, param.Type)
	}

	return CallFunctionType{
		Params:   callParams,
		Returns:  ft.Return,
		Variadic: ft.Variadic,
	}
}

func (fp *FunctionParam) ToCallFunctionType() (CallFunctionType, bool) {
	if fn, ok := fp.Type.ReadFunc(); ok {
		return fn.ToCallFunctionType(), true
	}
	return CallFunctionType{}, false
}

func (o OPSType) ReadCallFunc() (CallFunctionType, bool) {
	if fn, ok := o.ReadFunc(); ok {
		return fn.ToCallFunctionType(), true
	}
	return CallFunctionType{}, false
}

func CreateTupleType(types ...OPSType) OPSType {
	if len(types) == 0 {
		return "Void"
	}

	if len(types) == 1 {
		return types[0]
	}

	var typeStrs []string
	for _, t := range types {
		typeStrs = append(typeStrs, string(t))
	}

	return OPSType("tuple(" + strings.Join(typeStrs, ", ") + ")")
}

func (o OPSType) IsTuple() bool {
	s := string(o)
	return strings.HasPrefix(s, "tuple(") && strings.HasSuffix(s, ")")
}

func (o OPSType) Equals(other OPSType) bool {
	if o == other {
		return true
	}

	// Any 类型特殊处理：Any 可以匹配任何类型，但其他类型不能匹配 Any
	if o.IsAny() || other.IsAny() {
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

	if o.IsMap() && other.IsMap() {
		oKey, oVal, _ := o.GetMapKeyValueTypes()
		otherKey, otherVal, _ := other.GetMapKeyValueTypes()
		return oKey.Equals(otherKey) && oVal.Equals(otherVal)
	}

	fc, ok := o.ReadFunc()
	fc2, ok2 := other.ReadFunc()
	if ok == ok2 && ok {
		functionType := fc.ToCallFunctionType()
		callFunctionType := fc2.ToCallFunctionType()
		return functionType.String() == callFunctionType.String()
	}
	return string(o) == string(other)
}

func (o OPSType) StructName() (Ident, bool) {
	if strings.Contains(string(o), "(") || strings.Contains(string(o), ")") {
		return "", false
	}
	return Ident(o), true
}

func (o OPSType) IsPrimitive() bool {
	s := string(o)
	switch s {
	case "Any", "Void", "Error", "String", "Number", "Float", "Bool", "Byte":
		return true
	}
	return false
}

func (o OPSType) Resolve(v *ValidContext) OPSType {
	if o.IsEmpty() || o.IsPrimitive() {
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
	if o.IsMap() {
		k, val, _ := o.GetMapKeyValueTypes()
		return CreateMapType(k.Resolve(v), val.Resolve(v))
	}
	if o.IsTuple() {
		types, _ := o.ReadTuple()
		var r []OPSType
		for _, t := range types {
			r = append(r, t.Resolve(v))
		}
		return CreateTupleType(r...)
	}
	if readFunc, b := o.ReadFunc(); b {
		var newParams []FunctionParam
		for _, p := range readFunc.Params {
			newParams = append(newParams, FunctionParam{
				Name: p.Name,
				Type: p.Type.Resolve(v),
			})
		}
		newReturn := readFunc.Return.Resolve(v)
		newFunc := FunctionType{Params: newParams, Return: newReturn}
		return OPSType(newFunc.String())
	}

	s := string(o)
	if strings.Contains(s, ".") {
		parts := strings.SplitN(s, ".", 2)
		if realPkg, ok := v.root.Imports[parts[0]]; ok {
			return OPSType(fmt.Sprintf("%s.%s", realPkg, parts[1]))
		}
		return o // fallback
	}

	if v.root.Package != "" && v.root.Package != "main" {
		return OPSType(fmt.Sprintf("%s.%s", v.root.Package, s))
	}
	return o
}

func (o OPSType) Valid(v *ValidContext) bool {
	// Any 类型总是有效的
	if o.IsAny() {
		return true
	}

	if o.IsArray() {
		elemType, ok := o.ReadArrayItemType()
		if !ok {
			v.AddErrorf("invalid array type: %s", o)
			return false
		}
		return elemType.Valid(v)
	}
	if o.IsPtr() {
		elemType, ok := o.GetPtrElementType()
		if !ok {
			v.AddErrorf("invalid pointer type: %s", o)
			return false
		}
		return elemType.Valid(v)
	}
	if o.IsMap() {
		keyType, valueType, ok := o.GetMapKeyValueTypes()
		if !ok {
			v.AddErrorf("invalid map type: %s", o)
			return false
		}
		if !keyType.Valid(v) {
			v.AddErrorf("invalid map key type: %s", keyType)
			return false
		}
		if !valueType.Valid(v) {
			v.AddErrorf("invalid map value type: %s", valueType)
			return false
		}
		return true
	}
	readFunc, b := o.ReadFunc()
	if b {
		for _, param := range readFunc.Params {
			if param.Name != "" {
				if !param.Name.Valid(v) {
					v.AddErrorf("invalid parameter name: %s", param.Name)
					return false
				}
				if !param.Type.Valid(v) {
					v.AddErrorf("invalid parameter type: %s", param.Type)
					return false
				}
			}
		}
		if !readFunc.Return.Valid(v) {
			v.AddErrorf("invalid return type: %s", readFunc.Return)
			return false
		}
		return true
	}
	tuple, b := o.ReadTuple()
	if b {
		for _, param := range tuple {
			if !param.Valid(v) {
				v.AddErrorf("invalid tuple type: %s", param)
				return false
			}
		}
		return true
	}
	_, b = v.GetStruct(Ident(o))
	if !b && o != "Void" && !o.IsAny() && o != "Error" {
		v.AddErrorf("struct %s not found.", o)
		return false
	}
	return true
}

// 辅助方法：格式化为字符串
func (ft *FunctionType) String() string {
	var paramStrs []string
	for i, param := range ft.Params {
		prefix := ""
		if ft.Variadic && i == len(ft.Params)-1 {
			prefix = "..."
		}
		if param.Name == "" {
			paramStrs = append(paramStrs, prefix+string(param.Type))
		} else {
			// 格式为：类型 参数名
			paramStrs = append(paramStrs, fmt.Sprintf("%s%s %s", prefix, param.Type, param.Name))
		}
	}
	paramsStr := strings.Join(paramStrs, ", ")
	var returnStr string
	if ft.Return == "" {
		returnStr = "Void"
	} else {
		returnStr = " " + string(ft.Return)
	}

	return fmt.Sprintf("function(%s)%s", paramsStr, returnStr)
}

func (o OPSType) AutoPtr(pVar Expr) (Expr, bool) {
	varType := pVar.GetBase().Type

	// 如果目标类型是 Any，可以接受任何类型（包括指针）
	if o.IsAny() {
		if !varType.IsPtr() {
			// 不要让值类型参与运算
			originalID := pVar.GetBase().ID
			pVar.GetBase().ID = originalID + "_Operand_0"
			return &AddressExpr{
				BaseNode: BaseNode{
					ID:   originalID,
					Meta: "address",
					Type: varType.ToPtr(),
				},
				Operand: pVar,
			}, true
		}
		return pVar, true
	}

	if o.Equals(varType) {
		return pVar, true
	}

	// 数值字面量自动转换
	if (o == "Number" || o == "Float" || o == "Byte") && varType.IsPrimitive() {
		if lit, ok := pVar.(*LiteralExpr); ok {
			switch o {
			case "Number":
				val, _ := strconv.ParseInt(lit.Value, 10, 64)
				data := NewMiniNumber(val)
				lit.Type = "Number"
				lit.Data = &data
				return lit, true
			case "Float":
				val, _ := strconv.ParseFloat(lit.Value, 64)
				data := NewMiniFloat(val)
				lit.Type = "Float"
				lit.Data = &data
				return lit, true
			case "Byte":
				val, _ := strconv.ParseUint(lit.Value, 10, 8)
				data := NewMiniByte(byte(val))
				lit.Type = "Byte"
				lit.Data = &data
				return lit, true
			}
		}
	}

	if o.IsPtr() && !varType.IsPtr() {
		unPtrT, _ := o.GetPtrElementType()
		if !unPtrT.Equals(varType) {
			return nil, false
		}
		originalID := pVar.GetBase().ID
		pVar.GetBase().ID = originalID + "_Operand_0"
		return &AddressExpr{
			BaseNode: BaseNode{
				ID:   originalID,
				Meta: "address",
				Type: varType.ToPtr(),
			},
			Operand: pVar,
		}, true
	}
	return nil, false
}

// IsAssignableTo 判断当前类型是否可以赋值给目标类型
func (o OPSType) IsAssignableTo(target OPSType) bool {
	// Any 类型可以接受任何类型
	if target.IsAny() {
		return true
	}

	// 其他类型之间的赋值需要类型匹配
	return o.Equals(target)
}

// CanBeAny 判断类型是否可以被当作 Any 处理
func (o OPSType) CanBeAny() bool {
	// 所有非函数类型都可以当作 Any
	if o.IsVoid() {
		return false
	}

	if _, ok := o.ReadFunc(); ok {
		return false
	}

	return true
}
