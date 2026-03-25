package e2e

import (
	"context"
	"os"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestFileExtendedAPI(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	t.Run("WriteStringAndName", func(t *testing.T) {
		code := `
			package main
			import "os"
			func main() {
				f, err := os.Create("test_1.txt")
				if err != nil { panic(err) }
				
				n, err := f.WriteString("hello")
				if err != nil { panic(err) }
				if n != 5 { panic("wrong n") }
				if f.Name() != "test_1.txt" { panic("wrong name") }
				
				f.Close()
			}
		`
		runScript(t, executor, code)
		os.Remove("test_1.txt")
	})

	t.Run("SeekAndReadAll", func(t *testing.T) {
		os.WriteFile("test_2.txt", []byte("0123456789"), 0644)
		defer os.Remove("test_2.txt")
		code := `
			package main
			import "os"
			import "io"
			func main() {
				f, err := os.Open("test_2.txt")
				if err != nil { panic(err) }
				
				pos, err := f.Seek(5, 0)
				if err != nil { panic(err) }
				if pos != 5 { panic("wrong pos") }
				
				// ReadAll 是正确的数据获取方式
				data, err := io.ReadAll(f)
				if err != nil { panic(err) }
				if String(data) != "56789" { panic("wrong data: " + String(data)) }
				
				f.Close()
			}
		`
		runScript(t, executor, code)
	})

	t.Run("WriteAtAndTruncate", func(t *testing.T) {
		code := `
			package main
			import "os"
			func main() {
				f, err := os.Create("test_3.txt")
				if err != nil { panic(err) }
				
				f.WriteString("AAAAA")
				
				// WriteAt 是正常的，因为只需要从脚本读取内存
				n, err := f.WriteAt(TypeBytes("BB"), 2)
				if err != nil { panic(err) }
				if n != 2 { panic("wrong n") }
				
				f.Sync()
				f.Close()
				
				data, err := os.ReadFile("test_3.txt")
				if String(data) != "AABBA" { panic("wrong data: " + String(data)) }
				
				f2, err := os.OpenFile("test_3.txt", 2, 0644) // O_RDWR
				if err != nil { panic(err) }
				f2.Truncate(3)
				f2.Close()
				
				data2, err := os.ReadFile("test_3.txt")
				if String(data2) != "AAB" { panic("wrong truncate: " + String(data2)) }
			}
		`
		runScript(t, executor, code)
		os.Remove("test_3.txt")
	})

	t.Run("IOTools", func(t *testing.T) {
		code := `
			package main
			import "os"
			import "io"
			func main() {
				src, _ := os.Create("src.txt")
				io.WriteString(src, "copy me")
				src.Seek(0, 0)
				
				dst, _ := os.Create("dst.txt")
				
				// 测试 io.Copy (完全在宿主侧完成，效率最高)
				n, err := io.Copy(dst, src)
				if err != nil { panic(err) }
				if n != 7 { panic("wrong n: " + String(n)) }
				
				src.Close()
				dst.Close()
				
				data, _ := os.ReadFile("dst.txt")
				if String(data) != "copy me" { panic("copy failed") }
				
				// 测试 io.ReadAll 直接处理 []byte
				data2, _ := io.ReadAll(TypeBytes("bytes"))
				if String(data2) != "bytes" { panic("ReadAll bytes failed") }
			}
		`
		runScript(t, executor, code)
		os.Remove("src.txt")
		os.Remove("dst.txt")
	})
}

func runScript(t *testing.T, executor *engine.MiniExecutor, code string) {
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}
