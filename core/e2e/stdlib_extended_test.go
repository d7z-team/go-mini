package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestExtendedStdlib(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	import "strconv"
	import "bytes"
	import "sort"
	import "regexp"
	import "unicode/utf8"
	import "math/rand"
	import "encoding/base64"
	import "encoding/hex"
	import "crypto/md5"
	import "crypto/sha256"
	import "net/url"

	func main() {
		// 1. strconv
		if strconv.Itoa(123) != "123" { panic("strconv.Itoa failed") }
		i, _ := strconv.Atoi("456")
		if i != 456 { panic("strconv.Atoi failed") }

		// 2. bytes
		b := []byte("hello")
		if !bytes.Contains(b, []byte("ell")) { panic("bytes.Contains failed") }

		// 3. sort
		ints := []Int64{3, 1, 2}
		ints = sort.Ints(ints)
		if ints[0] != 1 || ints[1] != 2 || ints[2] != 3 { panic("sort.Ints failed") }

		// 4. regexp
		match, _ := regexp.MatchString("a.c", "abc")
		if !match { panic("regexp.MatchString failed") }

		// 5. unicode/utf8
		if utf8.RuneCountInString("你好") != 2 { panic("utf8.RuneCountInString failed") }

		// 6. math/rand
		rand.Seed(1)
		r1 := rand.Float64()
		if r1 < 0.0 || r1 > 1.0 { panic("rand.Float64 failed") }

		// 7. encoding/base64
		enc := base64.EncodeToString([]byte("hello"))
		if enc != "aGVsbG8=" { panic("base64 failed") }

		// 8. encoding/hex
		h := hex.EncodeToString([]byte("abc"))
		if h != "616263" { panic("hex failed") }

		// 9. crypto
		m := md5.Sum([]byte("abc"))
		if hex.EncodeToString(m) != "900150983cd24fb0d6963f7d28e17f72" { panic("md5 failed") }
		
		s := sha256.Sum256([]byte("abc"))
		if hex.EncodeToString(s) != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" { panic("sha256 failed") }

		// 10. net/url
		esc := url.QueryEscape("a b")
		if esc != "a+b" { panic("url.QueryEscape failed") }
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
