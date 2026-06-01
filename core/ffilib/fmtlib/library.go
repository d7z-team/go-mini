package fmtlib

import (
	"context"
	gofmt "fmt"

	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

type Outputter interface {
	Print(context.Context, string)
}

type ctxKey string

const FMTKey ctxKey = "gomini.fmt.Outputter"

func WithOutputter(ctx context.Context, o Outputter) context.Context {
	return context.WithValue(ctx, FMTKey, o)
}

func Surface() *surface.Bundle {
	return surface.Merge(
		internalSurface(),
		surface.Library("fmt", surface.GoFile("fmt.mgo", fmtSource)),
		surface.Templates(fmtTemplates()...),
	)
}

const (
	methodWrite uint32 = iota + 1
	methodErrorf
)

func internalSurface() *surface.Bundle {
	schema := runtime.NewFFISurfaceSchema()
	if err := schema.AddFunc("fmt/internal", "Write", "fmt/internal.Write", methodWrite, runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecString), "Write already-formatted text to the host output sink"); err != nil {
		return &surface.Bundle{Err: err}
	}
	if err := schema.AddFunc("fmt/internal", "Errorf", "fmt/internal.Errorf", methodErrorf, runtime.MustRuntimeFuncSig(runtime.SpecError, false, runtime.SpecString, runtime.ArrayType(runtime.SpecError)), "Create a VM error with optional wrapped causes"); err != nil {
		return &surface.Bundle{Err: err}
	}
	return surface.New(schema, func(_ runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		bound := runtime.NewBoundFFISurface(schema)
		bound.AddRoute("fmt/internal", "Write", runtime.FFIRoute{
			Name:     "fmt/internal.Write",
			Native:   nativeWrite,
			MethodID: methodWrite,
			FuncSig:  runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecString),
			Doc:      "Write already-formatted text to the host output sink",
		})
		bound.AddRoute("fmt/internal", "Errorf", runtime.FFIRoute{
			Name:     "fmt/internal.Errorf",
			Native:   runtime.NativeFmtErrorf,
			MethodID: methodErrorf,
			FuncSig:  runtime.MustRuntimeFuncSig(runtime.SpecError, false, runtime.SpecString, runtime.ArrayType(runtime.SpecError)),
			Doc:      "Create a VM error with optional wrapped causes",
		})
		return bound, nil
	})
}

func nativeWrite(_ *runtime.Executor, session *runtime.StackContext, _ runtime.FFIRoute, args []*runtime.Var, _ []runtime.LHSValue) (*runtime.Var, error) {
	text := ""
	if len(args) > 0 && args[0] != nil && args[0].VType == runtime.TypeString {
		text = args[0].Str
	}
	ctx := context.Background()
	if session != nil && session.Context != nil {
		ctx = session.Context
	}
	if o, ok := ctx.Value(FMTKey).(Outputter); ok {
		o.Print(ctx, text)
		return nil, nil
	}
	gofmt.Print(text)
	return nil, nil
}

func fmtTemplates() []calltemplate.FunctionTemplate {
	return []calltemplate.FunctionTemplate{
		{
			ID:        "builtin.print",
			Name:      "print",
			SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
			Body:      `{{ pkg "fmt" }}.Print({{ args }})`,
		},
		{
			ID:        "builtin.println",
			Name:      "println",
			SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
			Body:      `{{ pkg "fmt" }}.Println({{ args }})`,
		},
	}
}

const fmtSource = `
package fmt

import internal "fmt/internal"
import "reflect"
import "strconv"
import "strings"

func Print(args ...any) {
	internal.Write(Sprint(args...))
}

func Println(args ...any) {
	internal.Write(Sprintln(args...))
}

func Printf(format string, args ...any) {
	internal.Write(Sprintf(format, args...))
}

func Sprint(args ...any) string {
	out := ""
	for i, arg := range args {
		if i > 0 && sprintNeedsSpace(args[i-1], arg) {
			out += " "
		}
		out += formatValue(arg, "v", false, false, 0)
	}
	return out
}

func Sprintln(args ...any) string {
	out := ""
	for i, arg := range args {
		if i > 0 {
			out += " "
		}
		out += formatValue(arg, "v", false, false, 0)
	}
	return out + "\n"
}

func Sprintf(format string, args ...any) string {
	text, _ := formatf(format, args, false)
	return text
}

func Errorf(format string, args ...any) error {
	text, causes := formatf(format, args, true)
	return internal.Errorf(text, causes)
}

func formatf(format string, args []any, errorMode bool) (string, []error) {
	out := ""
	causes := []error{}
	last := 0
	argIndex := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		out += format[last:i]
		j := i + 1
		if j >= len(format) {
			out += "%!(NOVERB)"
			last = j
			break
		}
		if format[j] == '%' {
			out += "%"
			last = j + 1
			i = j
			continue
		}

		left := false
		plus := false
		sharp := false
		zero := false
		for j < len(format) && isFlag(format[j]) {
			switch format[j] {
			case '-':
				left = true
			case '+':
				plus = true
			case '#':
				sharp = true
			case '0':
				zero = true
			}
			j++
		}
		width := int64(0)
		hasWidth := false
		for j < len(format) && isDigit(format[j]) {
			hasWidth = true
			width = width*10 + (format[j] - '0')
			j++
		}
		if j < len(format) && format[j] == '.' {
			j++
			for j < len(format) && isDigit(format[j]) {
				j++
			}
		}
		if j >= len(format) {
			out += "%!(NOVERB)"
			last = j
			break
		}

		verb := format[j:j+1]
		if argIndex >= len(args) {
			out += "%!" + verb + "(MISSING)"
			last = j + 1
			i = j
			continue
		}
		arg := args[argIndex]
		argIndex++
		if verb == "w" {
			if errorMode {
				if err, ok := arg.(error); ok && err != nil {
					causes = append(causes, err)
				}
			}
			verb = "v"
		}
		text := formatValue(arg, verb, plus, sharp, 0)
		if hasWidth {
			text = applyWidth(text, width, left, zero)
		}
		out += text
		last = j + 1
		i = j
	}
	out += format[last:]
	return out, causes
}

func formatValue(v any, verb string, plus bool, sharp bool, depth int64) string {
	if depth > 32 {
		return "<max-depth>"
	}
	if verb == "T" {
		return reflect.TypeOf(v).String()
	}
	switch x := v.(type) {
	case nil:
		return "<nil>"
	case bool:
		if verb == "t" || verb == "v" {
			return strconv.FormatBool(x)
		}
	case byte:
		return formatInt(Int64(x), verb)
	case rune:
		return formatInt(Int64(x), verb)
	case int64:
		return formatInt(x, verb)
	case float64:
		return formatFloat(x, verb)
	case string:
		return formatString(x, verb)
	case error:
		if x == nil {
			return "<nil>"
		}
		if verb == "q" {
			return strconv.Quote(x.Error())
		}
		return x.Error()
	}

	kind := reflect.KindOf(v)
	if kind == reflect.Interface {
		if inner, ok := reflect.Elem(v); ok {
			return formatValue(inner, verb, plus, sharp, depth+1)
		}
		if inner, ok := reflect.Unwrap(v); ok {
			return formatValue(inner, verb, plus, sharp, depth+1)
		}
		return "<nil>"
	}
	if verb == "s" {
		if isByteArray(v) {
			return String(v)
		}
		return formatValue(v, "v", plus, sharp, depth+1)
	}
	if verb == "q" && isByteArray(v) {
		return strconv.Quote(String(v))
	}
	switch kind {
	case reflect.Invalid:
		return "<nil>"
	case reflect.Array:
		if isByteArray(v) {
			return formatBytes(v, verb)
		}
		return formatArray(v, depth+1)
	case reflect.Map:
		return formatMap(v, depth+1)
	case reflect.Struct:
		return formatStruct(v, sharp, depth+1)
	case reflect.Ptr:
		return formatPointer(v, verb, plus, sharp, depth+1)
	case reflect.HostRef:
		return "<hostref " + reflect.TypeOf(v).String() + ">"
	case reflect.Chan:
		return "<chan " + reflect.TypeOf(v).String() + ">"
	case reflect.Func:
		return "<func " + reflect.TypeOf(v).String() + ">"
	case reflect.Module:
		return "<module " + reflect.TypeOf(v).String() + ">"
	case reflect.Any:
		if inner, ok := reflect.Unwrap(v); ok {
			return formatValue(inner, verb, plus, sharp, depth+1)
		}
	}
	if sharp {
		return reflect.TypeOf(v).String() + "(" + String(v) + ")"
	}
	return String(v)
}

func formatPointer(v any, verb string, plus bool, sharp bool, depth int64) string {
	if reflect.IsNil(v) {
		if verb == "p" {
			return "0x0"
		}
		return "<nil>"
	}
	if verb == "p" {
		return "<" + reflect.TypeOf(v).String() + ">"
	}
	if elem, ok := reflect.Elem(v); ok {
		return "&" + formatValue(elem, "v", plus, sharp, depth+1)
	}
	return "&<" + reflect.TypeOf(v).String() + ">"
}

func formatStruct(v any, sharp bool, depth int64) string {
	fields := reflect.Fields(v)
	parts := []string{}
	for _, field := range fields {
		item, ok := reflect.Field(v, field.Name)
		if !ok {
			continue
		}
		parts = append(parts, field.Name+":"+formatValue(item, "v", false, false, depth+1))
	}
	body := "{" + strings.Join(parts, " ") + "}"
	if sharp {
		return reflect.TypeOf(v).String() + body
	}
	return body
}

func formatArray(v any, depth int64) string {
	parts := []string{}
	for i := 0; i < reflect.Len(v); i++ {
		item, ok := reflect.Index(v, i)
		if !ok {
			continue
		}
		parts = append(parts, formatValue(item, "v", false, false, depth+1))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func formatMap(v any, depth int64) string {
	keys, ok := reflect.MapKeys(v)
	if !ok {
		return "map[]"
	}
	parts := []string{}
	for _, key := range keys {
		item, ok := reflect.MapIndex(v, key)
		if !ok {
			continue
		}
		parts = append(parts, formatMapKey(key, depth+1)+":"+formatValue(item, "v", false, false, depth+1))
	}
	sortText(parts)
	return "map[" + strings.Join(parts, " ") + "]"
}

func sortText(items []string) {
	for i := 1; i < len(items); i++ {
		item := items[i]
		j := i - 1
		for j >= 0 && items[j] > item {
			items[j+1] = items[j]
			j--
		}
		items[j+1] = item
	}
}

func formatMapKey(v any, depth int64) string {
	if s, ok := v.(string); ok {
		return s
	}
	return formatValue(v, "v", false, false, depth+1)
}

func formatBytes(v any, verb string) string {
	if verb == "s" {
		return String(v)
	}
	if verb == "q" {
		return strconv.Quote(String(v))
	}
	if verb == "x" || verb == "X" {
		out := ""
		for i := 0; i < reflect.Len(v); i++ {
			item, ok := reflect.Index(v, i)
			if !ok {
				continue
			}
			if b, ok := item.(byte); ok {
				out += hexByte(Int64(b), verb == "X")
			}
		}
		return out
	}
	parts := []string{}
	for i := 0; i < reflect.Len(v); i++ {
		item, ok := reflect.Index(v, i)
		if !ok {
			continue
		}
		if b, ok := item.(byte); ok {
			parts = append(parts, strconv.FormatInt(Int64(b), 10))
		}
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func isByteArray(v any) bool {
	if reflect.KindOf(v) != reflect.Array {
		return false
	}
	return reflect.TypeOf(v).Elem().String() == "Byte"
}

func formatString(v string, verb string) string {
	switch verb {
	case "q":
		return strconv.Quote(v)
	case "x":
		return stringHex(v, false)
	case "X":
		return stringHex(v, true)
	default:
		return v
	}
}

func formatInt(v int64, verb string) string {
	switch verb {
	case "b":
		return strconv.FormatInt(v, 2)
	case "c":
		return codePointString(v)
	case "o":
		return strconv.FormatInt(v, 8)
	case "x":
		return strconv.FormatInt(v, 16)
	case "X":
		return strings.ToUpper(strconv.FormatInt(v, 16))
	case "q":
		return quoteRune(v)
	default:
		return strconv.FormatInt(v, 10)
	}
}

func quoteRune(v int64) string {
	switch v {
	case '\n':
		return "'\\n'"
	case '\r':
		return "'\\r'"
	case '\t':
		return "'\\t'"
	case '\\':
		return "'\\\\'"
	case '\'':
		return "'\\''"
	}
	if v < 32 || v == 127 {
		hex := strconv.FormatInt(v, 16)
		if len(hex) == 1 {
			hex = "0" + hex
		}
		return "'\\x" + hex + "'"
	}
	return "'" + codePointString(v) + "'"
}

func formatFloat(v float64, verb string) string {
	switch verb {
	case "e":
		return strconv.FormatFloat(v, 'e', -1, 64)
	case "E":
		return strconv.FormatFloat(v, 'E', -1, 64)
	case "f":
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
}

func stringHex(v string, upper bool) string {
	out := ""
	for i := 0; i < len(v); i++ {
		out += hexByte(v[i], upper)
	}
	return out
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

func hexByte(v int64, upper bool) string {
	digits := "0123456789abcdef"
	if upper {
		digits = "0123456789ABCDEF"
	}
	hi := (v / 16) % 16
	lo := v % 16
	return digits[hi:hi+1] + digits[lo:lo+1]
}

func applyWidth(text string, width int64, left bool, zero bool) string {
	padCount := width - len(text)
	if padCount <= 0 {
		return text
	}
	pad := " "
	if zero && !left {
		pad = "0"
	}
	for i := int64(0); i < padCount; i++ {
		if left {
			text += pad
		} else {
			text = pad + text
		}
	}
	return text
}

func sprintNeedsSpace(left any, right any) bool {
	return reflect.KindOf(left) != reflect.String && reflect.KindOf(right) != reflect.String
}

func isFlag(ch int64) bool {
	return ch == '-' || ch == '+' || ch == '#' || ch == ' ' || ch == '0'
}

func isDigit(ch int64) bool {
	return ch >= '0' && ch <= '9'
}
`
