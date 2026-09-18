package semantic

var predeclaredBuiltinNames = []string{
	"append", "cap", "clear", "close", "complex", "copy", "delete", "imag",
	"len", "make", "max", "min", "new", "panic", "print", "println", "real", "recover",
}

var predeclaredBuiltins = func() map[string]struct{} {
	result := make(map[string]struct{}, len(predeclaredBuiltinNames))
	for _, name := range predeclaredBuiltinNames {
		result[name] = struct{}{}
	}
	return result
}()

// IsPredeclaredBuiltin reports whether name denotes a predeclared Go builtin.
func IsPredeclaredBuiltin(name string) bool {
	_, ok := predeclaredBuiltins[name]
	return ok
}
