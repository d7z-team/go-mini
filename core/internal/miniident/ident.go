package miniident

func IsExported(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

func IsExportedQualifiedMember(name string) bool {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return IsExported(name[i+1:])
		}
	}
	return IsExported(name)
}
