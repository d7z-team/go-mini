package bootstrap_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/bootstrap/compilerentry"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func TestCompilerImageCompilesAndRunsSource(t *testing.T) {
	if testing.Short() {
		t.Skip("bootstrap compiler execution is an integration test")
	}
	image := buildCompilerImage(t)
	compilerInstance := instantiateCompilerImage(t, image)
	request := compilerentry.Request{
		Format: compilerentry.ServiceFormat, Version: compilerentry.ServiceVersion,
		Operation: "prepare", Root: "example/main",
		Packages: []compilerentry.Package{{
			Namespace:   "module:example",
			PackagePath: "main",
			ModulePath:  "example/main",
			Files: []compilerentry.File{
				{Path: "main.mgo", Text: "package main\nimport _ \"embed\"\n//go:embed answer.txt\nvar answer string\nfunc Answer() string { return answer + mode() }\nfunc Branch() int { if value := 1; value > 0 { return value }; return 0 }\n"},
				{Path: "mode_default.mgo", Text: "//go:build !feature\n\npackage main\nfunc mode() string { return \"-default\" }\n"},
				{Path: "mode_feature.mgo", Text: "//go:build feature\n\npackage main\nfunc mode() string { return \"-feature\" }\n"},
			},
			Resources: []compilerentry.Resource{{Path: "answer.txt", Data: []byte("42")}},
		}},
		EntryPoints: []compiler.EntryPoint{
			{Name: "answer", ModulePath: "example/main", Function: "Answer"},
			{Name: "branch", ModulePath: "example/main", Function: "Branch"},
		},
	}
	var input []byte
	var response compilerentry.Response
	var err error
	for _, tags := range [][]string{nil, {"feature"}} {
		candidate := request
		candidate.Tags = tags
		input, err = json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		response = callCompilerImage(t, compilerInstance, input)
		if response.Error != "" || response.Image == nil || len(response.Diagnostics) != 0 {
			t.Fatalf("compiler response for tags %v = %#v", tags, response)
		}
		if response.CompilerID != compiler.Identity() {
			t.Fatalf("compiler identity = %q, want %q", response.CompilerID, compiler.Identity())
		}
		compareCompilerResponse(t, response, compilerentry.Execute(candidate))
	}
	unknown := append(append([]byte(nil), input[:len(input)-1]...), []byte(`,"Unknown":true}`)...)
	for _, malformed := range [][]byte{unknown, append(append([]byte(nil), input...), []byte(" {}")...)} {
		bootstrapResponse := callCompilerImage(t, compilerInstance, malformed)
		var nativeResponse compilerentry.Response
		if err := json.Unmarshal(compilerentry.Compile(malformed), &nativeResponse); err != nil {
			t.Fatal(err)
		}
		if bootstrapResponse.Format != compilerentry.ServiceFormat || bootstrapResponse.Version != compilerentry.ServiceVersion || bootstrapResponse.CompilerID != compiler.Identity() {
			t.Fatalf("bootstrap error envelope = %#v", bootstrapResponse)
		}
		if bootstrapResponse.Error == "" || bootstrapResponse.Error != nativeResponse.Error {
			t.Fatalf("bootstrap error %q differs from native error %q", bootstrapResponse.Error, nativeResponse.Error)
		}
	}
	compiled, err := minigoruntime.LoadExecutionImage(*response.Image)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := compiled.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close generated instance: %v", err)
		}
	})
	answer, err := instance.Call(context.Background(), "answer")
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Values) != 1 {
		t.Fatalf("answer = %#v", answer.Values)
	}
	value, ok := answer.Values[0].StringValue()
	if !ok || value != "42-feature" {
		t.Fatalf("answer = %#v", answer.Values[0])
	}
}
