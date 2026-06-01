package jsonlib

import (
	"gopkg.d7z.net/go-mini/core/surface"
)

func Surface() *surface.Bundle {
	return surface.Library("encoding/json", surface.GoFile("json.mgo", jsonSource))
}

const jsonSource = `
package json

import "fmt"
import "reflect"
import "sort"
import "strconv"
import "strings"

type parser struct {
	text string
	pos int
}

type jsonNumber struct {
	Raw string
	Float float64
}

func Marshal(v any) ([]byte, error) {
	text, err := marshalValue(v, 0)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func Decode(data []byte) (any, error) {
	value, err := decodeRaw(data)
	if err != nil {
		return nil, err
	}
	return materializeDecodedValue(value), nil
}

func decodeRaw(data []byte) (any, error) {
	p := &parser{text: string(data)}
	value, err := parseValue(p)
	if err != nil {
		return nil, err
	}
	skipSpace(p)
	if p.pos != len(p.text) {
		return nil, fmt.Errorf("json: unexpected trailing data at byte %d", p.pos)
	}
	return value, nil
}

func Unmarshal(data []byte, out any) error {
	decoded, err := decodeRaw(data)
	if err != nil {
		return err
	}
	target := reflect.TypeOf(out)
	if target.Kind() != reflect.Ptr {
		return fmt.Errorf("json: Unmarshal target must be pointer, got %s", target.String())
	}
	elem := target.Elem()
	if elem.String() == "" {
		return fmt.Errorf("json: invalid Unmarshal target element for %s (%s kind %d)", target.String(), target.String(), target.Kind())
	}
	value, err := convertDecodedValue(elem, decoded)
	if err != nil {
		return err
	}
	return reflect.Assign(out, value)
}

func marshalValue(v any, depth int64) (string, error) {
	if depth > 64 {
		return "", fmt.Errorf("json: unsupported cyclic or too deeply nested value")
	}
	switch x := v.(type) {
	case nil:
		return "null", nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case byte:
		return strconv.FormatInt(Int64(x), 10), nil
	case rune:
		return strconv.FormatInt(Int64(x), 10), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		text := strconv.FormatFloat(x, 'g', -1, 64)
		if text == "NaN" || text == "+Inf" || text == "-Inf" {
			return "", fmt.Errorf("json: unsupported float value %s", text)
		}
		return text, nil
	case string:
		return strconv.Quote(x), nil
	}

	if reflect.KindOf(v) == reflect.Array && isByteArrayType(reflect.TypeOf(v)) {
		return marshalBytes(v)
	}
	switch reflect.KindOf(v) {
	case reflect.Array:
		return marshalArray(v, depth+1)
	case reflect.Map:
		return marshalMap(v, depth+1)
	case reflect.Struct:
		return marshalStruct(v, depth+1)
	case reflect.Ptr:
		return "", fmt.Errorf("json: pointer values are not supported")
	case reflect.HostRef:
		return "", fmt.Errorf("json: host references are not supported")
	case reflect.Chan:
		return "", fmt.Errorf("json: channel values are not supported")
	case reflect.Func:
		return "", fmt.Errorf("json: function values are not supported")
	case reflect.Interface:
		return "", fmt.Errorf("json: typed interface values are not supported")
	case reflect.Error:
		return "", fmt.Errorf("json: error values are not supported")
	case reflect.Module:
		return "", fmt.Errorf("json: module values are not supported")
	default:
		return "", fmt.Errorf("json: unsupported value kind %d", reflect.KindOf(v))
	}
}

func marshalBytes(v any) (string, error) {
	buf := []byte("")
	for i := 0; i < reflect.Len(v); i++ {
		item, ok := reflect.Index(v, i)
		if !ok {
			return "", fmt.Errorf("json: bytes index %d out of range", i)
		}
		b, ok := item.(byte)
		if ok {
			buf = append(buf, b)
			continue
		}
		return "", fmt.Errorf("json: bytes item %d is not Byte", i)
	}
	return strconv.Quote(string(buf)), nil
}

func isByteArrayType(target reflect.Type) bool {
	return target.Kind() == reflect.Array && target.Elem().String() == "Byte"
}

func marshalArray(v any, depth int64) (string, error) {
	parts := []string{}
	for i := 0; i < reflect.Len(v); i++ {
		item, ok := reflect.Index(v, i)
		if !ok {
			return "", fmt.Errorf("json: array index %d out of range", i)
		}
		text, err := marshalValue(item, depth)
		if err != nil {
			return "", err
		}
		parts = append(parts, text)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

func marshalMap(v any, depth int64) (string, error) {
	typ := reflect.TypeOf(v)
	if typ.Key().Kind() != reflect.String {
		return "", fmt.Errorf("json: map key type %s is not supported", typ.Key().String())
	}
	rawKeys, ok := reflect.MapKeys(v)
	if !ok {
		return "", fmt.Errorf("json: map keys are not accessible")
	}
	keys := []string{}
	for _, item := range rawKeys {
		key, ok := item.(string)
		if !ok {
			return "", fmt.Errorf("json: map key type %s is not supported", typ.Key().String())
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, key := range keys {
		item, ok := reflect.MapIndex(v, key)
		if !ok {
			continue
		}
		text, err := marshalValue(item, depth)
		if err != nil {
			return "", err
		}
		parts = append(parts, strconv.Quote(key)+":"+text)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func marshalStruct(v any, depth int64) (string, error) {
	fields := reflect.Fields(v)
	parts := []string{}
	for _, field := range fields {
		name, include := fieldName(field)
		if !include {
			continue
		}
		item, ok := reflect.Field(v, field.Name)
		if !ok {
			return "", fmt.Errorf("json: field %s is not accessible", field.Name)
		}
		text, err := marshalValue(item, depth)
		if err != nil {
			return "", fmt.Errorf("json: field %s: %w", field.Name, err)
		}
		parts = append(parts, strconv.Quote(name)+":"+text)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func parseValue(p *parser) (any, error) {
	skipSpace(p)
	if p.pos >= len(p.text) {
		return nil, fmt.Errorf("json: unexpected end of input")
	}
	switch p.text[p.pos] {
	case '{':
		return parseObject(p)
	case '[':
		return parseArray(p)
	case '"':
		return parseString(p)
	case 't':
		if hasToken(p, "true") {
			p.pos += 4
			return true, nil
		}
	case 'f':
		if hasToken(p, "false") {
			p.pos += 5
			return false, nil
		}
	case 'n':
		if hasToken(p, "null") {
			p.pos += 4
			return nil, nil
		}
	default:
		if p.text[p.pos] == '-' || isDigit(p.text[p.pos]) {
			return parseNumber(p)
		}
	}
	return nil, fmt.Errorf("json: unexpected character %q at byte %d", p.text[p.pos], p.pos)
}

func parseObject(p *parser) (any, error) {
	p.pos++
	skipSpace(p)
	obj := map[string]any{}
	if p.pos < len(p.text) && p.text[p.pos] == '}' {
		p.pos++
		return obj, nil
	}
	for {
		key, err := parseString(p)
		if err != nil {
			return nil, err
		}
		skipSpace(p)
		if p.pos >= len(p.text) || p.text[p.pos] != ':' {
			return nil, fmt.Errorf("json: expected ':' at byte %d", p.pos)
		}
		p.pos++
		value, err := parseValue(p)
		if err != nil {
			return nil, err
		}
		obj[key] = value
		skipSpace(p)
		if p.pos >= len(p.text) {
			return nil, fmt.Errorf("json: unexpected end of object")
		}
		if p.text[p.pos] == '}' {
			p.pos++
			return obj, nil
		}
		if p.text[p.pos] != ',' {
			return nil, fmt.Errorf("json: expected ',' at byte %d", p.pos)
		}
		p.pos++
		skipSpace(p)
	}
	return nil, fmt.Errorf("json: invalid object state")
}

func parseArray(p *parser) (any, error) {
	p.pos++
	skipSpace(p)
	items := []any{}
	if p.pos < len(p.text) && p.text[p.pos] == ']' {
		p.pos++
		return items, nil
	}
	for {
		value, err := parseValue(p)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
		skipSpace(p)
		if p.pos >= len(p.text) {
			return nil, fmt.Errorf("json: unexpected end of array")
		}
		if p.text[p.pos] == ']' {
			p.pos++
			return items, nil
		}
		if p.text[p.pos] != ',' {
			return nil, fmt.Errorf("json: expected ',' at byte %d", p.pos)
		}
		p.pos++
		skipSpace(p)
	}
	return nil, fmt.Errorf("json: invalid array state")
}

func parseString(p *parser) (string, error) {
	if p.pos >= len(p.text) || p.text[p.pos] != '"' {
		return "", fmt.Errorf("json: expected string at byte %d", p.pos)
	}
	p.pos++
	out := ""
	for p.pos < len(p.text) {
		ch := p.text[p.pos]
		if ch == '\\' {
			p.pos++
			if p.pos >= len(p.text) {
				return "", fmt.Errorf("json: invalid escape at byte %d", p.pos)
			}
			esc := p.text[p.pos]
			switch esc {
			case '"':
				out += "\""
				p.pos++
			case '\\':
				out += "\\"
				p.pos++
			case '/':
				out += "/"
				p.pos++
			case 'b':
				out += "\b"
				p.pos++
			case 'f':
				out += "\f"
				p.pos++
			case 'n':
				out += "\n"
				p.pos++
			case 'r':
				out += "\r"
				p.pos++
			case 't':
				out += "\t"
				p.pos++
			case 'u':
				cp, ok := readUnicodeEscape(p.text, p.pos)
				if !ok {
					return "", fmt.Errorf("json: invalid unicode escape at byte %d", p.pos-1)
				}
				p.pos += 5
				if cp >= 55296 && cp <= 56319 {
					if p.pos+5 >= len(p.text) || p.text[p.pos] != '\\' || p.text[p.pos+1] != 'u' {
						return "", fmt.Errorf("json: invalid unicode surrogate at byte %d", p.pos)
					}
					low, lowOK := readUnicodeEscape(p.text, p.pos+1)
					if !lowOK || low < 56320 || low > 57343 {
						return "", fmt.Errorf("json: invalid unicode surrogate at byte %d", p.pos)
					}
					cp = 65536 + (cp-55296)*1024 + (low-56320)
					p.pos += 6
				} else if cp >= 56320 && cp <= 57343 {
					return "", fmt.Errorf("json: invalid unicode surrogate at byte %d", p.pos-5)
				}
				out += codePointString(cp)
			default:
				return "", fmt.Errorf("json: invalid escape %q at byte %d", esc, p.pos)
			}
			continue
		}
		if ch == '"' {
			p.pos++
			return out, nil
		}
		if ch < 32 {
			return "", fmt.Errorf("json: invalid control character at byte %d", p.pos)
		}
		out += p.text[p.pos:p.pos+1]
		p.pos++
	}
	return "", fmt.Errorf("json: unterminated string")
}

func parseNumber(p *parser) (any, error) {
	start := p.pos
	if p.text[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.text) {
		return nil, fmt.Errorf("json: invalid number at byte %d", start)
	}
	if p.text[p.pos] == '0' {
		p.pos++
	} else {
		if !isDigit(p.text[p.pos]) {
			return nil, fmt.Errorf("json: invalid number at byte %d", start)
		}
		for p.pos < len(p.text) && isDigit(p.text[p.pos]) {
			p.pos++
		}
	}
	if p.pos < len(p.text) && p.text[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.text) || !isDigit(p.text[p.pos]) {
			return nil, fmt.Errorf("json: invalid number at byte %d", start)
		}
		for p.pos < len(p.text) && isDigit(p.text[p.pos]) {
			p.pos++
		}
	}
	if p.pos < len(p.text) && (p.text[p.pos] == 'e' || p.text[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.text) && (p.text[p.pos] == '+' || p.text[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.text) || !isDigit(p.text[p.pos]) {
			return nil, fmt.Errorf("json: invalid number at byte %d", start)
		}
		for p.pos < len(p.text) && isDigit(p.text[p.pos]) {
			p.pos++
		}
	}
	raw := p.text[start:p.pos]
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, err
	}
	return jsonNumber{Raw: raw, Float: value}, nil
}

func readUnicodeEscape(text string, pos int) (int64, bool) {
	if pos+4 >= len(text) || text[pos] != 'u' {
		return 0, false
	}
	value := int64(0)
	for i := pos + 1; i < pos + 5; i++ {
		digit, ok := hexValue(text[i])
		if !ok {
			return 0, false
		}
		value = value*16 + digit
	}
	return value, true
}

func hexValue(ch int64) (int64, bool) {
	if ch >= '0' && ch <= '9' {
		return ch - '0', true
	}
	if ch >= 'a' && ch <= 'f' {
		return ch - 'a' + 10, true
	}
	if ch >= 'A' && ch <= 'F' {
		return ch - 'A' + 10, true
	}
	return 0, false
}

func codePointString(cp int64) string {
	if cp < 0 || cp > 1114111 || (cp >= 55296 && cp <= 57343) {
		cp = 65533
	}
	buf := []byte("")
	if cp <= 127 {
		buf = append(buf, cp)
		return string(buf)
	}
	if cp <= 2047 {
		buf = append(buf, 192+cp/64)
		buf = append(buf, 128+cp%64)
		return string(buf)
	}
	if cp <= 65535 {
		buf = append(buf, 224+cp/4096)
		buf = append(buf, 128+(cp/64)%64)
		buf = append(buf, 128+cp%64)
		return string(buf)
	}
	buf = append(buf, 240+cp/262144)
	buf = append(buf, 128+(cp/4096)%64)
	buf = append(buf, 128+(cp/64)%64)
	buf = append(buf, 128+cp%64)
	return string(buf)
}

func skipSpace(p *parser) {
	for p.pos < len(p.text) {
		switch p.text[p.pos] {
		case ' ', '\n', '\r', '\t':
			p.pos++
		default:
			return
		}
	}
}

func hasToken(p *parser, token string) bool {
	end := p.pos + len(token)
	return end <= len(p.text) && p.text[p.pos:end] == token
}

func isDigit(ch int64) bool {
	return ch >= '0' && ch <= '9'
}

func convertDecodedValue(target reflect.Type, value any) (any, error) {
	if target.String() == "" {
		return nil, fmt.Errorf("json: invalid Unmarshal target type")
	}
	if err := validateJSONType(target); err != nil {
		return nil, err
	}
	return convertValue(target, value)
}

func convertValue(target reflect.Type, value any) (any, error) {
	value = unwrapDecodedValue(value)
	switch target.Kind() {
	case reflect.Any:
		return materializeDecodedValue(value), nil
	case reflect.Bool:
		boolValue, boolOK := value.(bool)
		if !boolOK {
			return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
		}
		return boolValue, nil
	case reflect.Int64, reflect.Byte, reflect.Rune:
		return decodeInt64(value, target.String())
	case reflect.Float64:
		return decodeFloat64(value, target.String())
	case reflect.String:
		stringValue, stringOK := value.(string)
		if !stringOK {
			return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
		}
		return stringValue, nil
	case reflect.Array:
		if isByteArrayType(target) {
			bytesString, bytesStringOK := value.(string)
			if !bytesStringOK {
				return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
			}
			return []byte(bytesString), nil
		}
		if reflect.KindOf(value) != reflect.Array {
			return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
		}
		outArray := reflect.Zero(target.String())
		for i := 0; i < reflect.Len(value); i++ {
			item, ok := reflect.Index(value, i)
			if !ok {
				return nil, fmt.Errorf("json: array index %d out of range", i)
			}
			converted, err := convertValue(target.Elem(), item)
			if err != nil {
				return nil, err
			}
			if err := reflect.Append(outArray, converted); err != nil {
				return nil, err
			}
		}
		return outArray, nil
	case reflect.Map:
		if reflect.KindOf(value) != reflect.Map {
			return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
		}
		if target.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("json: map key type %s is not supported", target.Key().String())
		}
		outMap, madeOK := reflect.MakeMap(target.String())
		if !madeOK {
			return nil, fmt.Errorf("json: cannot create map %s", target.String())
		}
		keys, keysOK := reflect.MapKeys(value)
		if !keysOK {
			return nil, fmt.Errorf("json: cannot read object keys")
		}
		for _, rawKey := range keys {
			key, keyOK := rawKey.(string)
			if !keyOK {
				return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
			}
			item, itemOK := reflect.MapIndex(value, key)
			if !itemOK {
				continue
			}
			converted, err := convertValue(target.Elem(), item)
			if err != nil {
				return nil, err
			}
			if err := reflect.SetMapIndex(outMap, key, converted); err != nil {
				return nil, err
			}
		}
		return outMap, nil
	case reflect.Struct:
		if reflect.KindOf(value) != reflect.Map {
			return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
		}
		outStruct := reflect.Zero(target.String())
		for _, field := range reflect.FieldsOfType(target) {
			name, include := fieldName(field)
			if !include {
				continue
			}
			item, itemOK := reflect.MapIndex(value, name)
			if !itemOK {
				continue
			}
			fieldValue, fieldOK := reflect.Field(outStruct, field.Name)
			if !fieldOK {
				return nil, fmt.Errorf("json: field %s is not accessible", field.Name)
			}
			fieldType := field.Type
			if fieldType.String() == "" {
				fieldType = reflect.TypeOf(fieldValue)
			}
			converted, err := convertValue(fieldType, item)
			if err != nil {
				return nil, fmt.Errorf("json: field %s: %w", field.Name, err)
			}
			if err := reflect.SetField(outStruct, field.Name, converted); err != nil {
				return nil, err
			}
		}
		return outStruct, nil
	default:
		return nil, fmt.Errorf("json: unsupported target type %s", target.String())
	}
}

func unwrapDecodedValue(value any) any {
	inner, ok := reflect.Unwrap(value)
	if ok {
		return inner
	}
	return value
}

func decodeInt64(value any, target string) (int64, error) {
	value = unwrapDecodedValue(value)
	if number, ok := decodedNumber(value); ok {
		if strings.Index(number.Raw, ".") >= 0 || strings.Index(number.Raw, "e") >= 0 || strings.Index(number.Raw, "E") >= 0 {
			return 0, fmt.Errorf("json: cannot decode non-integer number into %s", target)
		}
		out, err := strconv.ParseInt(number.Raw, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("json: cannot decode number into %s: %w", target, err)
		}
		return out, nil
	}
	if numberValue, ok := value.(float64); ok {
		intValue := int64(numberValue)
		if float64(intValue) != numberValue {
			return 0, fmt.Errorf("json: cannot decode non-integer number into %s", target)
		}
		return intValue, nil
	}
	return 0, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target)
}

func decodeFloat64(value any, target string) (float64, error) {
	value = unwrapDecodedValue(value)
	if number, ok := decodedNumber(value); ok {
		return number.Float, nil
	}
	if floatValue, ok := value.(float64); ok {
		return floatValue, nil
	}
	return 0, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target)
}

func materializeDecodedValue(value any) any {
	value = unwrapDecodedValue(value)
	if number, ok := decodedNumber(value); ok {
		return number.Float
	}
	switch reflect.KindOf(value) {
	case reflect.Array:
		arrOut := []any{}
		for i := 0; i < reflect.Len(value); i++ {
			item, ok := reflect.Index(value, i)
			if ok {
				arrOut = append(arrOut, materializeDecodedValue(item))
			}
		}
		return arrOut
	case reflect.Map:
		mapOut := map[string]any{}
		keys, ok := reflect.MapKeys(value)
		if !ok {
			return mapOut
		}
		for _, rawKey := range keys {
			key, ok := rawKey.(string)
			if !ok {
				continue
			}
			item, ok := reflect.MapIndex(value, key)
			if ok {
				mapOut[key] = materializeDecodedValue(item)
			}
		}
		return mapOut
	default:
		return value
	}
}

func decodedNumber(value any) (jsonNumber, bool) {
	if reflect.TypeOf(value).String() != "encoding/json.jsonNumber" {
		return jsonNumber{}, false
	}
	rawValue, rawOK := reflect.Field(value, "Raw")
	floatValue, floatOK := reflect.Field(value, "Float")
	if !rawOK || !floatOK {
		return jsonNumber{}, false
	}
	raw, rawOK := rawValue.(string)
	floatNumber, floatOK := floatValue.(float64)
	if !rawOK || !floatOK {
		return jsonNumber{}, false
	}
	return jsonNumber{Raw: raw, Float: floatNumber}, true
}

func validateJSONType(target reflect.Type) error {
	switch target.Kind() {
	case reflect.Any, reflect.Bool, reflect.Int64, reflect.Byte, reflect.Rune, reflect.Float64, reflect.String:
		return nil
	case reflect.Array:
		return validateJSONType(target.Elem())
	case reflect.Map:
		if target.Key().Kind() != reflect.String {
			return fmt.Errorf("json: map key type %s is not supported", target.Key().String())
		}
		return validateJSONType(target.Elem())
	case reflect.Struct:
		for _, field := range reflect.FieldsOfType(target) {
			if _, include := fieldName(field); !include {
				continue
			}
			fieldType := field.Type
			if fieldType.String() == "" {
				return fmt.Errorf("json: field %s has invalid type metadata", field.Name)
			}
			if err := validateJSONType(fieldType); err != nil {
				return fmt.Errorf("json: field %s: %w", field.Name, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("json: unsupported target type %s", target.String())
	}
}

func fieldName(field reflect.StructField) (string, bool) {
	tag := strings.TrimSpace(field.Tag)
	for tag != "" {
		colon := strings.Index(tag, ":")
		if colon <= 0 || colon+1 >= len(tag) || tag[colon+1] != '"' {
			break
		}
		name := tag[:colon]
		tag = tag[colon+2:]
		end := 0
		escaped := false
		for end < len(tag) {
			ch := tag[end]
			if escaped {
				escaped = false
				end++
				continue
			}
			if ch == '\\' {
				escaped = true
				end++
				continue
			}
			if ch == '"' {
				break
			}
			end++
		}
		if end >= len(tag) {
			break
		}
		value := tag[:end]
		tag = strings.TrimSpace(tag[end+1:])
		if name != "json" {
			continue
		}
		if comma := strings.Index(value, ","); comma >= 0 {
			value = value[:comma]
		}
		if value == "-" {
			return "", false
		}
		if value != "" {
			return value, true
		}
		break
	}
	return field.Name, true
}

func jsonValueName(value any) string {
	value = unwrapDecodedValue(value)
	if value == nil {
		return "null"
	}
	switch value.(type) {
	case bool:
		return "bool"
	case jsonNumber:
		return "number"
	case float64:
		return "number"
	case string:
		return "string"
	}
	switch reflect.KindOf(value) {
	case reflect.Array:
		return "array"
	case reflect.Map:
		return "object"
	default:
		return "value"
	}
}
`
