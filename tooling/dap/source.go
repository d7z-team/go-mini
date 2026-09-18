package dap

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	minigoruntime "github.com/d7z-team/mini-go/runtime"
	protocol "github.com/google/go-dap"
)

// SourceIdentity binds debugger text to a frame's exact program and symbols.
type SourceIdentity struct {
	Generation  uint64
	ProgramHash string
	SymbolsHash string
	Module      string
	File        string
	SourceHash  string
}

func (s *Session) frameSource(frame minigoruntime.DebugFrame, epoch uint64) *protocol.Source {
	if !frame.HasSymbols || frame.File == "" {
		return nil
	}
	if s.sourceEpoch != epoch || s.debugSources == nil {
		s.debugSources = make(map[int]SourceIdentity)
		s.sourceEpoch = epoch
	}
	identity := SourceIdentity{Generation: frame.Generation, ProgramHash: frame.ProgramHash, SymbolsHash: frame.SymbolsHash, Module: frame.ModulePath, File: frame.File, SourceHash: frame.SourceHash}
	reference := 0
	for id, source := range s.debugSources {
		if source == identity {
			reference = id
			break
		}
	}
	if reference == 0 {
		s.sourceNext++
		reference = s.sourceNext
		s.debugSources[reference] = identity
	}
	return &protocol.Source{Name: filepath.Base(frame.File), SourceReference: reference, Origin: fmt.Sprintf("%s · generation %d", frame.ModulePath, frame.Generation)}
}

func (s *Session) sourceContent(ctx context.Context, reference int) (string, error) {
	snapshot, err := s.debugSnapshot()
	if err != nil || snapshot.Epoch != s.sourceEpoch {
		return "", errors.New("source reference expired")
	}
	identity, ok := s.debugSources[reference]
	if !ok {
		return "", errors.New("unknown source reference")
	}
	if identity.SourceHash == "" {
		return "", errors.New("historical source identity is unavailable")
	}
	var text string
	if s.target.Source != nil {
		text, err = s.target.Source(ctx, identity)
	} else {
		path := ""
		if s.target.Locations != nil {
			path, _ = s.target.Locations.File(identity.Module, identity.File)
		}
		if path == "" && identity.Module == s.target.ModulePath && filepath.IsLocal(identity.File) {
			path = filepath.Join(s.target.RootPath, identity.File)
		}
		if path == "" {
			return "", errors.New("historical source is unavailable")
		}
		var data []byte
		data, err = os.ReadFile(path)
		text = string(data)
	}
	if err != nil {
		return "", err
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(text))) != identity.SourceHash {
		return "", errors.New("source content does not match frame revision")
	}
	return text, nil
}
