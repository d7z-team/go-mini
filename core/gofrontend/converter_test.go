package gofrontend

import (
	"strings"
	"testing"
)

func TestCanonicalBuiltinTypeNameCoversGoIntegerAliases(t *testing.T) {
	converter := NewConverter()
	for _, name := range []string{"uint64", "uintptr", "byte", "rune"} {
		if got := converter.canonicalBuiltinTypeName(name); got != "Int64" {
			t.Fatalf("expected %s to normalize to Int64, got %s", name, got)
		}
	}
}

func TestConvertSourceReportsUnsupportedConstructsWithoutPanic(t *testing.T) {
	cases := []struct {
		name string
		code string
		want string
	}{
		{
			name: "new string literal type",
			code: `package main
func main() {
	_ = new("Int64")
}`,
			want: "new 第一个参数必须是类型",
		},
		{
			name: "range key index target",
			code: `package main
func main() {
	arr := []Int64{1}
	for arr[0] = range arr {}
}`,
			want: "range 的 key 目标只支持标识符",
		},
		{
			name: "fallthrough",
			code: `package main
func main() {
	switch 1 {
	case 1:
		fallthrough
	}
}`,
			want: "fallthrough",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ConvertSource panicked: %v", r)
				}
			}()
			_, err := NewConverter().ConvertSource("bad.mgo", tc.code)
			if err == nil {
				t.Fatal("expected conversion error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}
