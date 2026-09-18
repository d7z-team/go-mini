package lower

import (
	"strings"

	check "github.com/d7z-team/mini-go/compiler/semantic"
	"github.com/d7z-team/mini-go/compiler/types"
)

func (l *lowerer) sameTypeIdentity(left, right string) bool {
	return l.typeIdentity(left) == l.typeIdentity(right)
}

func (l *lowerer) typeIdentity(typ string) string {
	typ = l.resolveType(strings.TrimSpace(typ))
	if typ == "" {
		return ""
	}
	if ref, ok := l.typeRef(typ); ok {
		typ = l.typeRefString(ref)
	}
	if l.isNamedType(typ) {
		if isPredeclaredNamedType(typ) {
			return typ
		}
		if export, ok := l.importedTypeInfo(typ); ok {
			if strings.TrimSpace(export.ModulePath) != "" && strings.HasPrefix(typ, strings.TrimSpace(export.ModulePath)+".") {
				return typ
			}
			return strings.TrimSpace(export.ModulePath) + "." + strings.TrimSpace(typ)
		}
		if l.program != nil && strings.TrimSpace(l.program.ModulePath) != "" {
			return strings.TrimSpace(l.program.ModulePath) + "." + typ
		}
		return typ
	}
	return typ
}

func (l *lowerer) isNamedType(typ string) bool {
	typ = l.resolveType(strings.TrimSpace(typ))
	if typ == "" {
		return false
	}
	if isPredeclaredNamedType(typ) {
		return true
	}
	if _, ok := l.typeDeclFor(typ); ok {
		return true
	}
	if _, ok := l.importedNamedTypeInfo(typ); ok {
		return true
	}
	return false
}

func (l *lowerer) importedNamedTypeInfo(typ string) (moduleExportInfo, bool) {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return moduleExportInfo{}, false
	}
	for modulePath, exports := range l.moduleExports {
		prefix := modulePath + "."
		if !strings.HasPrefix(typ, prefix) {
			continue
		}
		name := strings.TrimSpace(typ[len(prefix):])
		if name == "" || strings.Contains(name, ".") {
			continue
		}
		info, ok := exports[name]
		if ok && info.Kind == check.ObjectType {
			return info, true
		}
	}
	for _, exports := range l.moduleExports {
		for _, info := range exports {
			if info.Kind == check.ObjectType && strings.TrimSpace(info.Type) == typ {
				return info, true
			}
		}
	}
	return moduleExportInfo{}, false
}

func isPredeclaredNamedType(typ string) bool {
	primitive, ok := types.PrimitiveByName(typ)
	return ok && types.IsPrimitiveType(nil, types.Builtin(primitive))
}
