package storagelib

type StorageImpl struct {
	Capacity uint32
}

func (s *StorageImpl) SetCapacity(capacity uint32) {
	s.Capacity = capacity
}

func (s *StorageImpl) GetStatus() int16 {
	return 1024
}
