package types

import "testing"

func TestUnderlyingConversionIgnoresTagsOnly(t *testing.T) {
	table := NewTable()
	p := NewParser("example", table)
	left, err := p.Parse("struct{X:struct{Y:Int `json:\"a\"`}}")
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := View(table, left).StructFields()
	if fields[0].Tag != "" || View(table, fields[0].Type).Shape() != Struct {
		t.Fatal("nested field tag was attached to the outer field")
	}
	inner, _ := View(table, fields[0].Type).StructFields()
	if inner[0].Tag != `json:"a"` {
		t.Fatalf("nested tag: %q", inner[0].Tag)
	}
	for _, test := range []struct {
		typ         string
		convertible bool
	}{
		{"struct{X:struct{Y:Int `json:\"b\"`}}", true},
		{"struct{X:struct{Z:Int `json:\"a\"`}}", false},
		{"struct{X:struct{Y:String `json:\"a\"`}}", false},
	} {
		right, err := p.Parse(test.typ)
		if err != nil {
			t.Fatal(err)
		}
		r := NewRelations(table)
		if got := r.UnderlyingIdenticalIgnoringTags(left, right).OK; got != test.convertible {
			t.Fatalf("%s: %v", test.typ, got)
		}
		if r.Identical(left, right).OK || r.Assignable(left, right).OK {
			t.Fatalf("conversion widened identity or assignment: %s", test.typ)
		}
	}
}
