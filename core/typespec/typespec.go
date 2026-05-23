package typespec

import (
	"fmt"
	"sort"
	"strings"
)

type Type string

const (
	Int64   Type = "Int64"
	Float64 Type = "Float64"
	String  Type = "String"
	Bool    Type = "Bool"
	Bytes   Type = "TypeBytes"
	Any     Type = "Any"
	Error   Type = "Error"
	Void    Type = "Void"
	Module  Type = "TypeModule"
	Closure Type = "TypeClosure"
)

type Param struct {
	Name string
	Type Type
}

type Function struct {
	Params   []Param
	Return   Type
	Variadic bool
}

type Method struct {
	Name string
	Sig  Function
}

type Member struct {
	Name string
	Type Type
}

type ParsedKind uint8

const (
	KindInvalid ParsedKind = iota
	KindVoid
	KindAny
	KindPrimitive
	KindNamed
	KindPtr
	KindHostRef
	KindArray
	KindMap
	KindTuple
	KindFunction
	KindStruct
	KindInterface
)

type Parsed struct {
	Kind     ParsedKind
	Raw      Type
	TypeID   string
	Elem     Type
	Key      Type
	Value    Type
	Items    []Type
	Function Function
	Fields   []Member
	Methods  []Method
}

func (t Type) String() string { return string(t) }
func (t Type) IsEmpty() bool  { return strings.TrimSpace(string(t)) == "" }
func (t Type) IsVoid() bool   { return t.IsEmpty() || t == Void }
func (t Type) IsAny() bool    { return t == Any || t == Module || t == Closure }
func (t Type) IsString() bool { return t == String }
func (t Type) IsInt() bool    { return t == Int64 }
func (t Type) IsBool() bool   { return t == Bool }
func (t Type) IsNumeric() bool {
	return t == Int64 || t == Float64
}

func (t Type) IsPrimitive() bool {
	return t.IsAny() || t.IsString() || t.IsNumeric() || t.IsBool() || t == Bytes || t == Error
}

func (t Type) IsPtr() bool {
	s := strings.TrimSpace(string(t))
	return strings.HasPrefix(s, "Ptr<") && strings.HasSuffix(s, ">")
}

func (t Type) IsHostRef() bool {
	s := strings.TrimSpace(string(t))
	return strings.HasPrefix(s, "HostRef<") && strings.HasSuffix(s, ">")
}

func (t Type) IsArray() bool {
	s := strings.TrimSpace(string(t))
	return strings.HasPrefix(s, "Array<") && strings.HasSuffix(s, ">")
}

func (t Type) IsMap() bool {
	s := strings.TrimSpace(string(t))
	return strings.HasPrefix(s, "Map<") && strings.HasSuffix(s, ">")
}

func (t Type) IsTuple() bool {
	s := strings.TrimSpace(string(t))
	return strings.HasPrefix(s, "tuple(") && strings.HasSuffix(s, ")")
}

func (t Type) IsFunction() bool {
	return strings.HasPrefix(strings.TrimSpace(string(t)), "function(")
}

func (t Type) IsStruct() bool {
	s := strings.TrimSpace(string(t))
	return strings.HasPrefix(s, "struct") && strings.Contains(s, "{") && strings.HasSuffix(s, "}")
}

func (t Type) IsInterface() bool {
	s := strings.TrimSpace(string(t))
	if !strings.HasPrefix(s, "interface{") || !strings.HasSuffix(s, "}") {
		return false
	}
	return strings.TrimSpace(s[len("interface{"):len(s)-1]) != ""
}

func Array(elem Type) Type { return Type(fmt.Sprintf("Array<%s>", elem)) }
func Map(key, value Type) Type {
	return Type(fmt.Sprintf("Map<%s, %s>", key, value))
}
func Ptr(elem Type) Type     { return Type(fmt.Sprintf("Ptr<%s>", elem)) }
func HostRef(elem Type) Type { return Type(fmt.Sprintf("HostRef<%s>", elem)) }

func Tuple(items ...Type) Type {
	if len(items) == 0 {
		return Void
	}
	if len(items) == 1 {
		return items[0]
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, string(item))
	}
	return Type("tuple(" + strings.Join(parts, ", ") + ")")
}

func Func(params []Param, ret Type, variadic bool) Type {
	if ret.IsEmpty() {
		ret = Void
	}
	parts := make([]string, 0, len(params))
	for i, param := range params {
		prefix := ""
		if variadic && i == len(params)-1 {
			prefix = "..."
		}
		parts = append(parts, prefix+string(param.Type))
	}
	return Type(fmt.Sprintf("function(%s) %s", strings.Join(parts, ", "), ret))
}

func Interface(methods []Method) Type {
	if len(methods) == 0 {
		return Any
	}
	parts := make([]string, 0, len(methods))
	for _, method := range methods {
		if method.Name == "" {
			continue
		}
		sig := strings.TrimPrefix(string(Func(method.Sig.Params, method.Sig.Return, method.Sig.Variadic)), "function")
		parts = append(parts, method.Name+sig+";")
	}
	return Type("interface{" + strings.Join(parts, "") + "}")
}

func Struct(members []Member) Type {
	if len(members) == 0 {
		return "struct {}"
	}
	parts := make([]string, 0, len(members))
	for _, member := range members {
		if member.Name == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s;", member.Name, member.Type))
	}
	return Type("struct { " + strings.Join(parts, " ") + " }")
}

func (t Type) Element() (Type, bool) {
	s := strings.TrimSpace(string(t))
	switch {
	case strings.HasPrefix(s, "Ptr<") && strings.HasSuffix(s, ">"):
		return Type(strings.TrimSpace(s[4 : len(s)-1])), true
	case strings.HasPrefix(s, "HostRef<") && strings.HasSuffix(s, ">"):
		return Type(strings.TrimSpace(s[8 : len(s)-1])), true
	case strings.HasPrefix(s, "Array<") && strings.HasSuffix(s, ">"):
		return Type(strings.TrimSpace(s[6 : len(s)-1])), true
	case strings.HasPrefix(s, "..."):
		return Type(strings.TrimSpace(s[3:])), true
	}
	return "", false
}

func (t Type) PtrElement() (Type, bool) {
	if !t.IsPtr() {
		return "", false
	}
	return t.Element()
}

func (t Type) HostRefElement() (Type, bool) {
	if !t.IsHostRef() {
		return "", false
	}
	return t.Element()
}

func (t Type) RefElement() (Type, bool) {
	if !t.IsPtr() && !t.IsHostRef() {
		return "", false
	}
	return t.Element()
}

func (t Type) ReadArrayItemType() (Type, bool) {
	if !t.IsArray() {
		return "", false
	}
	return t.Element()
}

func (t Type) MapTypes() (Type, Type, bool) {
	s := strings.TrimSpace(string(t))
	if !strings.HasPrefix(s, "Map<") || !strings.HasSuffix(s, ">") {
		return "", "", false
	}
	parts := SplitCommaAware(s[4 : len(s)-1])
	if len(parts) != 2 {
		return "", "", false
	}
	return Type(strings.TrimSpace(parts[0])), Type(strings.TrimSpace(parts[1])), true
}

func (t Type) TupleTypes() ([]Type, bool) {
	s := strings.TrimSpace(string(t))
	if !strings.HasPrefix(s, "tuple(") || !strings.HasSuffix(s, ")") {
		return nil, false
	}
	inner := strings.TrimSpace(s[6 : len(s)-1])
	if inner == "" {
		return []Type{}, true
	}
	parts := SplitCommaAware(inner)
	items := make([]Type, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			items = append(items, Type(part))
		}
	}
	return items, true
}

func (t Type) Function() (Function, bool) {
	s := strings.TrimSpace(string(t))
	if !strings.HasPrefix(s, "function(") {
		return Function{}, false
	}
	start := len("function(")
	pDepth, aDepth := 1, 0
	paramEnd := -1
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '(':
			pDepth++
		case ')':
			pDepth--
		case '<':
			aDepth++
		case '>':
			aDepth--
		}
		if pDepth == 0 && aDepth == 0 {
			paramEnd = i
			break
		}
	}
	if paramEnd == -1 {
		return Function{}, false
	}
	params, variadic := parseParams(s[start:paramEnd])
	ret := parseReturnType(strings.TrimSpace(s[paramEnd+1:]))
	if ret.IsEmpty() {
		ret = Void
	}
	return Function{Params: params, Return: ret, Variadic: variadic}, true
}

func (t Type) StructFields() ([]Member, bool) {
	if !t.IsStruct() {
		return nil, false
	}
	raw := strings.TrimSpace(string(t))
	start := strings.Index(raw, "{")
	inner := raw[start+1 : len(raw)-1]
	parts := SplitTopLevel(inner, ';')
	fields := make([]Member, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		items := strings.SplitN(part, " ", 2)
		if len(items) != 2 {
			return nil, false
		}
		fields = append(fields, Member{Name: strings.TrimSpace(items[0]), Type: Type(strings.TrimSpace(items[1]))})
	}
	return fields, true
}

func (t Type) InterfaceMethods() ([]Method, bool) {
	if !t.IsInterface() {
		return nil, false
	}
	raw := strings.TrimSpace(string(t))
	start := strings.Index(raw, "{")
	inner := raw[start+1 : len(raw)-1]
	if strings.TrimSpace(inner) == "" {
		return []Method{}, true
	}
	parts := SplitTopLevel(inner, ';')
	methods := make([]Method, 0, len(parts))
	for _, part := range parts {
		method := strings.TrimSpace(part)
		if method == "" {
			continue
		}
		name := method
		var sig Function
		if idx := strings.Index(method, "("); idx != -1 {
			name = strings.TrimSpace(method[:idx])
			pDepth, aDepth := 0, 0
			endIdx := -1
			for i := idx; i < len(method); i++ {
				switch method[i] {
				case '(':
					pDepth++
				case ')':
					pDepth--
				case '<':
					aDepth++
				case '>':
					aDepth--
				}
				if pDepth == 0 && aDepth == 0 {
					endIdx = i
					break
				}
			}
			if endIdx != -1 {
				fnStr := "function" + method[idx:endIdx+1]
				ret := strings.TrimSpace(method[endIdx+1:])
				if ret != "" {
					fnStr += " " + ret
				}
				if parsed, ok := Type(fnStr).Function(); ok {
					sig = parsed
				}
			}
		}
		if sig.Return.IsEmpty() {
			sig.Return = Any
		} else if sig.Return == Void {
			sig.Return = Any
		}
		methods = append(methods, Method{Name: name, Sig: sig})
	}
	return methods, true
}

func Parse[S ~string](spec S) (Parsed, error) {
	raw := Type(strings.TrimSpace(string(spec)))
	if raw.IsEmpty() || raw.IsVoid() {
		return Parsed{Kind: KindVoid, Raw: raw}, nil
	}
	if err := raw.ValidateCanonical(); err != nil {
		return Parsed{}, err
	}
	switch {
	case raw.IsAny():
		return Parsed{Kind: KindAny, Raw: raw, TypeID: CanonicalTypeID(raw.String())}, nil
	case raw.IsPrimitive():
		return Parsed{Kind: KindPrimitive, Raw: raw, TypeID: CanonicalTypeID(raw.String())}, nil
	case raw.IsPtr():
		elem, _ := raw.Element()
		return Parsed{Kind: KindPtr, Raw: raw, TypeID: CanonicalTypeID(elem.String()), Elem: elem}, nil
	case raw.IsHostRef():
		elem, _ := raw.Element()
		return Parsed{Kind: KindHostRef, Raw: raw, TypeID: CanonicalTypeID(elem.String()), Elem: elem}, nil
	case raw.IsArray():
		elem, _ := raw.Element()
		return Parsed{Kind: KindArray, Raw: raw, TypeID: CanonicalTypeID(elem.String()), Elem: elem}, nil
	case raw.IsMap():
		key, value, _ := raw.MapTypes()
		return Parsed{Kind: KindMap, Raw: raw, TypeID: CanonicalTypeID(value.String()), Key: key, Value: value}, nil
	case raw.IsTuple():
		items, _ := raw.TupleTypes()
		return Parsed{Kind: KindTuple, Raw: raw, TypeID: CanonicalTypeID(raw.String()), Items: items}, nil
	case raw.IsFunction():
		fn, _ := raw.Function()
		return Parsed{Kind: KindFunction, Raw: raw, TypeID: CanonicalTypeID(raw.String()), Function: fn}, nil
	case raw.IsStruct():
		fields, ok := raw.StructFields()
		if !ok {
			return Parsed{}, fmt.Errorf("invalid struct type: %s", raw)
		}
		return Parsed{Kind: KindStruct, Raw: raw, TypeID: CanonicalTypeID(raw.String()), Fields: fields}, nil
	case raw.IsInterface():
		methods, ok := raw.InterfaceMethods()
		if !ok {
			return Parsed{}, fmt.Errorf("invalid interface type: %s", raw)
		}
		return Parsed{Kind: KindInterface, Raw: raw, TypeID: CanonicalTypeID(raw.String()), Methods: methods}, nil
	default:
		return Parsed{Kind: KindNamed, Raw: raw, TypeID: CanonicalTypeID(raw.String())}, nil
	}
}

func CanonicalTypeID(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "*")
	if strings.HasPrefix(name, "Ptr<") && strings.HasSuffix(name, ">") {
		return CanonicalTypeID(name[4 : len(name)-1])
	}
	if strings.HasPrefix(name, "HostRef<") && strings.HasSuffix(name, ">") {
		return CanonicalTypeID(name[8 : len(name)-1])
	}
	return name
}

func (t Type) BaseName() Type {
	if elem, ok := t.PtrElement(); ok {
		return elem.BaseName()
	}
	if elem, ok := t.HostRefElement(); ok {
		return elem.BaseName()
	}
	if elem, ok := t.ReadArrayItemType(); ok {
		return elem.BaseName()
	}
	if _, value, ok := t.MapTypes(); ok {
		return value.BaseName()
	}
	return t
}

func (t Type) ValidateCanonical() error {
	if t.IsCanonical() {
		return nil
	}
	return fmt.Errorf("non-canonical type: %s", t)
}

func (t Type) IsCanonical() bool {
	switch t {
	case Int64, Float64, String, Bool, Bytes, Any, Error, Void, Module, Closure:
		return true
	}
	if t.IsTuple() {
		items, ok := t.TupleTypes()
		if !ok {
			return false
		}
		for _, item := range items {
			if !item.IsCanonical() {
				return false
			}
		}
		return true
	}
	if t.IsPtr() || t.IsHostRef() || t.IsArray() {
		elem, ok := t.Element()
		return ok && elem.IsCanonical()
	}
	if t.IsMap() {
		key, value, ok := t.MapTypes()
		return ok && key.IsCanonical() && value.IsCanonical()
	}
	if t.IsStruct() {
		fields, ok := t.StructFields()
		if !ok {
			return false
		}
		for _, field := range fields {
			if !field.Type.IsCanonical() {
				return false
			}
		}
		return true
	}
	if t.IsInterface() {
		methods, ok := t.InterfaceMethods()
		if !ok || len(methods) == 0 {
			return false
		}
		for _, method := range methods {
			if method.Name == "" || !method.Sig.Return.IsCanonical() {
				return false
			}
			for _, param := range method.Sig.Params {
				if !param.Type.IsCanonical() {
					return false
				}
			}
		}
		return true
	}
	if fn, ok := t.Function(); ok {
		if !fn.Return.IsCanonical() {
			return false
		}
		for _, param := range fn.Params {
			if !param.Type.IsCanonical() {
				return false
			}
		}
		return true
	}
	return isCanonicalNamedType(t)
}

func (t Type) Equals(other Type) bool {
	if t == other || t.IsAny() || other.IsAny() {
		return true
	}
	if t.IsArray() && other.IsArray() {
		elem, _ := t.Element()
		otherElem, _ := other.Element()
		return elem.Equals(otherElem)
	}
	if t.IsPtr() && other.IsPtr() {
		elem, _ := t.Element()
		otherElem, _ := other.Element()
		return elem.Equals(otherElem)
	}
	if t.IsHostRef() && other.IsHostRef() {
		elem, _ := t.Element()
		otherElem, _ := other.Element()
		return elem.Equals(otherElem)
	}
	if t.IsTuple() && other.IsTuple() {
		items, _ := t.TupleTypes()
		otherItems, _ := other.TupleTypes()
		if len(items) != len(otherItems) {
			return false
		}
		for i := range items {
			if !items[i].Equals(otherItems[i]) {
				return false
			}
		}
		return true
	}
	return t == other
}

func (t Type) IsAssignableTo(target Type) bool {
	return t.isAssignableToRecursive(target, 0, 256)
}

func (t Type) IsAssignableToWithMaxDepth(target Type, maxDepth int) bool {
	if maxDepth <= 0 {
		maxDepth = 256
	}
	return t.isAssignableToRecursive(target, 0, maxDepth)
}

func (t Type) isAssignableToRecursive(target Type, depth, maxDepth int) bool {
	if depth > maxDepth {
		return false
	}
	if target.IsAny() || t.IsAny() || t == "Constant" {
		return true
	}
	if t.Equals(target) {
		return true
	}
	if target.IsString() && t == Error {
		return true
	}
	if target.IsNumeric() && t.IsNumeric() {
		return true
	}
	if t.IsMap() && !target.IsPrimitive() && !target.IsArray() && !target.IsMap() && !target.IsPtr() && !target.IsHostRef() && !target.IsInterface() {
		return true
	}
	if target.IsInterface() {
		if t.IsInterface() {
			sourceMethods, _ := t.InterfaceMethods()
			targetMethods, _ := target.InterfaceMethods()
			source := make(map[string]struct{}, len(sourceMethods))
			for _, method := range sourceMethods {
				source[method.Name] = struct{}{}
			}
			for _, method := range targetMethods {
				if _, ok := source[method.Name]; !ok {
					return false
				}
			}
			return true
		}
		return true
	}
	if target.IsPtr() && t.IsHostRef() {
		targetElem, _ := target.Element()
		hostElem, _ := t.Element()
		return hostElem.Equals(targetElem)
	}
	if t.IsHostRef() || target.IsHostRef() {
		return t.Equals(target)
	}
	if target.IsPtr() && !t.IsPtr() {
		elem, _ := target.Element()
		if elem.isAssignableToRecursive(t, depth+1, maxDepth) {
			return true
		}
	}
	if t.IsPtr() && !target.IsPtr() {
		elem, _ := t.Element()
		if elem.isAssignableToRecursive(target, depth+1, maxDepth) {
			return true
		}
	}
	return t.Equals(target)
}

func (t Type) ZeroValue() interface{} {
	if t.IsPtr() || t.IsHostRef() || t.IsArray() || t.IsMap() || t.IsAny() || t == Bytes {
		return nil
	}
	switch t {
	case Int64:
		return int64(0)
	case Float64:
		return 0.0
	case String:
		return ""
	case Bool:
		return false
	}
	return nil
}

func SplitCommaAware(s string) []string {
	return SplitTopLevel(s, ',')
}

func SplitTopLevel(s string, sep rune) []string {
	var parts []string
	var current strings.Builder
	pDepth, bDepth, aDepth, cDepth := 0, 0, 0, 0
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
		case '{':
			cDepth++
		case '}':
			cDepth--
		default:
			if ch == sep && pDepth == 0 && bDepth == 0 && aDepth == 0 && cDepth == 0 {
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

func WalkNamedTypes(t Type, visit func(Type)) {
	if visit == nil {
		return
	}
	if parsed, err := Parse(t); err == nil {
		switch parsed.Kind {
		case KindPtr, KindHostRef, KindArray:
			WalkNamedTypes(parsed.Elem, visit)
			return
		case KindMap:
			WalkNamedTypes(parsed.Key, visit)
			WalkNamedTypes(parsed.Value, visit)
			return
		case KindTuple:
			for _, item := range parsed.Items {
				WalkNamedTypes(item, visit)
			}
			return
		case KindFunction:
			for _, param := range parsed.Function.Params {
				WalkNamedTypes(param.Type, visit)
			}
			WalkNamedTypes(parsed.Function.Return, visit)
			return
		case KindStruct:
			for _, field := range parsed.Fields {
				WalkNamedTypes(field.Type, visit)
			}
			return
		case KindInterface:
			for _, method := range parsed.Methods {
				for _, param := range method.Sig.Params {
					WalkNamedTypes(param.Type, visit)
				}
				WalkNamedTypes(method.Sig.Return, visit)
			}
			return
		case KindNamed:
			visit(parsed.Raw)
			return
		}
		return
	}
	if isCanonicalNamedType(t) {
		visit(t)
	}
}

func SortedMethods(methods map[string]Function) []Method {
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	res := make([]Method, 0, len(names))
	for _, name := range names {
		res = append(res, Method{Name: name, Sig: methods[name]})
	}
	return res
}

func parseParams(paramsStr string) ([]Param, bool) {
	paramsStr = strings.TrimSpace(paramsStr)
	if paramsStr == "" {
		return nil, false
	}
	variadic := false
	parts := SplitCommaAware(paramsStr)
	params := make([]Param, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "...") {
			variadic = true
			part = strings.TrimPrefix(part, "...")
		}
		if _, ok := Type(part).Function(); ok {
			params = append(params, Param{Type: Type(part)})
			continue
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
		var name, typ string
		if nameEnd <= end && nameEnd > 0 && partRunes[nameEnd-1] == ' ' {
			name = string(partRunes[nameEnd : end+1])
			typ = strings.TrimSpace(string(partRunes[:nameEnd-1]))
		} else {
			typ = strings.TrimSpace(part)
		}
		params = append(params, Param{Name: name, Type: Type(typ)})
	}
	return params, variadic
}

func parseReturnType(returnStr string) Type {
	returnStr = strings.TrimSpace(returnStr)
	if returnStr == "" {
		return Void
	}
	if strings.HasPrefix(returnStr, "(") && strings.HasSuffix(returnStr, ")") {
		inner := strings.TrimSpace(returnStr[1 : len(returnStr)-1])
		parts := SplitCommaAware(inner)
		items := make([]Type, 0, len(parts))
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				items = append(items, Type(part))
			}
		}
		return Tuple(items...)
	}
	return Type(returnStr)
}

func isIdentChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func isCanonicalNamedType(t Type) bool {
	s := string(t)
	if s == "" {
		return false
	}
	switch s {
	case "any", "interface{}", "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float", "float32", "float64", "complex64", "complex128",
		"bool", "byte", "rune", "error", "void",
		"Int", "Int8", "Int16", "Int32", "Float", "Float32",
		"Uint", "Uint8", "Uint16", "Uint32", "Uint64", "Byte":
		return false
	}
	if strings.ContainsAny(s, "[]*") {
		return false
	}
	if strings.ContainsAny(s, "<>(),;{} ") {
		return false
	}
	if strings.Contains(s, "interface{}") {
		return false
	}
	if strings.Contains(s, "/") {
		return isCanonicalModulePathType(s)
	}
	return true
}

func isCanonicalModulePathType(s string) bool {
	if strings.Contains(s, "://") {
		return false
	}
	lastDot := strings.LastIndex(s, ".")
	if lastDot <= 0 || lastDot == len(s)-1 {
		return false
	}
	path := s[:lastDot]
	typeName := s[lastDot+1:]
	if strings.Contains(typeName, "/") || typeName == "" {
		return false
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || strings.Contains(seg, ".") {
			return false
		}
		for _, ch := range seg {
			if !isIdentChar(ch) && ch != '-' {
				return false
			}
		}
	}
	for _, ch := range typeName {
		if !isIdentChar(ch) {
			return false
		}
	}
	return true
}
