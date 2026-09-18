package bootstrap

import (
	"context"
	"os"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/workspace"
	rt "github.com/d7z-team/mini-go/runtime"
)

func TestExactConstantRepresentabilityInVM(t *testing.T) {
	sources, session, err := newCompilerSession(BuildOptions{Filesystem: os.DirFS("../.."), Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	session.Close()
	sources, err = workspace.Overlay(sources, []workspace.SourceChange{{ModulePath: "probe", Path: "main.mgo", Text: `package main
import "github.com/d7z-team/mini-go/compiler/constant"
import "strconv"
func Run() string {
 value,_:=constant.NewRational("1","1")
 cutoff,_:=constant.SubtractUnsignedDecimal(constant.Pow2UnsignedDecimal(128),constant.Pow2UnsignedDecimal(103))
 limit,_:=constant.NewRational(cutoff,"1")
 return strconv.FormatBool(constant.FloatRepresentable(value,32))+":"+cutoff+":"+strconv.Itoa(constant.CompareRational(value,limit))
}
`}})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := compiler.Prepare(compiler.Request{Root: "probe", Sources: sources, EntryPoints: []compiler.EntryPoint{{Name: "run", ModulePath: "probe", Function: "Run"}}})
	if err != nil || !prepared.Checked.OK() {
		t.Fatalf("prepare %v %#v", err, prepared.Checked.Diagnostics)
	}
	program, err := rt.LoadExecutionImage(*prepared.Image)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := program.Instantiate(context.Background(), rt.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Call(context.Background(), "run")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := result.Values[0].StringValue()
	if got != "true:340282356779733661637539395458142568448:-1" {
		t.Fatalf("representability: %s", got)
	}
}
