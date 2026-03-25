package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestFilepathLibrary(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	import "filepath"

	func main() {
		// Test Join
		p := filepath.Join("a", "b", "c")
		// filepath.Join result is OS dependent, but on Linux it is a/b/c
		
		// Test Base
		if filepath.Base(p) != "c" { panic("Base failed") }
		
		// Test Dir
		if filepath.Dir(p) != filepath.Join("a", "b") { panic("Dir failed") }
		
		// Test Ext
		if filepath.Ext("test.txt") != ".txt" { panic("Ext failed") }
		
		// Test Clean
		if filepath.Clean("a/b/../c") != filepath.Join("a", "c") { panic("Clean failed") }
		
		// Test Split
		dir, file := filepath.Split("a/b/c.txt")
		if dir != "a/b/" || file != "c.txt" { panic("Split failed") }
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
