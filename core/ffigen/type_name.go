package ffigen

import "strings"

func splitQualifiedTypeName(typeName string) (string, string, bool) {
	idx := strings.LastIndex(typeName, ".")
	if idx <= 0 || idx == len(typeName)-1 {
		return "", "", false
	}
	return typeName[:idx], typeName[idx+1:], true
}
