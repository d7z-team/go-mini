package runtime

import (
	"errors"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type ffiCopyBackTarget struct {
	LHS      LHSValue
	Type     RuntimeType
	WireType RuntimeType
	Mode     FFIParamMode
}

func ffiCopyBackIndices(sig *RuntimeFuncSig, argCount int) ([]int, error) {
	if sig == nil || len(sig.ParamModes) == 0 {
		return nil, nil
	}
	indices := make([]int, 0)
	for i, mode := range sig.ParamModes {
		if mode != FFIParamInOutBytes && mode != FFIParamInOutArray {
			continue
		}
		if sig.Variadic && i == len(sig.ParamModes)-1 {
			return nil, errors.New("variadic inout parameters are not supported")
		}
		if i >= argCount {
			return nil, fmt.Errorf("missing argument for inout parameter %d", i)
		}
		indices = append(indices, i)
	}
	return indices, nil
}

func (e *Executor) resolveFFICopyBackTargets(session *StackContext, sig *RuntimeFuncSig, args []*Var, argLHS []LHSValue) ([]ffiCopyBackTarget, error) {
	indices, err := ffiCopyBackIndices(sig, len(args))
	if err != nil {
		return nil, err
	}
	if len(indices) == 0 {
		return nil, nil
	}
	targets := make([]ffiCopyBackTarget, 0, len(indices))
	for _, idx := range indices {
		if idx >= len(args) {
			return nil, fmt.Errorf("missing argument for inout parameter %d", idx)
		}
		var lhs LHSValue
		if idx < len(argLHS) {
			lhs = argLHS[idx]
		}
		if lhs == nil {
			return nil, fmt.Errorf("inout parameter %d requires assignable argument", idx)
		}
		loaded, err := e.loadAddress(session, lhs)
		if err != nil {
			return nil, err
		}
		current := loaded
		current = e.unwrapValue(current)
		mode := sig.ParamModes[idx]
		switch mode {
		case FFIParamInOutBytes:
			if current == nil || current.VType != TypeBytes {
				return nil, fmt.Errorf("inout bytes argument %d must be TypeBytes", idx)
			}
		case FFIParamInOutArray:
			if current == nil || current.VType != TypeArray {
				return nil, fmt.Errorf("inout array argument %d must be Array", idx)
			}
		default:
			return nil, fmt.Errorf("unsupported inout parameter mode %d", mode)
		}
		target := ffiCopyBackTarget{
			LHS:      lhs,
			Type:     current.RuntimeType(),
			WireType: sig.ParamTypes[idx],
			Mode:     mode,
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (e *Executor) applyFFICopyBack(session *StackContext, bridge ffigo.FFIBridge, target ffiCopyBackTarget, reader *ffigo.Reader) error {
	if target.LHS == nil {
		return errors.New("inout copy-back target is not assignable")
	}
	switch target.Mode {
	case FFIParamInOutBytes:
		bytes, err := reader.ReadBytes()
		if err != nil {
			return err
		}
		next := NewBytes(bytes)
		if !target.Type.IsEmpty() {
			next.SetRuntimeType(target.Type)
		}
		return e.storeAddress(session, target.LHS, next)
	case FFIParamInOutArray:
		payload, err := reader.ReadBytes()
		if err != nil {
			return err
		}
		copyBackReader := ffigo.NewReader(payload)
		next, err := e.deserializeRuntimeType(session, copyBackReader, target.WireType, bridge)
		if err != nil {
			return err
		}
		if err := copyBackReader.Err(); err != nil {
			return err
		}
		if next != nil && !target.Type.IsEmpty() {
			next.SetRuntimeType(target.Type)
		}
		return e.storeAddress(session, target.LHS, next)
	default:
		return fmt.Errorf("unsupported inout parameter mode %d", target.Mode)
	}
}
