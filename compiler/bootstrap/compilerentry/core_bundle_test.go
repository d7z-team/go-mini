package compilerentry

import (
	"reflect"
	"testing"
)

func TestCoreBundleRoundTrip(t *testing.T) {
	packages := []Package{{
		Namespace: "std", PackagePath: "example", ModulePath: "example",
		Files:     []File{{Path: "example/main.mgo", Text: "package example\n"}},
		TestFiles: []File{{Path: "example/main_test.mgo", Text: "package example\n"}},
		Resources: []Resource{
			{Path: "source.mgo", SourcePath: "example/main.mgo"},
			{Path: "nil.bin"},
			{Path: "empty.bin", Data: []byte{}},
			{Path: "data.bin", Data: []byte{0, 0xff, '\n'}},
		},
	}}
	bundle, err := EncodeCoreBundle(packages)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCoreBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, packages) {
		t.Fatalf("decoded packages = %#v, want %#v", decoded, packages)
	}
}

func TestCoreBundleRejectsInvalidEncoding(t *testing.T) {
	bundle, err := EncodeCoreBundle([]Package{{Namespace: "std", PackagePath: "example", ModulePath: "example"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "header", data: []byte("invalid")},
		{name: "truncated", data: bundle[:len(bundle)-1]},
		{name: "trailing", data: append(append([]byte(nil), bundle...), 0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeCoreBundle(test.data); err == nil {
				t.Fatal("invalid core bundle was accepted")
			}
		})
	}
}
