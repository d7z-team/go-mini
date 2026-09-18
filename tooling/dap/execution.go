package dap

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	protocol "github.com/google/go-dap"

	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func (s *Session) poll() error {
	if s.execution == nil {
		return nil
	}
	state := s.execution.State()
	if state == minigoruntime.ExecutionRunning || state == minigoruntime.ExecutionPending {
		_, err := s.execution.Poll()
		if err != nil {
			return s.failExecution(err)
		}
	}
	return s.publishState()
}

func (s *Session) publishState() error {
	if s.execution == nil {
		return nil
	}
	if err := s.publishOutput(); err != nil {
		return err
	}
	switch s.execution.State() {
	case minigoruntime.ExecutionPaused:
		snapshot, err := s.execution.DebugSnapshot()
		if err != nil {
			return err
		}
		if snapshot.Epoch == s.lastEpoch {
			return nil
		}
		s.lastEpoch = snapshot.Epoch
		threadID := 1
		if len(snapshot.Threads) != 0 {
			threadID = int(snapshot.Threads[0].ID)
		}
		reason := snapshot.Reason
		switch reason {
		case "step":
			reason = "step"
		case "pause":
			reason = "pause"
		default:
			reason = "breakpoint"
		}
		return s.send(&protocol.StoppedEvent{Event: s.event("stopped"), Body: protocol.StoppedEventBody{Reason: reason, ThreadId: threadID, AllThreadsStopped: true}})
	case minigoruntime.ExecutionCompleted:
		if s.terminated {
			return nil
		}
		if err := s.send(&protocol.ExitedEvent{Event: s.event("exited"), Body: protocol.ExitedEventBody{ExitCode: 0}}); err != nil {
			return err
		}
		return s.terminate()
	case minigoruntime.ExecutionFailed, minigoruntime.ExecutionCanceled:
		if s.terminated {
			return nil
		}
		if err := s.send(&protocol.ExitedEvent{Event: s.event("exited"), Body: protocol.ExitedEventBody{ExitCode: 1}}); err != nil {
			return err
		}
		return s.terminate()
	}
	return nil
}

func (s *Session) publishOutput() error {
	if s.target.Output == nil {
		return nil
	}
	stdout, stderr, nextStdout, nextStderr, err := s.target.Output.ReadOutput(s.stdoutOffset, s.stderrOffset)
	if err != nil {
		return err
	}
	s.stdoutOffset, s.stderrOffset = nextStdout, nextStderr
	if stdout != "" {
		if err := s.send(&protocol.OutputEvent{Event: s.event("output"), Body: protocol.OutputEventBody{Category: "stdout", Output: stdout}}); err != nil {
			return err
		}
	}
	if stderr != "" {
		if err := s.send(&protocol.OutputEvent{Event: s.event("output"), Body: protocol.OutputEventBody{Category: "stderr", Output: stderr}}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) failExecution(err error) error {
	if err == nil {
		return nil
	}
	if sendErr := s.send(&protocol.OutputEvent{Event: s.event("output"), Body: protocol.OutputEventBody{Category: "stderr", Output: err.Error() + "\n"}}); sendErr != nil {
		return sendErr
	}
	if sendErr := s.send(&protocol.ExitedEvent{Event: s.event("exited"), Body: protocol.ExitedEventBody{ExitCode: 1}}); sendErr != nil {
		return sendErr
	}
	return s.terminate()
}

func (s *Session) terminate() error {
	if s.terminated {
		return nil
	}
	s.terminated = true
	return s.send(&protocol.TerminatedEvent{Event: s.event("terminated")})
}

func (s *Session) requirePaused() error {
	if s.execution == nil || s.execution.State() != minigoruntime.ExecutionPaused {
		return errors.New("execution is not paused")
	}
	return nil
}

func (s *Session) debugSnapshot() (minigoruntime.DebugSnapshot, error) {
	if s.execution == nil {
		return minigoruntime.DebugSnapshot{}, errors.New("execution is not paused")
	}
	return s.execution.DebugSnapshot()
}

func (s *Session) sourceIdentity(source protocol.Source) (string, string, error) {
	path := source.Path
	if s.pathFormat == "uri" {
		parsed, err := url.Parse(path)
		if err != nil || parsed.Scheme != "file" || parsed.Host != "" && parsed.Host != "localhost" {
			return "", "", errors.New("breakpoint source must be a file URI")
		}
		path = filepath.FromSlash(parsed.Path)
	}
	if path == "" {
		return "", "", errors.New("breakpoint source path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.target.RootPath, path)
	}
	if s.target.Locations != nil {
		if location, ok := s.target.Locations.Identity(path); ok {
			return location.ModulePath, location.Path, nil
		}
		return "", "", errors.New("breakpoint source is not registered")
	}
	relative, err := filepath.Rel(s.target.RootPath, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", errors.New("breakpoint source is outside the workspace")
	}
	dir := filepath.ToSlash(filepath.Dir(relative))
	modulePath := s.target.ModulePath
	if dir != "." {
		modulePath += "/" + dir
	}
	return modulePath, filepath.ToSlash(relative), nil
}
