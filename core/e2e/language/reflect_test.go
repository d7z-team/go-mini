package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	miniruntime "gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

func TestReflectStructMetadataAndFieldMutation(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

import "reflect"

type User struct {
	Name string ` + "`json:\"name\"`" + `
	Age int
}

func (u User) Label() string {
	return u.Name
}

func main() {
	u := User{Name: "Ada", Age: 41}
	t := reflect.TypeOf(u)
	if t.String() != "main.User" || t.Name() != "User" || t.PkgPath() != "main" || t.Kind() != reflect.Struct {
		panic("bad reflected struct type")
	}
	if t.NumField() != 2 {
		panic("bad reflected field count")
	}
	nameField, ok := t.FieldByName("Name")
	if !ok || nameField.Name != "Name" || nameField.Tag != "json:\"name\"" || nameField.Type.String() != "String" {
		panic("bad reflected field metadata")
	}
	if t.Field(1).Name != "Age" {
		panic("bad reflected field order")
	}
	if len(reflect.Fields(u)) != 2 {
		panic("bad reflect.Fields result")
	}

	rawName, ok := reflect.Field(&u, "Name")
	if !ok || rawName.(String) != "Ada" {
		panic("bad reflected field value")
	}
	if err := reflect.SetField(&u, "Age", 42); err != nil {
		panic(err.Error())
	}
	if u.Age != 42 {
		panic("reflected field assignment failed")
	}

	method, ok := t.MethodByName("Label")
	if !ok || method.Name != "Label" || method.IsFFI || method.IsNative || method.Type.Kind() != reflect.Func {
		panic("bad reflected VM method")
	}
	if method.Type.NumIn() != 1 || method.Type.NumOut() != 1 || method.Type.Out(0).String() != "String" {
		panic("bad reflected method signature")
	}

	created := reflect.Zero("main.User")
	if err := reflect.SetField(created, "Name", "Lin"); err != nil {
		panic(err.Error())
	}
	createdName, ok := reflect.Field(created, "Name")
	if !ok || createdName.(String) != "Lin" {
		panic("bad reflected zero value mutation")
	}
	if reflect.SetField(u, "Age", 43) == nil {
		panic("direct struct SetField should fail")
	}
	if reflect.SetField(&t, "Raw", "String") == nil {
		panic("reflect metadata pointer should be read-only")
	}
	var meta Any = t
	if reflect.SetField(meta, "Raw", "String") == nil {
		panic("reflect metadata Any should be read-only")
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestReflectMetadataRejectsDirectMutation(t *testing.T) {
	cases := map[string]string{
		"field assignment": `
package main

import "reflect"

type User struct {
	Name string
}

func main() {
	t := reflect.TypeOf(User{Name: "Ada"})
	t.Raw = "String"
}
`,
		"field pointer": `
package main

import "reflect"

type User struct {
	Name string
}

func main() {
	t := reflect.TypeOf(User{Name: "Ada"})
	raw := &t.Raw
	*raw = "String"
}
`,
	}

	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			executor := engine.MustNewMiniExecutor()
			prog, err := executor.NewRuntimeByGoCode(code)
			if err != nil {
				t.Fatalf("compile failed: %v", err)
			}
			if err := prog.Execute(context.Background()); err == nil {
				t.Fatal("expected metadata mutation to fail")
			}
		})
	}
}

func TestReflectLookupAndAnyBoundaries(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

import "reflect"

type UnsafeBox struct {
	Name string
	Ptr *int
	Ch chan int
}

type AnyBox struct {
	Value Any
}

type CollectionBox struct {
	Items []int64
	Lookup map[string]int64
}

func local() int {
	return 1
}

func main() {
	n := 1
	box := UnsafeBox{Name: "safe", Ptr: &n, Ch: make(chan int)}
	name, ok := reflect.Field(box, "Name")
	if !ok || name.(String) != "safe" {
		panic("safe field should be reflected")
	}
	if _, ok := reflect.Field(box, "Ptr"); ok {
		panic("pointer field must not enter reflect Any")
	}
	if _, ok := reflect.Field(box, "Ch"); ok {
		panic("channel field must not enter reflect Any")
	}
	anyBox := AnyBox{}
	if err := reflect.SetField(&anyBox, "Value", local); err == nil {
		panic("function value must not enter reflect Any")
	}

	missing, ok := reflect.TypeFrom("Missing")
	if ok || missing.String() != "" || missing.Kind() != reflect.Invalid {
		panic("missing type should return zero Type")
	}
	if nested, ok := reflect.TypeFrom("Ptr<Missing>"); ok || nested.String() != "" {
		panic("nested missing pointer type should return zero Type")
	}
	if nested, ok := reflect.TypeFrom("Array<Missing>"); ok || nested.String() != "" {
		panic("nested missing array type should return zero Type")
	}
	if nested, ok := reflect.TypeFrom("function(Missing) Void"); ok || nested.String() != "" {
		panic("nested missing function type should return zero Type")
	}
	t, ok := reflect.TypeFrom("main.UnsafeBox")
	if !ok || t.String() != "main.UnsafeBox" {
		panic("known type lookup failed")
	}
	if t.Field(99).Index != -1 || t.Field(99).Name != "" {
		panic("invalid field index should return zero field")
	}
	if t.Method(99).Index != -1 || t.Method(99).Name != "" {
		panic("invalid method index should return zero method")
	}
	if t.In(0).String() != "" || t.Out(0).String() != "" {
		panic("non-function type input/output should be zero Type")
	}

	pkg, ok := reflect.Package("missing")
	if ok || pkg.Path != "" {
		panic("missing package should return zero PackageInfo")
	}

	collections := CollectionBox{Items: []int64{1, 2}, Lookup: map[string]int64{"a": 1}}
	rawItems, ok := reflect.Field(&collections, "Items")
	if !ok {
		panic("array field should be reflected")
	}
	items := rawItems.([]int64)
	items[0] = 99
	if collections.Items[0] != 1 {
		panic("array field reflection must return a snapshot")
	}
	rawLookup, ok := reflect.Field(&collections, "Lookup")
	if !ok {
		panic("map field should be reflected")
	}
	lookup := rawLookup.(map[string]int64)
	lookup["a"] = 99
	if collections.Lookup["a"] != 1 {
		panic("map field reflection must return a snapshot")
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestReflectCollectionValueAPIs(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

import "reflect"

type Labeler interface {
	Label() string
}

type User struct {
	Name string
}

func (u User) Label() string {
	return u.Name
}

func main() {
	items := []int64{10, 20}
	if reflect.Len(items) != 2 {
		panic("bad reflected array length")
	}
	item, ok := reflect.Index(items, 1)
	if !ok || item.(Int64) != 20 {
		panic("bad reflected array index")
	}
	if _, ok := reflect.Index(items, 9); ok {
		panic("out of range array index should fail")
	}
	ch := make(chan int64, 2)
	ch <- 1
	if reflect.Len(ch) != 1 {
		panic("bad reflected channel length")
	}
	if b, ok := reflect.Index([]byte("az"), 1); !ok || b.(Int64) != 122 {
		panic("bad reflected bytes index")
	}
	if c, ok := reflect.Index("go", 1); !ok || c.(Int64) != 111 {
		panic("bad reflected string index")
	}

	lookup := map[string]int64{"a": 1, "b": 2}
	if reflect.Len(lookup) != 2 {
		panic("bad reflected map length")
	}
	keys, ok := reflect.MapKeys(lookup)
	if !ok || len(keys) != 2 {
		panic("bad reflected map keys")
	}
	seen := map[string]bool{}
	for _, key := range keys {
		seen[key.(String)] = true
	}
	if !seen["a"] || !seen["b"] {
		panic("bad reflected map key snapshot")
	}
	value, ok := reflect.MapIndex(lookup, "b")
	if !ok || value.(Int64) != 2 {
		panic("bad reflected map index")
	}
	if _, ok := reflect.MapIndex(lookup, "missing"); ok {
		panic("missing reflected map index should fail")
	}

	dynamic, ok := reflect.MakeMap("Map<String, Int64>")
	if !ok || reflect.KindOf(dynamic) != reflect.Map || reflect.Len(dynamic) != 0 {
		panic("bad reflected MakeMap result")
	}
	if err := reflect.SetMapIndex(dynamic, "x", 7); err != nil {
		panic(err.Error())
	}
	madeValue, ok := reflect.MapIndex(dynamic, "x")
	if !ok || madeValue.(Int64) != 7 || reflect.Len(dynamic) != 1 {
		panic("bad reflected SetMapIndex result")
	}
	if _, ok := reflect.MakeMap("Map<String, Ptr<Int64>>"); ok {
		panic("unsafe map value type should not enter reflected Any")
	}
	if _, ok := reflect.MakeMap("Array<Int64>"); ok {
		panic("non-map MakeMap should fail")
	}

	var anyValue any = map[string]any{"name": "mini"}
	unwrapped, ok := reflect.Unwrap(anyValue)
	if !ok || reflect.KindOf(unwrapped) != reflect.Map {
		panic("bad reflected Any unwrap")
	}
	name, ok := reflect.MapIndex(unwrapped, "name")
	if !ok || name.(String) != "mini" {
		panic("bad reflected unwrapped map value")
	}

	var labeler Labeler = User{Name: "Ada"}
	unwrapped, ok = reflect.Unwrap(labeler)
	if !ok || reflect.KindOf(unwrapped) != reflect.Struct {
		panic("bad reflected interface unwrap")
	}
	field, ok := reflect.Field(unwrapped, "Name")
	if !ok || field.(String) != "Ada" {
		panic("bad reflected unwrapped interface field")
	}

	n := int64(1)
	unsafeKeys := map[*int64]string{&n: "x"}
	if _, ok := reflect.MapKeys(unsafeKeys); ok {
		panic("unsafe map key should not enter reflected Any")
	}
	emptyUnsafeKeys := map[*int64]string{}
	if _, ok := reflect.MapKeys(emptyUnsafeKeys); ok {
		panic("empty unsafe map key type should not enter reflected Any")
	}
	unsafeKeyValues := map[string]*int64{"n": &n}
	if _, ok := reflect.MapKeys(unsafeKeyValues); ok {
		panic("unsafe map value type should block reflected keys")
	}
	emptyUnsafeKeyValues := map[string]*int64{}
	if _, ok := reflect.MapKeys(emptyUnsafeKeyValues); ok {
		panic("empty unsafe map value type should block reflected keys")
	}
	unsafeValues := map[string]*int64{}
	if err := reflect.SetMapIndex(unsafeValues, "n", &n); err == nil {
		panic("unsafe map value should not enter reflected Any")
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestReflectNamedTypeMethods(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

import "reflect"

type Names []string
type MoreNames Names
type Lookup map[string]int64
type Handler func(string, int64) (int64, string)

func main() {
	names, ok := reflect.TypeFrom("main.Names")
	if !ok || names.String() != "main.Names" || names.Kind() != reflect.Array || names.Elem().String() != "String" {
		panic("bad reflected named array element")
	}
	more, ok := reflect.TypeFrom("main.MoreNames")
	if !ok || more.String() != "main.MoreNames" || more.Kind() != reflect.Array || more.Elem().String() != "String" {
		panic("bad reflected chained named array element")
	}
	lookup, ok := reflect.TypeFrom("main.Lookup")
	if !ok || lookup.String() != "main.Lookup" || lookup.Kind() != reflect.Map || lookup.Key().String() != "String" || lookup.Elem().String() != "Int64" {
		panic("bad reflected named map type")
	}
	handler, ok := reflect.TypeFrom("main.Handler")
	if !ok || handler.String() != "main.Handler" || handler.Kind() != reflect.Func {
		panic("bad reflected named function kind")
	}
	if handler.NumIn() != 2 || handler.In(0).String() != "String" || handler.In(1).String() != "Int64" {
		panic("bad reflected named function inputs")
	}
	if handler.NumOut() != 2 || handler.Out(0).String() != "Int64" || handler.Out(1).String() != "String" {
		panic("bad reflected named function outputs")
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestReflectDistinguishesSameStructAcrossModules(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	err := executor.UseSurface(surface.Libraries(
		surface.LibraryModule{
			Path: "alpha",
			Files: []surface.LibraryFile{surface.GoFile("alpha.mgo", `
package alpha

type User struct {
	Name string
}

func New(name string) User {
	return User{Name: name}
}
`)},
		},
		surface.LibraryModule{
			Path: "beta",
			Files: []surface.LibraryFile{surface.GoFile("beta.mgo", `
package beta

type User struct {
	Name string
}

func New(name string) User {
	return User{Name: name}
}
`)},
		},
	))
	if err != nil {
		t.Fatalf("register source libraries failed: %v", err)
	}

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "reflect"
import "alpha"
import "beta"

func main() {
	a := alpha.New("Ada")
	b := beta.New("Lin")
	ta := reflect.TypeOf(a)
	tb := reflect.TypeOf(b)
	if ta.String() != "alpha.User" || ta.Name() != "User" || ta.PkgPath() != "alpha" {
		panic("bad alpha type identity")
	}
	if tb.String() != "beta.User" || tb.Name() != "User" || tb.PkgPath() != "beta" {
		panic("bad beta type identity")
	}
	if ta.AssignableTo(tb) || tb.AssignableTo(ta) {
		panic("same-shaped module structs must not be assignable")
	}
	alphaType, ok := reflect.TypeFrom("alpha.User")
	if !ok {
		panic("alpha TypeFrom failed")
	}
	if !alphaType.AssignableTo(ta) {
		panic("alpha TypeFrom should assign to alpha value")
	}
	if alphaType.AssignableTo(tb) {
		panic("alpha TypeFrom must not assign to beta value")
	}
	if _, ok := reflect.TypeFrom("User"); ok {
		panic("unqualified struct lookup must not resolve")
	}
	av, ok := reflect.Field(a, "Name")
	if !ok || av.(String) != "Ada" {
		panic("bad alpha reflected field")
	}
	bv, ok := reflect.Field(b, "Name")
	if !ok || bv.(String) != "Lin" {
		panic("bad beta reflected field")
	}
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestReflectPureAnyTypeBoundaries(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

import "reflect"

type UnsafeCollections struct {
	Ptrs []*int64
	Lookup map[string]*int64
	Callbacks []func() int64
}

func main() {
	box := UnsafeCollections{}
	if _, ok := reflect.Field(box, "Ptrs"); ok {
		panic("empty pointer array field must not enter reflect Any")
	}
	if _, ok := reflect.Field(box, "Lookup"); ok {
		panic("empty pointer map field must not enter reflect Any")
	}
	if _, ok := reflect.Field(box, "Callbacks"); ok {
		panic("empty function array field must not enter reflect Any")
	}
	if _, ok := reflect.MakeMap("Map<String, function() Int64>"); ok {
		panic("function map values must not enter reflect Any")
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestReflectZeroRejectsNestedUnknownType(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

import "reflect"

func main() {
	reflect.Zero("Array<Missing>")
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err == nil {
		t.Fatal("expected reflect.Zero to reject nested unknown type")
	}
}

func TestReflectZeroRejectsUnsafeAnyType(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

import "reflect"

func main() {
	reflect.Zero("Map<String, Ptr<Int64>>")
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err == nil {
		t.Fatal("expected reflect.Zero to reject unsafe Any type")
	}
}

func TestReflectMetadataSurvivesBytecodeJSONRoundTrip(t *testing.T) {
	compilerExec := engine.MustNewMiniExecutor()
	payload, err := compilerExec.CompileGoCodeToBytecodeJSON(`
package main

import "reflect"

type User struct {
	Name string ` + "`json:\"name\"`" + `
}

func (u User) Label() string {
	return u.Name
}

func main() {
	t := reflect.TypeOf(User{Name: "Ada"})
	field, ok := t.FieldByName("Name")
	if !ok || field.Tag != "json:\"name\"" {
		panic("field tag did not survive bytecode load")
	}
	method, ok := t.MethodByName("Label")
	if !ok || method.Type.NumOut() != 1 || method.Type.Out(0).String() != "String" {
		panic("method route did not survive bytecode load")
	}
}
`)
	if err != nil {
		t.Fatalf("compile bytecode failed: %v", err)
	}

	loader := engine.MustNewMiniExecutor()
	prog, err := loader.NewRuntimeByBytecodeJSON(payload)
	if err != nil {
		t.Fatalf("load bytecode failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute bytecode failed: %v", err)
	}
}

func TestReflectPredicatesAndFFIPackageMetadata(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	if err := executor.UseSurface(surface.Library("model", surface.GoFile("model.mgo", `
package model

type User struct {
	Name string
}

func NewUser(name string) User {
	return User{Name: name}
}
`))); err != nil {
		t.Fatalf("register model source library: %v", err)
	}
	code := `
	package main

	import "model"
	import "reflect"
	import "strings"

type User struct {
	Name string
}

func local() int {
	return 1
}

func main() {
	u := User{Name: "Ada"}
	if !reflect.IsPtr(&u) || !reflect.IsStruct(u) || reflect.IsStruct(&u) {
		panic("bad pointer or struct predicate")
	}
	if !reflect.IsVMFunc(local) || !reflect.IsFunc(local) {
		panic("bad VM function predicate")
	}
	if !reflect.IsFFIFunc(strings.Contains) || !reflect.IsFunc(strings.Contains) {
		panic("bad FFI function predicate")
	}
	if !reflect.IsNativeFunc(reflect.TypeOf) || !reflect.IsFunc(reflect.TypeOf) {
		panic("bad native function predicate")
	}

	pkg, ok := reflect.Package("strings")
	if !ok || pkg.Path != "strings" {
		panic("bad reflected package")
	}
	found := false
	for _, member := range reflect.Members(pkg) {
		if member.Name == "Contains" {
			found = true
			if member.Kind != "func" || !member.Route.IsFFI || member.Route.MethodID == 0 || member.Type.Kind() != reflect.Func {
				panic("bad reflected FFI member")
			}
		}
	}
		if !found {
			panic("missing reflected FFI member")
		}
		member, ok := reflect.MemberByName(pkg, "Contains")
		if !ok || member.Name != "Contains" || !member.Route.IsFFI {
			panic("bad reflected member lookup")
		}
		sourcePkg, ok := reflect.Package("model")
		if !ok || sourcePkg.Path != "model" {
			panic("bad reflected source package")
		}
		sourceMember, ok := reflect.MemberByName(sourcePkg, "NewUser")
		if !ok || sourceMember.Kind != "func" || sourceMember.Route.IsFFI || sourceMember.Type.Kind() != reflect.Func {
			panic("bad reflected source member")
		}
		if model.NewUser("Ada").Name != "Ada" {
			panic("bad source module call")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestReflectShowsTypeOnlyFFIModuleMembers(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	schema := miniruntime.NewFFISurfaceSchema()
	if err := schema.AddStruct("acme.tools", "Payload", miniruntime.MustParseRuntimeStructSpec(
		"acme.tools.Payload",
		miniruntime.StructOwnershipVMValue,
		"struct { Value Int64; }",
	)); err != nil {
		t.Fatal(err)
	}
	if err := executor.UseSurface(surface.Router(schema, nil)); err != nil {
		t.Fatalf("register type-only FFI surface: %v", err)
	}

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "reflect"

func main() {
	pkg, ok := reflect.Package("acme.tools")
	if !ok || pkg.Path != "acme.tools" {
		panic("missing type-only FFI package")
	}
	member, ok := reflect.MemberByName(pkg, "Payload")
	if !ok || member.Name != "Payload" || member.Kind != "type" {
		panic("missing type-only FFI member")
	}
	if member.Type.String() != "acme.tools.Payload" || member.Type.Name() != "Payload" || member.Type.PkgPath() != "acme.tools" {
		panic("bad type-only FFI member type")
	}
	found := false
	for _, item := range reflect.Members(pkg) {
		if item.Name == "Payload" && item.Kind == "type" {
			found = true
		}
	}
	if !found {
		panic("type-only FFI member not listed")
	}
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}
