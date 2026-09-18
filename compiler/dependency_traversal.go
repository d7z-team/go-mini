package compiler

type dependencyTraversal struct {
	state map[string]uint8
	stack []string
	order []string
}

func newDependencyTraversal(capacity int) dependencyTraversal {
	return dependencyTraversal{
		state: make(map[string]uint8, capacity),
		order: make([]string, 0, capacity),
	}
}

func (t *dependencyTraversal) enter(modulePath string) (bool, []string) {
	switch t.state[modulePath] {
	case 2:
		return false, nil
	case 1:
		start := 0
		for index, path := range t.stack {
			if path == modulePath {
				start = index
				break
			}
		}
		return false, append(append([]string(nil), t.stack[start:]...), modulePath)
	default:
		t.state[modulePath] = 1
		t.stack = append(t.stack, modulePath)
		return true, nil
	}
}

func (t *dependencyTraversal) complete(modulePath string) {
	if len(t.stack) != 0 {
		t.stack = t.stack[:len(t.stack)-1]
	}
	t.state[modulePath] = 2
	t.order = append(t.order, modulePath)
}
