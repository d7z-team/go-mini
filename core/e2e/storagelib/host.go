package storagelib

type StorageImpl struct {
	Capacity uint32
}

func (s *StorageImpl) SetCapacity(cap uint32) {
	s.Capacity = cap
}

func (s *StorageImpl) GetStatus() int16 {
	return 1024
}
