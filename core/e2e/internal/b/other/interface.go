package other

type Type struct {
	ID int
}

// ffigen:methods other.Type
type Service interface {
	Hello(t *Type) string
}

type ServiceImpl struct{}

func (s *ServiceImpl) Hello(t *Type) string {
	return "Hello from B: " + string(rune(t.ID))
}
