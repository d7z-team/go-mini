// Package dap adapts Mini-Go's protocol-neutral runtime debugger to DAP.
package dap

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/url"
	"path/filepath"

	protocol "github.com/google/go-dap"

	"github.com/d7z-team/mini-go/compiler/workspace"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

type LaunchConfig struct {
	Module    string                      `json:"module,omitempty"`
	Sources   []workspace.DirectorySource `json:"sources,omitempty"`
	Directory string                      `json:"cwd"`
	Root      string                      `json:"root,omitempty"`
	Tags      []string                    `json:"tags,omitempty"`
}

type LaunchTarget struct {
	// Source resolves exact historical content. Returned text is checked against SourceHash.
	Source     func(context.Context, SourceIdentity) (string, error)
	Locations  *workspace.SourceLocations
	Program    *minigoruntime.Program
	Options    minigoruntime.InstanceOptions
	Output     OutputReader
	RootPath   string
	ModulePath string
	Cleanup    func() error
}

type Launcher func(context.Context, LaunchConfig) (LaunchTarget, error)

type Session struct {
	sourceEpoch  uint64
	sourceNext   int
	debugSources map[int]SourceIdentity
	input        *bufio.Reader
	closer       io.Closer
	output       io.Writer
	launcher     Launcher
	requests     chan protocol.Message
	readErr      chan error
	done         chan struct{}
	seq          int
	breakpointID int

	initialized     bool
	launched        bool
	configured      bool
	linesStartAt1   bool
	columnsStartAt1 bool
	pathFormat      string
	target          LaunchTarget
	instance        *minigoruntime.Instance
	execution       *minigoruntime.Execution
	lastEpoch       uint64
	stdoutOffset    int
	stderrOffset    int
	terminated      bool
}

func NewSession(input io.Reader, output io.Writer, launcher Launcher) *Session {
	closer, _ := input.(io.Closer)
	return &Session{input: bufio.NewReader(input), closer: closer, output: output, launcher: launcher, requests: make(chan protocol.Message), readErr: make(chan error, 1), done: make(chan struct{}), linesStartAt1: true, columnsStartAt1: true, pathFormat: "path"}
}

func (s *Session) Serve(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	go s.readRequests()
	defer s.closeTarget()
	if s.closer != nil {
		defer s.closer.Close()
	}
	defer close(s.done)
	for {
		var ready <-chan struct{}
		if s.execution != nil {
			ready = s.execution.Ready()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-s.readErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case message := <-s.requests:
			stop, err := s.handle(ctx, message)
			if err != nil {
				return err
			}
			if stop {
				return nil
			}
		case <-ready:
			if err := s.poll(); err != nil {
				return err
			}
		}
	}
}

func (s *Session) readRequests() {
	for {
		message, err := protocol.ReadProtocolMessage(s.input)
		if err != nil {
			select {
			case s.readErr <- err:
			case <-s.done:
			}
			return
		}
		select {
		case s.requests <- message:
		case <-s.done:
			return
		}
	}
}

func (s *Session) response(request *protocol.Request) protocol.Response {
	return protocol.Response{ProtocolMessage: protocol.ProtocolMessage{Seq: s.nextSeq(), Type: "response"}, RequestSeq: request.Seq, Success: true, Command: request.Command}
}

func (s *Session) event(name string) protocol.Event {
	return protocol.Event{ProtocolMessage: protocol.ProtocolMessage{Seq: s.nextSeq(), Type: "event"}, Event: name}
}

func (s *Session) nextSeq() int {
	s.seq++
	return s.seq
}

func (s *Session) send(message protocol.Message) error {
	return protocol.WriteProtocolMessage(s.output, message)
}

func (s *Session) sendError(request *protocol.Request, message string) error {
	return s.send(&protocol.ErrorResponse{Response: protocol.Response{ProtocolMessage: protocol.ProtocolMessage{Seq: s.nextSeq(), Type: "response"}, RequestSeq: request.Seq, Success: false, Command: request.Command, Message: message}})
}

func (s *Session) runtimeLine(line int) int {
	if s.linesStartAt1 {
		return line
	}
	return line + 1
}

func (s *Session) clientLine(line int) int {
	if s.linesStartAt1 {
		return line
	}
	return line - 1
}

func (s *Session) clientColumn(column int) int {
	if s.columnsStartAt1 {
		return column
	}
	return column - 1
}

func (s *Session) clientPath(path string) string {
	if s.pathFormat == "uri" {
		return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	}
	return path
}

func page(start, count, length int) (int, int) {
	if start < 0 {
		start = 0
	}
	if start > length {
		start = length
	}
	end := length
	if count > 0 && count < end-start {
		end = start + count
	}
	return start, end
}

func (s *Session) closeTarget() {
	if s.execution != nil && s.execution.State() != minigoruntime.ExecutionCompleted && s.execution.State() != minigoruntime.ExecutionFailed && s.execution.State() != minigoruntime.ExecutionCanceled {
		s.execution.Cancel()
	}
	if s.instance != nil {
		_ = s.instance.Close()
		s.instance = nil
	}
	if s.target.Cleanup != nil {
		_ = s.target.Cleanup()
		s.target.Cleanup = nil
	}
}
