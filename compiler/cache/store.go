package cache

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type Store struct {
	backend Backend
	locks   *actionLocks
}

func New(backend Backend) Cache {
	return Store{backend: backend, locks: &actionLocks{}}
}

func (s Store) requireBackend() (Backend, error) {
	if s.backend == nil {
		return nil, errors.New("nil cache backend")
	}
	return s.backend, nil
}

func (s Store) load(actionID ActionID) ([]byte, bool, string, error) {
	backend, err := s.requireBackend()
	if err != nil {
		return nil, false, "", err
	}
	entry, found, err := backend.GetAction(actionID)
	if err != nil {
		return nil, false, "", err
	}
	if !found {
		return nil, false, "action missing", nil
	}
	if entry.Size < 0 {
		return nil, false, "action entry invalid", nil
	}
	data, found, err := backend.GetOutput(entry.Output)
	if err != nil {
		return nil, false, "", err
	}
	if !found || int64(len(data)) != entry.Size || OutputIDFor(data) != entry.Output {
		return nil, false, "output missing or corrupt", nil
	}
	return data, true, "hit", nil
}

func (s Store) store(actionID ActionID, data []byte) error {
	backend, err := s.requireBackend()
	if err != nil {
		return err
	}
	outputID := OutputIDFor(data)
	if err := backend.PutOutput(outputID, data); err != nil {
		return err
	}
	return backend.PutAction(actionID, Entry{Output: outputID, Size: int64(len(data))})
}

func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
