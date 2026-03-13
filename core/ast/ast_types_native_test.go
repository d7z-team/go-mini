package ast

import (
	"testing"
)

func TestMiniFloat32(t *testing.T) {
	val1 := NewMiniFloat32(1.5)
	val2 := NewMiniFloat32(2.5)

	// Test Data()
	if val1.Data() != 1.5 {
		t.Errorf("expected 1.5, got %f", val1.Data())
	}

	// Test OPSType()
	if val1.OPSType() != "Float32" {
		t.Errorf("expected Float32, got %s", val1.OPSType())
	}

	// Test Clone()
	clone := val1.Clone().(*MiniFloat32)
	if clone.Data() != val1.Data() {
		t.Errorf("Clone failed")
	}

	// Test String()
	str := val1.String()
	if str.GoString() != "1.500000" {
		t.Errorf("expected 1.500000, got %s", str.GoString())
	}

	// Test GoValue()
	if val1.GoValue() != float32(1.5) {
		t.Errorf("expected float32(1.5), got %v", val1.GoValue())
	}

	// Test Operations
	sum := val1.Plus(&val2)
	if sum.Data() != 4.0 {
		t.Errorf("Plus failed: %f", sum.Data())
	}

	diff := val2.Minus(&val1)
	if diff.Data() != 1.0 {
		t.Errorf("Minus failed: %f", diff.Data())
	}

	prod := val1.Mult(&val2)
	if prod.Data() != 3.75 {
		t.Errorf("Mult failed: %f", prod.Data())
	}

	div := val2.Div(&val1)
	// 2.5 / 1.5 = 1.6666666
	if div.Data() < 1.66 || div.Data() > 1.67 {
		t.Errorf("Div failed: %f", div.Data())
	}

	// Test Comparisons
	if val1.Eq(&val2).data != false {
		t.Errorf("Eq failed")
	}
	if val1.Neq(&val2).data != true {
		t.Errorf("Neq failed")
	}
	if val1.Lt(&val2).data != true {
		t.Errorf("Lt failed")
	}
	if val1.Gt(&val2).data != false {
		t.Errorf("Gt failed")
	}
	if val1.Le(&val2).data != true {
		t.Errorf("Le failed")
	}
	if val1.Ge(&val2).data != false {
		t.Errorf("Ge failed")
	}

	// Test New()
	newVal, err := val1.New("3.14")
	if err != nil {
		t.Errorf("New failed: %v", err)
	}
	if newVal.(*MiniFloat32).Data() != 3.14 {
		t.Errorf("expected 3.14, got %f", newVal.(*MiniFloat32).Data())
	}
}

func TestMiniComplex64(t *testing.T) {
	val1 := NewMiniComplex64(complex(1, 2))
	val2 := NewMiniComplex64(complex(3, 4))

	if val1.Data() != complex(1, 2) {
		t.Errorf("expected (1+2i), got %v", val1.Data())
	}

	if val1.OPSType() != "Complex64" {
		t.Errorf("expected Complex64, got %s", val1.OPSType())
	}

	str := val1.String()
	if str.GoString() != "(1+2i)" {
		t.Errorf("expected (1+2i), got %s", str.GoString())
	}

	sum := val1.Plus(&val2)
	if sum.Data() != complex(4, 6) {
		t.Errorf("Plus failed: %v", sum.Data())
	}

	diff := val2.Minus(&val1)
	if diff.Data() != complex(2, 2) {
		t.Errorf("Minus failed: %v", diff.Data())
	}

	eq := val1.Eq(&val2)
	if eq.data != false {
		t.Errorf("Eq failed")
	}

	neq := val1.Neq(&val2)
	if neq.data != true {
		t.Errorf("Neq failed")
	}

	_, err := val1.New("1+2i")
	if err == nil {
		t.Errorf("Complex New should return error")
	}
}
