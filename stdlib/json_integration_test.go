package stdlib_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func TestJSONPreservesBinaryDataAndInvalidUTF8(t *testing.T) {
	const root = "example/json"
	program := prepareStdlibProgram(t, root, `package main

import (
	"crypto/sha256"
	"encoding/json"
)

func Decode() []byte {
	var value []byte
	if err := json.Unmarshal([]byte("\"AQ0CFQD/\""), &value); err != nil { return nil }
	return value
}

func Hash() []byte {
	value := sha256.Sum256([]byte{1, 13, 2, 21, 0, 255})
	return value[:]
}

func MarshalInvalidUTF8() []byte {
	value, err := json.Marshal(string([]byte{'a', 0xff, 'b'}))
	if err != nil { return nil }
	return value
}
	`, []compiler.EntryPoint{
		{Name: "decode", ModulePath: root, Function: "Decode"},
		{Name: "hash", ModulePath: root, Function: "Hash"},
		{Name: "marshal_invalid_utf8", ModulePath: root, Function: "MarshalInvalidUTF8"},
	})
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close instance: %v", err)
		}
	})
	callBytes := func(entry string) []byte {
		result, err := instance.Call(context.Background(), entry)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Values) != 1 {
			t.Fatalf("%s returned %#v", entry, result.Values)
		}
		value, ok := result.Values[0].Bytes()
		if !ok {
			t.Fatalf("%s returned %#v", entry, result.Values[0])
		}
		return value
	}

	got := callBytes("decode")
	want := []byte{1, 13, 2, 21, 0, 255}
	if !bytes.Equal(got, want) {
		t.Fatalf("Decode returned %v, want %v", got, want)
	}
	gotHash := callBytes("hash")
	wantHash := sha256.Sum256(want)
	if !bytes.Equal(gotHash, wantHash[:]) {
		t.Fatalf("Hash returned %x, want %x", gotHash, wantHash)
	}
	gotJSON := callBytes("marshal_invalid_utf8")
	wantJSON := []byte(`"a\ufffdb"`)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("MarshalInvalidUTF8 returned %q, want %q", gotJSON, wantJSON)
	}
}
