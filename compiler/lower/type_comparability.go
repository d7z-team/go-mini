package lower

import "github.com/d7z-team/mini-go/compiler/types"

func (l *lowerer) channelAssignableType(source, target string) bool {
	sourceChannel, sourceOK := l.channelTypeInfo(l.resolveNamedUnderlyingType(source))
	targetChannel, targetOK := l.channelTypeInfo(l.resolveNamedUnderlyingType(target))
	if !sourceOK || !targetOK {
		return false
	}
	if sourceChannel.direction != "both" {
		return false
	}
	if !l.sameTypeIdentity(sourceChannel.elem, targetChannel.elem) {
		return false
	}
	return !l.isNamedType(source) || !l.isNamedType(target)
}

type assignableChannelType struct {
	direction string
	elem      string
}

func (l *lowerer) channelTypeInfo(typ string) (assignableChannelType, bool) {
	view, ok := l.typeView(typ)
	if !ok {
		return assignableChannelType{}, false
	}
	direction, elem, ok := view.Waitable()
	if !ok {
		return assignableChannelType{}, false
	}
	info := assignableChannelType{elem: l.typeRefString(elem)}
	switch direction {
	case types.ChannelBoth:
		info.direction = "both"
	case types.ChannelReceive:
		info.direction = "recv"
	case types.ChannelSend:
		info.direction = "send"
	default:
		return assignableChannelType{}, false
	}
	return info, true
}

func (l *lowerer) isNilAssignableType(typ string) bool {
	ref, ok := l.typeRef(typ)
	return ok && !l.isGeneralInterfaceType(typ) && l.typeRelations().NilAssignable(ref).OK
}

func (l *lowerer) isComparableType(typ string) bool {
	ref, ok := l.typeRef(typ)
	return !ok || l.typeRelations().Comparable(ref).OK
}
