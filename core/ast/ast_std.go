package ast

var StdlibStructs = []any{(*MiniBool)(nil), (*MiniNumber)(nil), (*MiniFloat)(nil), (*MiniString)(nil), (*MiniByte)(nil)}

type MiniOsString interface {
	MiniObj
	GoString() string
	String() MiniString
}

type GoValueMini interface {
	GoValue() any
}
