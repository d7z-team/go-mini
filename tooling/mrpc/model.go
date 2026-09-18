// Package mrpc parses and validates Mini-Go RPC contract files.
package mrpc

const (
	// Syntax is the accepted MRPC source language version.
	Syntax = "mrpc/v2"
	// ProtocolDomain separates contract hashes from other content hashes.
	ProtocolDomain = "minigo.mrpc.contract.v4"
)

// File is the parsed form of one MRPC source file.
type File struct {
	Path          string
	Text          string
	Syntax        string
	Namespace     string
	NamespaceSpan Span
	Options       []Option
	Imports       []Import
	Enums         []Enum
	Messages      []Message
	Resources     []Resource
	Services      []Service
	Hash          string
}

// Enum declares one named integer enumeration.
type Enum struct {
	Doc      string
	Name     string
	NameSpan Span
	Values   []EnumValue
	Span     Span
}

// EnumValue declares one stable enum name and number.
type EnumValue struct {
	Doc      string
	Name     string
	NameSpan Span
	Number   int64
	Span     Span
}

// Option is a generator-specific namespace option.
type Option struct {
	Name      string
	Value     string
	NameSpan  Span
	ValueSpan Span
	Span      Span
}

// Import binds an explicit source alias to another MRPC catalog.
type Import struct {
	Alias     string
	Path      string
	AliasSpan Span
	PathSpan  Span
	Span      Span
}

// Message declares one structured wire value.
type Message struct {
	Doc      string
	Name     string
	NameSpan Span
	Fields   []Field
	Span     Span
}

// Field is one stable message, parameter, or result field.
type Field struct {
	Doc      string
	Name     string
	NameSpan Span
	Type     Type
	ID       int
	IDSpan   Span
	Span     Span
}

// Resource declares a remotely referenced object and its methods.
type Resource struct {
	Doc      string
	Name     string
	NameSpan Span
	Methods  []Method
	Span     Span
}

// Service declares a stateless group of methods.
type Service struct {
	Doc      string
	Name     string
	NameSpan Span
	Methods  []Method
	Span     Span
}

// Method declares one RPC call signature.
type Method struct {
	Doc      string
	Name     string
	NameSpan Span
	Params   []Field
	Results  []Field
	Span     Span
}

// TypeKind identifies the structural form of an MRPC type.
type TypeKind uint8

const (
	// TypeName identifies a scalar or declared named type.
	TypeName TypeKind = iota + 1
	// TypeSlice identifies a variable-length sequence.
	TypeSlice
	// TypeMap identifies a key-value collection.
	TypeMap
	// TypeOptional identifies an explicitly nullable value.
	TypeOptional
)

// Type is a structural MRPC wire type.
type Type struct {
	Kind          TypeKind
	Name          string
	Qualifier     string
	NameSpan      Span
	QualifierSpan Span
	Key           *Type
	Elem          *Type
	Span          Span
}

// String returns the canonical source spelling of t.
func (t Type) String() string {
	switch t.Kind {
	case TypeSlice:
		return "[]" + typeString(t.Elem)
	case TypeMap:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Elem)
	case TypeOptional:
		return "optional[" + typeString(t.Elem) + "]"
	default:
		if t.Qualifier != "" {
			return t.Qualifier + "." + t.Name
		}
		return t.Name
	}
}

func typeString(t *Type) string {
	if t == nil {
		return "<invalid>"
	}
	return t.String()
}
