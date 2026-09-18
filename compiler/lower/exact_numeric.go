package lower

import (
	"encoding/json"
	"strings"

	"github.com/d7z-team/mini-go/compiler/constant"
)

type exactRational = constant.Rational

type exactComplex struct {
	realPart      exactRational
	imaginaryPart exactRational
}

func foldExactRationalBinary(operator string, left, right exactRational) (exactRational, bool) {
	switch operator {
	case "+":
		return constant.AddRational(left, right)
	case "-":
		return constant.SubtractRational(left, right)
	case "*":
		return constant.MultiplyRational(left, right)
	case "/":
		return constant.DivideRational(left, right)
	default:
		return exactRational{}, false
	}
}

func foldExactRationalCompare(operator string, left, right exactRational) (json.RawMessage, bool) {
	comparison := constant.CompareRational(left, right)
	switch operator {
	case "==":
		return boolRaw(comparison == 0), true
	case "!=":
		return boolRaw(comparison != 0), true
	case "<":
		return boolRaw(comparison < 0), true
	case "<=":
		return boolRaw(comparison <= 0), true
	case ">":
		return boolRaw(comparison > 0), true
	case ">=":
		return boolRaw(comparison >= 0), true
	default:
		return nil, false
	}
}

func foldExactComplexBinary(operator string, left, right exactComplex) (exactComplex, bool) {
	value, ok := constant.Binary(operator,
		constant.Value{Real: left.realPart.String(), Imag: left.imaginaryPart.String(), Type: "Complex128"},
		constant.Value{Real: right.realPart.String(), Imag: right.imaginaryPart.String(), Type: "Complex128"})
	if !ok {
		return exactComplex{}, false
	}
	realPart, realOK := constant.ParseRationalLiteral(value.Real)
	imaginaryPart, imagOK := constant.ParseRationalLiteral(value.Imag)
	return exactComplex{realPart: realPart, imaginaryPart: imaginaryPart}, realOK && imagOK
}

func parseExactIntegerLiteral(text string) (string, bool) {
	value, ok := constant.ParseIntegerLiteral(text)
	return value.Text, ok
}

func parseExactRationalLiteral(text string) (exactRational, bool) {
	return constant.ParseRationalLiteral(text)
}

func parseExactImaginaryLiteral(text string) (exactComplex, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasSuffix(text, "i") {
		return exactComplex{}, false
	}
	imagPart, ok := parseExactRationalLiteral(strings.TrimSuffix(text, "i"))
	if !ok {
		return exactComplex{}, false
	}
	zero, _ := constant.NewRational("0", "1")
	return exactComplex{realPart: zero, imaginaryPart: imagPart}, true
}

func exactRationalText(value exactRational) string {
	if !value.Valid() {
		return ""
	}
	return value.Numerator + "/" + value.Denominator
}

func exactRationalRaw(value exactRational) (json.RawMessage, bool) {
	encoded, err := json.Marshal(exactRationalText(value))
	return json.RawMessage(encoded), err == nil
}

func exactIntegerJSONRaw(value string) (json.RawMessage, bool) {
	value, ok := constant.NormalizeSignedDecimal(value)
	if !ok {
		return nil, false
	}
	if _, ok := constant.SignedDecimalInt64(value); ok {
		return json.RawMessage(value), true
	}
	encoded, err := json.Marshal(value)
	return json.RawMessage(encoded), err == nil
}

func exactComplexRaw(value exactComplex) (json.RawMessage, bool) {
	encoded, err := json.Marshal(struct {
		Real string `json:"real"`
		Imag string `json:"imag"`
	}{Real: exactRationalText(value.realPart), Imag: exactRationalText(value.imaginaryPart)})
	return json.RawMessage(encoded), err == nil
}

func exactRationalFromRaw(raw json.RawMessage) (exactRational, bool) {
	if text, ok := rawString(raw); ok {
		return parseExactRationalLiteral(text)
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return exactRational{}, false
	}
	return parseExactRationalLiteral(number.String())
}

func constantRawNegativeZero(raw json.RawMessage) bool {
	text := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(text, "-") {
		return false
	}
	value, ok := parseExactRationalLiteral(text)
	return ok && value.IsZero()
}

func exactComplexFromRaw(raw json.RawMessage) (exactComplex, bool) {
	var wire struct {
		Real json.RawMessage `json:"real"`
		Imag json.RawMessage `json:"imag"`
	}
	if err := json.Unmarshal(raw, &wire); err == nil && len(wire.Real) != 0 && len(wire.Imag) != 0 {
		realPart, realOK := exactRationalFromRaw(wire.Real)
		imagPart, imagOK := exactRationalFromRaw(wire.Imag)
		return exactComplex{realPart: realPart, imaginaryPart: imagPart}, realOK && imagOK
	}
	return exactComplex{}, false
}

func (l *lowerer) constExactRational(raw json.RawMessage, typ string) (exactRational, bool) {
	kind := l.underlyingConstType(typ)
	if !isIntegerType(kind) && !isFloatType(kind) {
		return exactRational{}, false
	}
	return exactRationalFromRaw(raw)
}

func (l *lowerer) constExactComplex(raw json.RawMessage, typ string) (exactComplex, bool) {
	kind := l.underlyingConstType(typ)
	if isComplexType(kind) {
		return exactComplexFromRaw(raw)
	}
	if isIntegerType(kind) || isFloatType(kind) {
		realPart, ok := exactRationalFromRaw(raw)
		if !ok {
			return exactComplex{}, false
		}
		zero, _ := constant.NewRational("0", "1")
		return exactComplex{realPart: realPart, imaginaryPart: zero}, true
	}
	return exactComplex{}, false
}

func (l *lowerer) rationalRawForType(value exactRational, typ string, untyped bool) (json.RawMessage, bool) {
	if untyped {
		return exactRationalRaw(value)
	}
	kind := l.underlyingConstType(typ)
	bits := 64
	if kind == "Float32" {
		bits = 32
	}
	converted, ok := constant.RationalFloat(value, bits)
	if !ok {
		return nil, false
	}
	return finiteFloatRaw(converted)
}

func (l *lowerer) complexRawForType(value exactComplex, typ string, untyped bool) (json.RawMessage, bool) {
	if untyped {
		return exactComplexRaw(value)
	}
	kind := l.underlyingConstType(typ)
	bits := 64
	if kind == "Complex128" {
		bits = 128
	}
	componentBits := bits / 2
	realPart, realOK := constant.RationalFloat(value.realPart, componentBits)
	imagPart, imagOK := constant.RationalFloat(value.imaginaryPart, componentBits)
	if !realOK || !imagOK {
		return nil, false
	}
	return complexRaw(complex(realPart, imagPart))
}
