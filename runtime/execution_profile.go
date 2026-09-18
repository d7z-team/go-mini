package runtime

import (
	"sort"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

type guestSampleKey struct {
	Generation uint64
	Module     string
	FunctionID string
	PC         int
	Opcode     string
	Location   ir.Location
}

// GuestSample identifies one sampled guest instruction across hot revisions.
type GuestSample struct {
	Generation uint64
	Module     string
	FunctionID string
	PC         int
	Opcode     string
	Location   ir.Location
	Count      uint64
}

// GuestProfileSnapshot is an immutable bounded snapshot of one execution.
type GuestProfileSnapshot struct {
	SampleEvery uint64
	Dropped     uint64
	Samples     []GuestSample
}

func (e *Execution) recordGuestSample(frame *frame, functionID string, pc int, inst *preparedInstruction) {
	if e == nil || frame == nil || inst == nil || e.profileEvery == 0 {
		return
	}
	key := guestSampleKey{
		Generation: frame.revisionGeneration(), Module: frame.module.modulePath(), FunctionID: functionID,
		PC: pc, Opcode: inst.opcodeText(),
	}
	if location := runtimeLocation(frame, functionID, pc); location != nil {
		key.Location = *location
	}
	e.mu.Lock()
	if _, exists := e.profileSamples[key]; !exists && len(e.profileSamples) >= e.profileLimit {
		e.profileDropped++
	} else {
		e.profileSamples[key]++
	}
	e.mu.Unlock()
}

// GuestProfile returns a deterministic snapshot without exposing profiler state.
func (e *Execution) GuestProfile() GuestProfileSnapshot {
	if e == nil {
		return GuestProfileSnapshot{}
	}
	e.mu.RLock()
	snapshot := GuestProfileSnapshot{SampleEvery: e.profileEvery, Dropped: e.profileDropped, Samples: make([]GuestSample, 0, len(e.profileSamples))}
	for key, count := range e.profileSamples {
		snapshot.Samples = append(snapshot.Samples, GuestSample{
			Generation: key.Generation, Module: key.Module, FunctionID: key.FunctionID,
			PC: key.PC, Opcode: key.Opcode, Location: key.Location, Count: count,
		})
	}
	e.mu.RUnlock()
	sort.Slice(snapshot.Samples, func(i, j int) bool {
		left, right := snapshot.Samples[i], snapshot.Samples[j]
		if left.Count != right.Count {
			return left.Count > right.Count
		}
		if left.Generation != right.Generation {
			return left.Generation < right.Generation
		}
		if left.Module != right.Module {
			return left.Module < right.Module
		}
		if left.FunctionID != right.FunctionID {
			return left.FunctionID < right.FunctionID
		}
		return left.PC < right.PC
	})
	return snapshot
}
