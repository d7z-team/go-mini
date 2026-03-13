package test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestFull(t *testing.T) {
	_, err := utils.TestGoCode(`
type Person struct {
	Name string
	Age  int
}

func (this *Person) String() (string,int) {
   return this.Name ,this.Age
}

func Add(a, b int) int {
	return a + b
}

func main() {
	x := 10
	y := 20
	result := Add(x, y)
	if result > 30 {
		println("Large")
	} else {
		println("Small")
	}
	for i := 0; i < 10; i++ {
		println(i)
	}
	person:=&Person{
		Name: "Alice",
		Age:  18,
	}
	data := person.String()
	println(data)
}
`)
	assert.NoError(t, err)
}

func TestExpr(t *testing.T) {
	t.Run("var", func(t *testing.T) {
		result, err := utils.TestGoExpr(`a:=1;b=a+1;push(a);a=b;push(b)`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"1", "2"}, result)
	})

	t.Run("if", func(t *testing.T) {
		data, err := utils.TestGoExpr(`
a:=100
b:=200
if a > b {
push("100>200")
}else{
push("100<200")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"100<200"}, data)
	})
	t.Run("bool", func(t *testing.T) {
		data, err := utils.TestGoExpr(`
a := true
b := false
if a {
    push("a is true")
}
if b {
    push("b is true")
} else {
    push("b is false")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"a is true", "b is false"}, data)
	})
	t.Run("for", func(t *testing.T) {
		data, err := utils.TestGoExpr(`
for i:=0;i<5;i++ {
	push(i)
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"0", "1", "2", "3", "4"}, data)
	})
	t.Run("for-func", func(t *testing.T) {
		data, err := utils.TestGoCode(`
func id() int{
  return 5
}
func main(){
for i:=0;i<id();i++ {
	push(i)
}
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"0", "1", "2", "3", "4"}, data)
	})
	t.Run("for-break", func(t *testing.T) {
		data, err := utils.TestGoExpr(`
for i:=0;i<10;i++ {
	if i == 5 {
	break
	}
	push(i)
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"0", "1", "2", "3", "4"}, data)
	})

	t.Run("for-continue", func(t *testing.T) {
		data, err := utils.TestGoExpr(`
for i:=0;i<10;i++ {
if i == 5 {
	continue
	}
	push(i)
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"0", "1", "2", "3", "4", "6", "7", "8", "9"}, data)
	})

	t.Run("struct", func(t *testing.T) {
		result, err := utils.TestGoCode(`
type Person struct {
	Name string
	Age  int
}
func (this *Person) Info() (string,int) {
   return this.Name,this.Age
}
func main(){
person:=&Person{
		Name: "Alice",
		Age:  18,
	}
	push(person.Name)
	push(person.Age)
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"Alice", "18"}, result)
	})

	t.Run("Append", func(t *testing.T) {
		expr, err := utils.TestGoExpr(`
var a=1
var b=2
var c="摄氏度"
push(a+b)
push(b.String()+c)
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"3", "2摄氏度"}, expr)
	})

	t.Run("Float", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
a := 1.5
b := 2.5
push(a + b)
push(a * b)
push(b - a)
push(b / a)
`)
		assert.NoError(t, err)
		// Assuming MiniFloat.String() or GoValue() produces these strings
		assert.Contains(t, result, "4")
		assert.Contains(t, result, "3.75")
		assert.Contains(t, result, "1")
		// 2.5 / 1.5 = 1.666...
	})

	t.Run("Boolean-Logic", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
a := true
b := false
if a && b {
    push("both")
}
if a || b {
    push("either")
}
if !b {
    push("not b")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"either", "not b"}, result)
	})

	t.Run("Switch", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
x := 2
switch x {
case 1:
    push("one")
case 2:
    push("two")
default:
    push("other")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"two"}, result)
	})

	t.Run("Defer", func(t *testing.T) {
		result, err := utils.TestGoCode(`
func main() {
    defer push("deferred")
    push("normal")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"normal", "deferred"}, result)
	})

	t.Run("Range-Array", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
arr := []int{1, 2, 3}
for i, v := range arr {
    push_num(v)
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"1", "2", "3"}, result)
	})

	t.Run("Return-Path-If", func(t *testing.T) {
		_, err := utils.TestGoCode(`
func check(n int) string {
    if n > 0 {
        return "positive"
    } else {
        return "non-positive"
    }
}
func main() {
    push(check(10))
}
`)
		assert.NoError(t, err)
	})

	t.Run("Return-Path-Error", func(t *testing.T) {
		_, err := utils.TestGoCode(`
func check(n int) string {
    if n > 0 {
        return "positive"
    }
}
func main() {}
`)
		assert.Error(t, err)
	})

	t.Run("Multi-Assignment", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
a, b := 1, 2
push_num(a)
push_num(b)
a, b = 10, 20
push_num(a)
push_num(b)
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"1", "2", "10", "20"}, result)
	})

	t.Run("Pointers", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
x := 100
p := &x
push_num(*p)
*p = 200
push_num(x)
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"100", "200"}, result)
	})
}

func TestUnmarshalJSON(t *testing.T) {
	e := engine.NewMiniExecutor()
	e.MustAddFunc("println", func(s *ast.MiniString) {})
	// Minimal ProgramStmt in JSON
	jsonData := []byte(`{
		"meta": "boot",
		"id": "test",
		"package": "main",
		"constants": {},
		"variables": {},
		"structs": {},
		"functions": {
			"main": {
				"meta": "function",
				"id": "main",
				"name": "main",
				"return": "Void",
				"body": {
					"meta": "block",
					"id": "body",
					"children": [
						{
							"meta": "call",
							"id": "call",
							"func": {
								"meta": "const_ref",
								"id": "func",
								"name": "println"
							},
							"args": [
								{
									"meta": "literal",
									"id": "lit",
									"type": "String",
									"value": "hello from json"
								}
							]
						}
					]
				}
			}
		},
		"main": []
	}`)

	prog, err := e.NewRuntimeByJSON(jsonData)
	assert.NoError(t, err)
	assert.NotNil(t, prog)
}

func TestAStruct(t *testing.T) {
	t.Run("struct", func(t *testing.T) {
		ret, err := utils.TestGoCode(`
type Person struct {
Id int
}
func (this *Person) Count() int {
	return this.Id
}
func main(){
	p := &Person{ Id: 5}
	for i:=0;i<p.Count();i++ {
	push(i)
}
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"0", "1", "2", "3", "4"}, ret)
	})
}

func TestEmptyStruct(t *testing.T) {
	_, err := utils.TestGoCode(`
func main(){
	var test1 String
	var test2 *String
}
`)
	assert.NoError(t, err)
}

func TestMiniStringMethods(t *testing.T) {
	result, err := utils.TestGoExpr(`
s := "hello world"
push(s.Contains("hello"))
push(s.HasPrefix("hel"))
push(s.HasSuffix("rld"))
push(s.Index("o"))
push(s.Repeat(2))
push(s.Replace("world", "mini", 1))
push(s.ReplaceAll("l", "x"))
push(s.Trim("  h  "))

parts := "a,b,c".Split(",")
push(parts.length())
push(parts[0])
push(parts[1])
push(parts[2])

s2 := "!!hello!!"
push(s2.TrimLeft("!"))
push(s2.TrimRight("!"))
push(s2.TrimPrefix("!!"))
push(s2.TrimSuffix("!!"))
`)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"true", "true", "true", "4", "hello worldhello world", "hello mini", "hexxo worxd", "ello world",
		"3", "a", "b", "c",
		"hello!!", "!!hello", "hello!!", "!!hello",
	}, result)
}

func TestNilPointerComparison(t *testing.T) {
	result, err := utils.TestGoCode(`
package main

import "mini"

func main() {
    var str *string
    if str == nil {
        push("String is nil")
    } else {
        push("String is not nil")
    }
}
`)
	assert.NoError(t, err)
	assert.Equal(t, []string{"String is nil"}, result)
}
