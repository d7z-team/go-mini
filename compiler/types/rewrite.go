package types

import (
	"strconv"
	"strings"
)

// RewriteCanonicalText rewrites a canonical type string by resolving named
// leaves through resolveName. It is intended for compiler source boundaries that
// still receive canonical text containing source aliases or imported selectors.
func RewriteCanonicalText(text string, resolveName func(string) (string, bool)) (string, bool) {
	return rewriteCanonicalText(strings.TrimSpace(text), resolveName, 0)
}

func rewriteCanonicalText(text string, resolveName func(string) (string, bool), depth int) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	if depth > 64 {
		return text, true
	}
	if rewritten, ok := rewritePrefixedCanonicalText(text, resolveName, depth); ok {
		return rewritten, true
	}
	if resolveName == nil {
		return text, true
	}
	resolved, ok := resolveName(text)
	resolved = strings.TrimSpace(resolved)
	if !ok || resolved == "" || resolved == text {
		return text, true
	}
	return rewriteCanonicalText(resolved, resolveName, depth+1)
}

func rewritePrefixedCanonicalText(text string, resolveName func(string) (string, bool), depth int) (string, bool) {
	for _, prefix := range []string{"Ptr<", "Slice<", "Waitable<", "ReceiveWaitable<", "SendWaitable<"} {
		if strings.HasPrefix(text, prefix) && strings.HasSuffix(text, ">") {
			elem, ok := rewriteCanonicalText(text[len(prefix):len(text)-1], resolveName, depth+1)
			if !ok {
				return "", false
			}
			return prefix + elem + ">", true
		}
	}
	if strings.HasPrefix(text, "Array<") && strings.HasSuffix(text, ">") {
		parts := splitTopLevel(text[len("Array<"):len(text)-1], ',')
		if len(parts) != 2 {
			return "", false
		}
		length := strings.TrimSpace(parts[0])
		if _, err := strconv.ParseUint(length, 10, 64); err != nil {
			return "", false
		}
		elem, ok := rewriteCanonicalText(parts[1], resolveName, depth+1)
		if !ok {
			return "", false
		}
		return "Array<" + length + ", " + elem + ">", true
	}
	if strings.HasPrefix(text, "Map<") && strings.HasSuffix(text, ">") {
		parts := splitTopLevel(text[len("Map<"):len(text)-1], ',')
		if len(parts) != 2 {
			return "", false
		}
		key, ok := rewriteCanonicalText(parts[0], resolveName, depth+1)
		if !ok {
			return "", false
		}
		elem, ok := rewriteCanonicalText(parts[1], resolveName, depth+1)
		if !ok {
			return "", false
		}
		return "Map<" + key + ", " + elem + ">", true
	}
	if strings.HasPrefix(text, "tuple(") && strings.HasSuffix(text, ")") {
		parts, ok := rewriteCanonicalList(text[len("tuple("):len(text)-1], resolveName, depth)
		if !ok {
			return "", false
		}
		return "tuple(" + strings.Join(parts, ", ") + ")", true
	}
	if strings.HasPrefix(text, "function(") {
		return rewriteFunctionText(text, resolveName, depth)
	}
	if strings.HasPrefix(text, "struct{") && strings.HasSuffix(text, "}") {
		return rewriteStructText(text, resolveName, depth)
	}
	if strings.HasPrefix(text, "interface{") && strings.HasSuffix(text, "}") {
		return rewriteInterfaceText(text, resolveName, depth)
	}
	return "", false
}

func rewriteCanonicalList(inner string, resolveName func(string) (string, bool), depth int) ([]string, bool) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil, true
	}
	parts := splitTopLevel(inner, ',')
	for i, part := range parts {
		rewritten, ok := rewriteCanonicalText(part, resolveName, depth+1)
		if !ok {
			return nil, false
		}
		parts[i] = rewritten
	}
	return parts, true
}

func rewriteFunctionText(text string, resolveName func(string) (string, bool), depth int) (string, bool) {
	closeIndex := matchingClose(text, strings.IndexByte(text, '('))
	if closeIndex < 0 {
		return "", false
	}
	params, ok := rewriteFunctionParams(text[len("function("):closeIndex], resolveName, depth)
	if !ok {
		return "", false
	}
	result := strings.TrimSpace(text[closeIndex+1:])
	if result == "" {
		result = "Void"
	} else if result != "Void" {
		var ok bool
		result, ok = rewriteCanonicalText(result, resolveName, depth+1)
		if !ok {
			return "", false
		}
	}
	return "function(" + strings.Join(params, ", ") + ") " + result, true
}

func rewriteFunctionParams(inner string, resolveName func(string) (string, bool), depth int) ([]string, bool) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil, true
	}
	parts := splitTopLevel(inner, ',')
	for i, part := range parts {
		part = strings.TrimSpace(part)
		variadic := strings.HasPrefix(part, "variadic ")
		if variadic {
			part = strings.TrimSpace(strings.TrimPrefix(part, "variadic "))
		}
		rewritten, ok := rewriteCanonicalText(part, resolveName, depth+1)
		if !ok {
			return nil, false
		}
		if variadic {
			rewritten = "variadic " + rewritten
		}
		parts[i] = rewritten
	}
	return parts, true
}

func rewriteStructText(text string, resolveName func(string) (string, bool), depth int) (string, bool) {
	inner := strings.TrimSpace(text[len("struct{") : len(text)-1])
	if inner == "" {
		return "struct{}", true
	}
	fields := splitTopLevel(inner, ',')
	for i, field := range fields {
		name, fieldType, tag, embedded, ok := splitStructFieldText(field)
		if !ok {
			return "", false
		}
		rewritten, ok := rewriteCanonicalText(fieldType, resolveName, depth+1)
		if !ok {
			return "", false
		}
		prefix := ""
		if embedded {
			prefix = "embedded "
		}
		fields[i] = prefix + name + ":" + rewritten
		if tag != "" {
			fields[i] += " `" + tag + "`"
		}
	}
	return "struct{" + strings.Join(fields, ",") + "}", true
}

func splitStructFieldText(text string) (string, string, string, bool, bool) {
	parts := splitTopLevel(text, ':')
	if len(parts) != 2 {
		return "", "", "", false, false
	}
	name := strings.TrimSpace(parts[0])
	embedded := strings.HasPrefix(name, "embedded ")
	if embedded {
		name = strings.TrimSpace(strings.TrimPrefix(name, "embedded "))
	}
	fieldType := strings.TrimSpace(parts[1])
	tag := ""
	if strings.HasSuffix(fieldType, "`") {
		open := strings.LastIndex(fieldType[:len(fieldType)-1], "`")
		if open >= 0 {
			tag = fieldType[open+1 : len(fieldType)-1]
			fieldType = strings.TrimSpace(fieldType[:open])
		}
	}
	return name, fieldType, tag, embedded, name != "" && fieldType != ""
}

func rewriteInterfaceText(text string, resolveName func(string) (string, bool), depth int) (string, bool) {
	inner := strings.TrimSpace(text[len("interface{") : len(text)-1])
	if inner == "" {
		return "interface{}", true
	}
	members := splitTopLevel(inner, ',')
	for i, member := range members {
		member = strings.TrimSpace(member)
		if isTypeSetMemberText(member) {
			rewritten, ok := rewriteInterfaceTypeSetText(member, resolveName, depth)
			if !ok {
				return "", false
			}
			members[i] = rewritten
			continue
		}
		parts := splitTopLevel(member, ':')
		if len(parts) == 2 {
			signature, ok := rewriteCanonicalText(parts[1], resolveName, depth+1)
			if !ok {
				return "", false
			}
			members[i] = strings.TrimSpace(parts[0]) + ":" + signature
			continue
		}
		rewritten, ok := rewriteCanonicalText(member, resolveName, depth+1)
		if !ok {
			return "", false
		}
		members[i] = rewritten
	}
	return "interface{" + strings.Join(members, ",") + "}", true
}

func isTypeSetMemberText(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "~") || len(splitTopLevel(text, '|')) > 1
}

func rewriteInterfaceTypeSetText(text string, resolveName func(string) (string, bool), depth int) (string, bool) {
	parts := splitTopLevel(text, '|')
	for i, part := range parts {
		part = strings.TrimSpace(part)
		prefix := ""
		if strings.HasPrefix(part, "~") {
			prefix = "~"
			part = strings.TrimSpace(strings.TrimPrefix(part, "~"))
		}
		rewritten, ok := rewriteCanonicalText(part, resolveName, depth+1)
		if !ok {
			return "", false
		}
		parts[i] = prefix + rewritten
	}
	return strings.Join(parts, "|"), true
}
