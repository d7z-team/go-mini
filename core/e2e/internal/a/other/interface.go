package other

type Type struct {
	Name string
}

// ffigen:methods other.Type
type Service interface {
	Hello(t *Type) string
}

type ServiceImpl struct{}

func (s *ServiceImpl) Hello(t *Type) string {
	return "Hello from A: " + t.Name
}
