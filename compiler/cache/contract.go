// Package cache defines the host-neutral storage boundary used by compiler caches.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

type (
	ActionID [sha256.Size]byte
	OutputID [sha256.Size]byte

	Entry struct {
		Output OutputID
		Size   int64
	}
)

func (id ActionID) String() string { return hex.EncodeToString(id[:]) }
func (id OutputID) String() string { return hex.EncodeToString(id[:]) }

func ParseActionID(value string) (ActionID, error) {
	var id ActionID
	err := decodeID(id[:], value)
	return id, err
}

func ParseOutputID(value string) (OutputID, error) {
	var id OutputID
	err := decodeID(id[:], value)
	return id, err
}

func decodeID(dst []byte, value string) error {
	if len(value) != hex.EncodedLen(len(dst)) {
		return errors.New("invalid cache identifier length")
	}
	if _, err := hex.Decode(dst, []byte(value)); err != nil {
		return errors.New("invalid cache identifier")
	}
	return nil
}

func OutputIDFor(data []byte) OutputID { return sha256.Sum256(data) }

type Backend interface {
	GetAction(ActionID) (Entry, bool, error)
	GetOutput(OutputID) ([]byte, bool, error)
	PutOutput(OutputID, []byte) error
	PutAction(ActionID, Entry) error
}

type Event struct {
	Kind         string
	ModulePath   string
	ActionKey    string
	ArtifactHash string
	ExportHash   string
	Reason       string
	MaterialJSON []byte
}
