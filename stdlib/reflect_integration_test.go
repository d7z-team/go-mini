package stdlib_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func TestReflectRecvNilChannelReportsBlocked(t *testing.T) {
	const root = "minigo.test/reflect-recv"
	program := prepareStdlibProgram(t, root, `package main

import "reflect"

func Block() {
	var values chan int
	_, _ = reflect.ValueOf(values).Recv()
}
	`, []compiler.EntryPoint{{Name: "block", ModulePath: root, Function: "Block"}})
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close instance: %v", err)
		}
	})
	execution, err := instance.Start("block")
	if err != nil {
		t.Fatal(err)
	}
	state, runErr := execution.Poll()
	var blocked minigoruntime.AllBlockedError
	if state != minigoruntime.ExecutionFailed || !errors.As(runErr, &blocked) {
		t.Fatalf("nil channel receive state = %s, %v", state, runErr)
	}
}

func TestReflectMakeFuncPinsAndReleasesRevision(t *testing.T) {
	const root = "minigo.test/reflect-patch"
	const sourceTemplate = `package main

import "reflect"

var held reflect.Value

func Install() {
	version := VERSION
	held = reflect.MakeFunc(reflect.TypeOf((func() int)(nil)), func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(version)}
	})
}

func CallHeld() int {
	return int(held.Call([]reflect.Value{})[0].Int())
}
`
	testReflectCallableRevisionLifecycle(t, root, sourceTemplate, "MakeFunc")
}

func TestReflectMethodFuncPinsAndReleasesRevision(t *testing.T) {
	const root = "minigo.test/reflect-method-patch"
	const sourceTemplate = `package main

import "reflect"

var held reflect.Value

type runner struct{}

func (runner) Value() int {
	return VERSION
}

func Install() {
	method, ok := reflect.TypeOf(runner{}).MethodByName("Value")
	if !ok {
		panic("method is missing")
	}
	held = method.Func
}

func CallHeld() int {
	return int(held.Call([]reflect.Value{reflect.ValueOf(runner{})})[0].Int())
}
`
	testReflectCallableRevisionLifecycle(t, root, sourceTemplate, "Method.Func")
}

func TestReflectCallPreservesCrossModuleNamedTypes(t *testing.T) {
	const root = "minigo.test/reflect-cross-module"
	const library = "minigo.test/reflect-cross-module/value"
	script, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{
			ModulePath: library,
			Files: []source.File{{Path: "value.mgo", Text: `package value

type Number int

func AddOne(value Number) Number {
	return value + 1
}
`}},
		},
		{
			ModulePath: root,
			Files: []source.File{{Path: "main.mgo", Text: `package main

import (
	"minigo.test/reflect-cross-module/value"
	"reflect"
)

func Run() int {
	result := reflect.ValueOf(value.AddOne).Call([]reflect.Value{reflect.ValueOf(value.Number(41))})
	converted, ok := result[0].Interface().(value.Number)
	if !ok {
		panic("named result type was lost")
	}
	return int(converted)
}
`}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := workspace.MergeSourceSets(script, testStandardLibrary(t))
	if err != nil {
		t.Fatal(err)
	}
	cacheRoot, err := cache.ResolveDiskRoot("")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := compiler.Prepare(compiler.Request{
		Root: root, Sources: sources, Cache: cache.New(cache.NewDiskBackend(cacheRoot)),
		EntryPoints: []compiler.EntryPoint{{Name: "run", ModulePath: root, Function: "Run"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.Checked.OK() || prepared.Image == nil {
		t.Fatalf("compile diagnostics = %#v", prepared.Checked.Diagnostics)
	}
	program, err := minigoruntime.LoadExecutionImage(*prepared.Image)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close instance: %v", err)
		}
	})
	result, err := instance.Call(context.Background(), "run")
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Values[0].Int64(); !ok || value != 42 {
		t.Fatalf("cross-module reflect call result = %#v", result.Values)
	}
}

func TestReflectChannelStateSurvivesCompatiblePatch(t *testing.T) {
	const root = "minigo.test/reflect-channel-patch"
	const sourceTemplate = `package main

import "reflect"

var values chan int

func Install() {
	if values == nil {
		values = make(chan int, 1)
	}
	reflect.ValueOf(values).Send(reflect.ValueOf(VERSION))
}

func Receive() int {
	chosen, value, ok := reflect.Select([]reflect.SelectCase{{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(values)}})
	if chosen != 0 || !ok {
		panic("reflect select did not receive")
	}
	return int(value.Int())
}

func TypeName() string {
	return reflect.TypeOf(values).String()
}
`
	entries := []compiler.EntryPoint{
		{Name: "install", ModulePath: root, Function: "Install"},
		{Name: "receive", ModulePath: root, Function: "Receive"},
		{Name: "type", ModulePath: root, Function: "TypeName"},
	}
	oldProgram := prepareStdlibProgram(t, root, strings.ReplaceAll(sourceTemplate, "VERSION", "1"), entries)
	newProgram := prepareStdlibProgram(t, root, strings.ReplaceAll(sourceTemplate, "VERSION", "2"), entries)
	instance, err := oldProgram.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close instance: %v", err)
		}
	})
	if _, err = instance.Call(context.Background(), "type"); err != nil {
		t.Fatal(err)
	}
	if _, err = instance.Call(context.Background(), "install"); err != nil {
		t.Fatal(err)
	}
	plan, err := instance.PreparePatch(context.Background(), newProgram)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = instance.ApplyPatch(plan); err != nil {
		t.Fatal(err)
	}
	if retained, err := instance.RetainedRevisions(context.Background()); err != nil || len(retained) != 1 || retained[0].Generation != 2 {
		t.Fatalf("type metadata or channel state retained the old revision: %#v", retained)
	}
	result, err := instance.Call(context.Background(), "receive")
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Values[0].Int64(); !ok || value != 1 {
		t.Fatalf("pre-patch channel value = %#v", result.Values)
	}
	if _, err = instance.Call(context.Background(), "install"); err != nil {
		t.Fatal(err)
	}
	result, err = instance.Call(context.Background(), "receive")
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Values[0].Int64(); !ok || value != 2 {
		t.Fatalf("post-patch channel value = %#v", result.Values)
	}
}

func TestReflectReferenceIdentitySurvivesCompatiblePatch(t *testing.T) {
	const root = "minigo.test/reference-patch"
	const source = `package main
import "reflect"
var data = []int{1,2,3}
var table = map[string]int{"a":1}
var savedSlice = reflect.ValueOf(data[1:])
var savedPointer = reflect.ValueOf(&data[1])
var savedMap = reflect.ValueOf(table)
func Run() int {
 if !reflect.SameReference(savedSlice,reflect.ValueOf(data[1:])) || !reflect.SameReference(savedPointer,reflect.ValueOf(&data[1])) || !reflect.SameReference(savedMap,reflect.ValueOf(table)) { return -1 }
 if reflect.SameReference(savedSlice,reflect.ValueOf(data[:2])) || reflect.SameReference(savedMap,reflect.ValueOf(map[string]int{"a":1})) { return -2 }
 return VERSION
}`
	entries := []compiler.EntryPoint{{Name: "run", ModulePath: root, Function: "Run"}}
	before := prepareStdlibProgram(t, root, strings.ReplaceAll(source, "VERSION", "1"), entries)
	after := prepareStdlibProgram(t, root, strings.ReplaceAll(source, "VERSION", "2"), entries)
	instance, err := before.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for version := int64(1); version <= 2; version++ {
		if version == 2 {
			plan, err := instance.PreparePatch(context.Background(), after)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := instance.ApplyPatch(plan); err != nil {
				t.Fatal(err)
			}
		}
		result, err := instance.Call(context.Background(), "run")
		if err != nil {
			t.Fatalf("revision %d: %v", version, err)
		}
		if got, ok := result.Values[0].Int64(); !ok || got != version {
			t.Fatalf("revision %d reference identity: %v", version, result.Values)
		}
	}
}
