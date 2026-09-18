package runtime

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/types"
)

// reflectTypeLayout is the stable logical ABI used by reflect.Type. It is
// derived from canonical artifact types, never from the host Go ABI.
type reflectTypeLayout struct {
	align   int
	size    uint64
	bits    int
	chanDir int
}

const reflectWordSize = 8

func (m *moduleInstance) reflectTypeLayout(typ string) reflectTypeLayout {
	return m.reflectTypeLayoutSeen(strings.TrimSpace(typ), map[string]struct{}{})
}

func (m *moduleInstance) reflectTypeLayoutSeen(typ string, seen map[string]struct{}) reflectTypeLayout {
	typ = strings.TrimSpace(typ)
	if typ == "" || typ == "Void" {
		return reflectTypeLayout{align: 1}
	}
	if m != nil {
		if _, recursive := seen[typ]; !recursive {
			if underlying, ok := m.underlyingType(typ); ok {
				seen[typ] = struct{}{}
				layout := m.reflectTypeLayoutSeen(underlying, seen)
				delete(seen, typ)
				return layout
			}
		}
	}
	return m.reflectRuntimeTypeLayoutSeen(m.resolvedRuntimeType(typ), seen)
}

func (m *moduleInstance) reflectRuntimeTypeLayoutSeen(typ vmType, seen map[string]struct{}) reflectTypeLayout {
	if !typ.Valid() {
		return reflectTypeLayout{align: reflectWordSize, size: reflectWordSize}
	}
	if layout, ok := reflectPrimitiveLayout(typ.Ref); ok {
		return layout
	}

	node, ok := typ.Node()
	if !ok && typ.Table != nil {
		node, ok = typ.Table.Node(typ.Underlying().Ref)
	}
	switch {
	case typ.Ref.Kind == types.Pointer || (ok && node.Kind == types.Pointer):
		return reflectTypeLayout{align: reflectWordSize, size: reflectWordSize}
	case typ.Ref.Kind == types.Slice || (ok && node.Kind == types.Slice):
		return reflectTypeLayout{align: reflectWordSize, size: 3 * reflectWordSize}
	case typ.Ref.Kind == types.Map || (ok && node.Kind == types.Map):
		return reflectTypeLayout{align: reflectWordSize, size: reflectWordSize}
	case typ.Ref.Kind == types.Waitable || (ok && node.Kind == types.Waitable):
		direction := 3
		if ok {
			switch node.Direction {
			case types.ChannelReceive:
				direction = 1
			case types.ChannelSend:
				direction = 2
			}
		}
		return reflectTypeLayout{align: reflectWordSize, size: reflectWordSize, chanDir: direction}
	case typ.Ref.Kind == types.Array || (ok && node.Kind == types.Array):
		if !ok {
			return reflectTypeLayout{align: reflectWordSize, size: reflectWordSize}
		}
		elemLayout := m.reflectRuntimeTypeLayoutSeen(vmType{Ref: node.Elem, Table: typ.Table}, seen)
		stride := reflectAlignUp(elemLayout.size, uint64(elemLayout.align))
		return reflectTypeLayout{align: elemLayout.align, size: uint64(node.Length) * stride, bits: elemLayout.bits}
	case typ.Ref.Kind == types.Struct || (ok && node.Kind == types.Struct):
		if !ok {
			return reflectTypeLayout{align: reflectWordSize, size: reflectWordSize}
		}
		return m.reflectStructNodeLayout(node.Fields, typ.Table, seen)
	case typ.Ref.Kind == types.Interface || (ok && node.Kind == types.Interface):
		return reflectTypeLayout{align: reflectWordSize, size: 2 * reflectWordSize}
	case typ.Ref.Kind == types.Function || (ok && node.Kind == types.Function):
		return reflectTypeLayout{align: reflectWordSize, size: reflectWordSize}
	}
	return reflectTypeLayout{align: reflectWordSize, size: reflectWordSize}
}

func (m *moduleInstance) reflectStructNodeLayout(fields []types.Field, table *types.TypeTable, seen map[string]struct{}) reflectTypeLayout {
	offset := uint64(0)
	maxAlign := 1
	for _, field := range fields {
		layout := m.reflectRuntimeTypeLayoutSeen(vmType{Ref: field.Type, Table: table}, seen)
		if layout.align < 1 {
			layout.align = 1
		}
		offset = reflectAlignUp(offset, uint64(layout.align))
		offset += layout.size
		if layout.align > maxAlign {
			maxAlign = layout.align
		}
	}
	return reflectTypeLayout{align: maxAlign, size: reflectAlignUp(offset, uint64(maxAlign))}
}

func reflectPrimitiveLayout(ref types.TypeRef) (reflectTypeLayout, bool) {
	switch {
	case ref.Kind == types.Void:
		return reflectTypeLayout{align: 1}, true
	case ref.Kind == types.Any:
		return reflectTypeLayout{align: reflectWordSize, size: 2 * reflectWordSize}, true
	case ref.Kind != types.Primitive:
		return reflectTypeLayout{}, false
	}
	switch ref.Primitive {
	case types.PrimitiveBool:
		return reflectTypeLayout{align: 1, size: 1}, true
	case types.PrimitiveString:
		return reflectTypeLayout{align: reflectWordSize, size: 2 * reflectWordSize}, true
	case types.PrimitiveInt, types.PrimitiveInt64, types.PrimitiveUint, types.PrimitiveUint64, types.PrimitiveUintptr, types.PrimitiveFloat64:
		return reflectTypeLayout{align: reflectWordSize, size: reflectWordSize, bits: 64}, true
	case types.PrimitiveInt8, types.PrimitiveUint8:
		return reflectTypeLayout{align: 1, size: 1, bits: 8}, true
	case types.PrimitiveInt16, types.PrimitiveUint16:
		return reflectTypeLayout{align: 2, size: 2, bits: 16}, true
	case types.PrimitiveInt32, types.PrimitiveUint32, types.PrimitiveFloat32:
		return reflectTypeLayout{align: 4, size: 4, bits: 32}, true
	case types.PrimitiveComplex64:
		return reflectTypeLayout{align: 4, size: 8, bits: 64}, true
	case types.PrimitiveComplex128:
		return reflectTypeLayout{align: reflectWordSize, size: 16, bits: 128}, true
	case types.PrimitiveFunction:
		return reflectTypeLayout{align: reflectWordSize, size: reflectWordSize}, true
	}
	return reflectTypeLayout{}, false
}

func reflectAlignUp(value, alignment uint64) uint64 {
	if alignment <= 1 {
		return value
	}
	rem := value % alignment
	if rem == 0 {
		return value
	}
	return value + alignment - rem
}

func reflectTypeLayoutValues(layout reflectTypeLayout) (int, int, uint64, int, int) {
	align := layout.align
	if align < 1 {
		align = 1
	}
	return align, align, layout.size, layout.bits, layout.chanDir
}
