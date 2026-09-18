package lower

import (
	"fmt"
	"strings"

	ir "github.com/d7z-team/mini-go/compiler/hir"
)

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		value := values[i]
		j := i - 1
		for j >= 0 && values[j] > value {
			values[j+1] = values[j]
			j--
		}
		values[j+1] = value
	}
}

func sortTypeMethods(values []runtimeTypeMethod) {
	for i := 1; i < len(values); i++ {
		value := values[i]
		j := i - 1
		for j >= 0 && typeMethodLess(value, values[j]) {
			values[j+1] = values[j]
			j--
		}
		values[j+1] = value
	}
}

func typeMethodLess(left, right runtimeTypeMethod) bool {
	if left.Receiver != right.Receiver {
		return left.Receiver < right.Receiver
	}
	if left.Name != right.Name {
		return left.Name < right.Name
	}
	return left.Signature < right.Signature
}

func functionID(name string) string {
	return "fn." + name
}

func userInitFunctionID(index int) string {
	return fmt.Sprintf("fn.user_init.%d", index)
}

func blankFunctionID(index int) string {
	return fmt.Sprintf("fn.blank.%d", index)
}

func constID(name string) string {
	return "const." + name
}

func (l *lowerer) newConstID(name string) string {
	id := fmt.Sprintf("const.local.%d.%s", l.nextConst, strings.TrimSpace(name))
	l.nextConst++
	return id
}

func globalID(name string) string {
	return "global." + name
}

func typeID(name string) string {
	return "type." + name
}

func methodID(receiver, name string) string {
	return "method." + receiver + "." + name
}

func userLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "user." + name
}

func localID(name string) string {
	return "local." + name
}

func receiverLocalID() string {
	return "receiver.0"
}

func resultLocalID(index int) string {
	return fmt.Sprintf("result.%d", index)
}

func isBlankIdentifier(name string) bool {
	return strings.TrimSpace(name) == "_"
}

func (l *lowerer) newLocalID(fn *ir.Function, name string) string {
	base := localID(name)
	if fn == nil || !functionHasLocalID(fn, base) {
		return base
	}
	for {
		id := fmt.Sprintf("%s.%d", base, l.nextSyntheticLocal)
		l.nextSyntheticLocal++
		if !functionHasLocalID(fn, id) {
			return id
		}
	}
}

func functionHasLocalID(fn *ir.Function, id string) bool {
	if fn == nil {
		return false
	}
	for _, local := range fn.Locals {
		if local.ID == id {
			return true
		}
	}
	return false
}

func isExported(name string) bool {
	if name == "" {
		return false
	}
	ch := name[0]
	return ch >= 'A' && ch <= 'Z'
}

func (l *lowerer) newLabel(prefix string) string {
	label := fmt.Sprintf(".%s.%d", prefix, l.nextLabel)
	l.nextLabel++
	return label
}

func (l *lowerer) newSyntheticLocal(scope *funcScope, prefix, typ string) string {
	id := fmt.Sprintf("local.%s.%d", prefix, l.nextSyntheticLocal)
	name := fmt.Sprintf("%s.%d", prefix, l.nextSyntheticLocal)
	l.nextSyntheticLocal++
	if scope != nil && scope.function != nil {
		scope.function.Locals = append(scope.function.Locals, ir.Local{
			ID:        id,
			Name:      name,
			Type:      l.hirType(strings.TrimSpace(typ)),
			Scope:     scope.debugScope,
			Generated: true,
		})
	}
	return id
}
