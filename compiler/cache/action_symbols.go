package cache

import (
	"encoding/json"
	"errors"
	"strings"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

const (
	symbolStateFormat  = "mini-go-symbol-state"
	symbolStateVersion = 2
)

type symbolState struct {
	Format   string          `json:"format"`
	Version  int             `json:"version"`
	ActionID string          `json:"action_id"`
	Symbols  json.RawMessage `json:"symbols"`
}

func (s Store) LookupSymbols(action SymbolAction) (SymbolLookup, error) {
	actionID, err := action.ID()
	if err != nil {
		return SymbolLookup{}, err
	}
	data, found, reason, err := s.load(actionID)
	if err != nil || !found {
		return SymbolLookup{Reason: reason}, err
	}
	var state symbolState
	if err := decodeStrict(data, &state); err != nil || state.Format != symbolStateFormat || state.Version != symbolStateVersion {
		return SymbolLookup{Reason: "symbol state invalid"}, nil
	}
	if state.ActionID != actionID.String() {
		return SymbolLookup{Reason: "symbol state action mismatch"}, nil
	}
	var symbols ir.ProgramSymbols
	if err := decodeStrict(state.Symbols, &symbols); err != nil || validateSymbolAction(action, symbols) != nil {
		return SymbolLookup{Reason: "program symbols invalid"}, nil
	}
	return SymbolLookup{Symbols: symbols, Hit: true, Reason: reason}, nil
}

func (s Store) StoreSymbols(action SymbolAction, symbols ir.ProgramSymbols) error {
	if err := validateSymbolAction(action, symbols); err != nil {
		return err
	}
	raw, err := canonicalJSON(symbols)
	if err != nil {
		return err
	}
	actionID, err := action.ID()
	if err != nil {
		return err
	}
	state, err := canonicalJSON(symbolState{Format: symbolStateFormat, Version: symbolStateVersion, ActionID: actionID.String(), Symbols: raw})
	if err != nil {
		return err
	}
	return s.store(actionID, state)
}

func validateSymbolAction(action SymbolAction, symbols ir.ProgramSymbols) error {
	if action.Format != Format || action.Version != Version || strings.TrimSpace(action.Compiler) == "" || !validHash(action.ProgramHash) || !validHash(action.SourceGraphHash) {
		return errors.New("symbol action invalid")
	}
	if symbols.CompilerID != action.Compiler || symbols.ProgramHash != action.ProgramHash || symbols.Optimization != action.Optimization {
		return errors.New("program symbols action mismatch")
	}
	if symbols.Format != ir.SymbolsFormat || symbols.Version != ir.SymbolsVersion || symbols.ContractID != ir.SymbolsContract || symbols.Packages == nil {
		return errors.New("program symbols contract mismatch")
	}
	hash, err := ir.HashProgramSymbols(symbols)
	if err != nil || hash != symbols.Hash {
		return errors.New("program symbols hash mismatch")
	}
	return nil
}
