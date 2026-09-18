package lower

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/types"
)

func (l *lowerer) methodReceiverType(typ ast.TypeExpr) string {
	if l.semantic != nil {
		if info, ok := l.semantic.Types[typ.NodeID]; ok && info.Type.Valid() {
			if formatted := l.formatSemanticType(info.Type); formatted != "" {
				return l.localMethodReceiverType(formatted)
			}
		}
	}
	return l.resolveSourceType(typ)
}

func (l *lowerer) localMethodReceiverType(receiver string) string {
	modulePrefix := strings.TrimSpace(l.modulePath) + "."
	localized, ok := types.RewriteCanonicalText(receiver, func(name string) (string, bool) {
		name = strings.TrimSpace(name)
		if modulePrefix != "." && strings.HasPrefix(name, modulePrefix) {
			localName := strings.TrimPrefix(name, modulePrefix)
			if _, declared := l.typeDecls[localName]; declared {
				return localName, true
			}
		}
		return name, true
	})
	if ok {
		return localized
	}
	return receiver
}
