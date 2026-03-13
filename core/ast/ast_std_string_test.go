package ast

import (
	"testing"
)

func TestMiniString(t *testing.T) {
	s1 := NewMiniString("hello world")
	s2 := NewMiniString(" go-mini")

	t.Run("Len", func(t *testing.T) {
		res := s1.Len()
		if res.data != 11 {
			t.Errorf("expected length 11, got %d", res.data)
		}
	})

	t.Run("Plus", func(t *testing.T) {
		res := s1.Plus(&s2)
		if res.GoString() != "hello world go-mini" {
			t.Errorf("expected 'hello world go-mini', got '%s'", res.GoString())
		}
	})

	t.Run("Base64", func(t *testing.T) {
		encoded := s1.Base64Encode()
		decoded := encoded.Base64Decode()
		if decoded.GoString() != s1.GoString() {
			t.Errorf("Base64 roundtrip failed")
		}
	})

	t.Run("Substring", func(t *testing.T) {
		start := NewMiniInt64(0)
		length := NewMiniInt64(5)
		res := s1.Substring(&start, &length)
		if res.GoString() != "hello" {
			t.Errorf("expected hello, got %s", res.GoString())
		}
	})

	t.Run("ChangeCase", func(t *testing.T) {
		up := NewMiniInt64(1)
		resUp := s1.ChangeCase(&up)
		if resUp.GoString() != "HELLO WORLD" {
			t.Errorf("expected HELLO WORLD, got %s", resUp.GoString())
		}
		down := NewMiniInt64(2)
		resDown := s1.ChangeCase(&down)
		if resDown.GoString() != "hello world" {
			t.Errorf("expected hello world, got %s", resDown.GoString())
		}
	})

	t.Run("Trim", func(t *testing.T) {
		s := NewMiniString("  hello  ")
		cutset := NewMiniString(" ")
		res := s.Trim(&cutset)
		if res.GoString() != "hello" {
			t.Errorf("expected hello, got '%s'", res.GoString())
		}
	})
}
