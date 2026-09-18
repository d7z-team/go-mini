package stdlib_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"testing"

	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

type jsonFuzzReader struct {
	text  string
	chunk int
}

func (r *jsonFuzzReader) Read(dst []byte) (int, error) {
	if r.text == "" {
		return 0, io.EOF
	}
	n := min(len(dst), len(r.text), r.chunk)
	copy(dst, r.text[:n])
	r.text = r.text[n:]
	return n, nil
}

func FuzzJSONStreamFieldsAndOptions(f *testing.F) {
	f.Add(`{"values":[1e9999,2],"name":"later"} {"count":1}`, uint8(1), false, false)
	for _, text := range []string{`{"name":"<世界>","count":127,"values":[9007199254740993]} true`, `{"count":128,"name":"later"} {"count":1}`, `{"count":null} {"name":"\uD834\uDD1E"}`, `{"values":[1e-9999,1e9999]}`, `{"name":`} {
		f.Add(text, uint8(1), true, true)
	}
	program := prepareStdlibRunProgram(f, "minigo.test/json-stream-fuzz", `package main
import ("bytes";"encoding/json";"io";"strconv")
type reader struct { text string; chunk int }
func (r *reader) Read(dst []byte) (int,error) {
	if r.text == "" { return 0,io.EOF }
	n:=min(len(dst),len(r.text),r.chunk);copy(dst,r.text[:n]);r.text=r.text[n:];return n,nil
}
type record struct { Name string; Count int8; Values []any }
func Run(input string, chunk int, escape bool, numbers bool) string {
	d:=json.NewDecoder(&reader{text:input,chunk:chunk});if numbers { d.UseNumber() }
	out:=""
	for i:=0;i<3;i++ {
		value:=record{};err:=d.Decode(&value);kind:="ok"
		if err==io.EOF { return out+"eof" }
		if err!=nil { kind="error";if _,ok:=err.(*json.UnmarshalTypeError);ok {kind="type"};if _,ok:=err.(*json.SyntaxError);ok {kind="syntax"} }
		var buffer bytes.Buffer;e:=json.NewEncoder(&buffer);e.SetEscapeHTML(escape)
		if e.Encode(value)!=nil { return "encode-error" }
		out+=kind+":"+strconv.FormatInt(d.InputOffset(),10)+":"+buffer.String()
		if kind!="ok" && kind!="type" {return out}
	}
	return out
}`)
	f.Fuzz(func(t *testing.T, input string, size uint8, escape, numbers bool) {
		if len(input) > 512 {
			t.Skip()
		}
		chunk := int(size%8) + 1
		decoder := json.NewDecoder(&jsonFuzzReader{text: input, chunk: chunk})
		if numbers {
			decoder.UseNumber()
		}
		want := ""
		for range 3 {
			value := struct {
				Name   string
				Count  int8
				Values []any
			}{}
			err := decoder.Decode(&value)
			kind := "ok"
			if err == io.EOF {
				want += "eof"
				break
			}
			if err != nil {
				kind = "error"
				if _, ok := err.(*json.UnmarshalTypeError); ok {
					kind = "type"
				}
				if _, ok := err.(*json.SyntaxError); ok {
					kind = "syntax"
				}
			}
			var buffer bytes.Buffer
			encoder := json.NewEncoder(&buffer)
			encoder.SetEscapeHTML(escape)
			if encoder.Encode(value) != nil {
				want = "encode-error"
				break
			}
			want += kind + ":" + strconv.FormatInt(decoder.InputOffset(), 10) + ":" + buffer.String()
			if kind != "ok" && kind != "type" {
				break
			}
		}
		instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		result, err := instance.Call(context.Background(), "run", minigoruntime.HostString(input), minigoruntime.HostInt("Int", int64(chunk)), minigoruntime.HostBool(escape), minigoruntime.HostBool(numbers))
		if err != nil {
			t.Fatal(err)
		}
		got, ok := result.Values[0].StringValue()
		if !ok || got != want {
			t.Fatalf("stream mismatch: got %q want %q", got, want)
		}
	})
}
