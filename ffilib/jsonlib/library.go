package jsonlib

import (
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

func Surface() *surface.Bundle {
	return surface.Merge(
		surface.Library("encoding/json/internal", surface.GoFile("internal.mgo", jsonInternalSource)),
		surface.Library("encoding/json", surface.GoFile("json.mgo", jsonSource)),
		surface.Templates(calltemplate.FunctionTemplate{
			ID:           "encoding/json.Unmarshal",
			PackagePath:  "encoding/json",
			Name:         "Unmarshal",
			TemplateOnly: true,
			SourceSig:    runtime.MustRuntimeFuncSig(runtime.SpecError, false, runtime.SpecBytes, runtime.SpecAny),
			RawArgs:      []int{1},
			Body: `
func() error {
	__gomini_tpl_decoded, __gomini_tpl_err := {{ pkg "encoding/json" }}.Decode({{ callArg 0 }})
	if __gomini_tpl_err != nil {
		return __gomini_tpl_err
	}
	__gomini_tpl_value, __gomini_tpl_err := {{ pkg "encoding/json/internal" }}.ConvertDecodedTarget({{ argType 1 }}, __gomini_tpl_decoded)
	if __gomini_tpl_err != nil {
		return __gomini_tpl_err
	}
	return {{ pkg "reflect" }}.Assign({{ callArg 1 }}, __gomini_tpl_value)
}()`,
		}),
	)
}

const jsonSource = `
package json

import "encoding/json/internal"
import "fmt"
import "reflect"
import "sort"
import "strconv"
import "strings"

type parser struct {
	text string
	pos int
}

func Marshal(v any) ([]byte, error) {
	text, err := marshalValue(v)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func Decode(data []byte) (any, error) {
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

func marshalValue(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
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

	switch reflect.KindOf(v) {
	case reflect.Bytes:
		return marshalBytes(v)
	case reflect.Array:
		return marshalArray(v)
	case reflect.Map:
		return marshalMap(v)
	case reflect.Struct:
		return marshalStruct(v)
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
	buf := []byte{}
	for i := 0; i < reflect.Len(v); i++ {
		item, ok := reflect.Index(v, i)
		if !ok {
			return "", fmt.Errorf("json: bytes index %d out of range", i)
		}
		b, ok := item.(int64)
		if !ok {
			return "", fmt.Errorf("json: bytes item %d is not int64", i)
		}
		buf = append(buf, b)
	}
	return strconv.Quote(string(buf)), nil
}

func marshalArray(v any) (string, error) {
	parts := []string{}
	for i := 0; i < reflect.Len(v); i++ {
		item, ok := reflect.Index(v, i)
		if !ok {
			return "", fmt.Errorf("json: array index %d out of range", i)
		}
		text, err := marshalValue(item)
		if err != nil {
			return "", err
		}
		parts = append(parts, text)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

func marshalMap(v any) (string, error) {
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
		text, err := marshalValue(item)
		if err != nil {
			return "", err
		}
		parts = append(parts, strconv.Quote(key)+":"+text)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func marshalStruct(v any) (string, error) {
	fields := reflect.Fields(v)
	parts := []string{}
	for _, field := range fields {
		name, include := internal.FieldName(field)
		if !include {
			continue
		}
		item, ok := reflect.Field(v, field.Name)
		if !ok {
			return "", fmt.Errorf("json: field %s is not accessible", field.Name)
		}
		text, err := marshalValue(item)
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
	}
	return nil, fmt.Errorf("json: invalid array state")
}

func parseString(p *parser) (string, error) {
	if p.pos >= len(p.text) || p.text[p.pos] != '"' {
		return "", fmt.Errorf("json: expected string at byte %d", p.pos)
	}
	start := p.pos
	p.pos++
	escaped := false
	for p.pos < len(p.text) {
		ch := p.text[p.pos]
		if escaped {
			escaped = false
			p.pos++
			continue
		}
		if ch == '\\' {
			escaped = true
			p.pos++
			continue
		}
		if ch == '"' {
			p.pos++
			return strconv.Unquote(p.text[start:p.pos])
		}
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
	return strconv.ParseFloat(p.text[start:p.pos], 64)
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
`

const jsonInternalSource = `
package internal

import "fmt"
import "reflect"
import "strings"

func ConvertDecodedTarget(targetName string, value any) (any, error) {
	target, ok := reflect.TypeFrom(targetName)
	if !ok {
		return nil, fmt.Errorf("json: invalid Unmarshal target type %s", targetName)
	}
	if target.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("json: Unmarshal target must be pointer, got %s", target.String())
	}
	elem := target.Elem()
	if elem.String() == "" {
		return nil, fmt.Errorf("json: invalid Unmarshal target element for %s (%s kind %d)", targetName, target.String(), target.Kind())
	}
	if err := validateJSONType(elem); err != nil {
		return nil, err
	}
	return convertValue(elem, value)
}

func convertValue(target reflect.Type, value any) (any, error) {
	switch target.Kind() {
	case reflect.Any:
		return value, nil
	case reflect.Bool:
		boolValue, boolOK := value.(bool)
		if !boolOK {
			return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
		}
		return boolValue, nil
	case reflect.Int64:
		numberValue, numberOK := value.(float64)
		if !numberOK {
			return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
		}
		intValue := int64(numberValue)
		if float64(intValue) != numberValue {
			return nil, fmt.Errorf("json: cannot decode non-integer number into %s", target.String())
		}
		return intValue, nil
	case reflect.Float64:
		floatValue, floatOK := value.(float64)
		if !floatOK {
			return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
		}
		return floatValue, nil
	case reflect.String:
		stringValue, stringOK := value.(string)
		if !stringOK {
			return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
		}
		return stringValue, nil
	case reflect.Bytes:
		bytesString, bytesStringOK := value.(string)
		if !bytesStringOK {
			return nil, fmt.Errorf("json: cannot decode %s into %s", jsonValueName(value), target.String())
		}
		return []byte(bytesString), nil
	case reflect.Array:
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
			name, include := FieldName(field)
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

func validateJSONType(target reflect.Type) error {
	switch target.Kind() {
	case reflect.Any, reflect.Bool, reflect.Int64, reflect.Float64, reflect.String, reflect.Bytes:
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
			if _, include := FieldName(field); !include {
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

func FieldName(field reflect.StructField) (string, bool) {
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
	if value == nil {
		return "null"
	}
	switch value.(type) {
	case bool:
		return "bool"
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
