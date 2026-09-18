package bytecode

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func EncodeJSON(a *Artifact) ([]byte, error) {
	if err := ValidateArtifact(a); err != nil {
		return nil, err
	}
	return CanonicalJSON(a)
}

func DecodeJSON(data []byte) (Artifact, error) {
	return ReadJSON(bytes.NewReader(data))
}

func ReadJSON(r io.Reader) (Artifact, error) {
	artifact, err := readJSON(r)
	if err != nil {
		return Artifact{}, err
	}
	if err := ValidateArtifact(&artifact); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

// ReadJSONWithLimits decodes and validates an untrusted artifact once using
// the caller's resource limits.
func ReadJSONWithLimits(r io.Reader, limits ValidationLimits) (Artifact, error) {
	artifact, err := readJSON(r)
	if err != nil {
		return Artifact{}, err
	}
	if err := ValidateArtifactWithLimits(&artifact, limits); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

func readJSON(r io.Reader) (Artifact, error) {
	var artifact Artifact
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifact); err != nil {
		return Artifact{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Artifact{}, errors.New("unexpected trailing JSON value")
		}
		return Artifact{}, err
	}
	return artifact, nil
}
