package runtime

import (
	"context"
	"errors"
	"sync"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type selectWaitGroup struct {
	mu        sync.Mutex
	chosen    bool
	token     uint64
	commit    *SelectCommitData
	scheduler *ExecutionContextScheduler
	waiters   []selectRegisteredWaiter
	cancels   []func()
}

type selectRegisteredWaiter struct {
	ch    *VMChannel
	token uint64
}

func (g *selectWaitGroup) choose(index int, value *Var, ok bool, errText string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	if g.chosen {
		g.mu.Unlock()
		return
	}
	g.chosen = true
	if g.commit != nil {
		g.commit.CaseIndex = index
		g.commit.Value = value
		g.commit.OK = ok
		g.commit.Err = errText
	}
	waiters := append([]selectRegisteredWaiter(nil), g.waiters...)
	cancels := append([]func(){}, g.cancels...)
	g.waiters = nil
	g.cancels = nil
	g.mu.Unlock()
	for _, waiter := range waiters {
		if waiter.ch != nil {
			waiter.ch.RemoveWaiter(waiter.token)
		}
	}
	runExecutionContextCancels(cancels)
	if g.scheduler != nil {
		g.scheduler.WakeVM(g.token)
	}
}

func (g *selectWaitGroup) add(ch *VMChannel, token uint64) {
	g.mu.Lock()
	g.waiters = append(g.waiters, selectRegisteredWaiter{ch: ch, token: token})
	g.mu.Unlock()
}

func (g *selectWaitGroup) addCancel(cancel func()) {
	if cancel == nil {
		return
	}
	g.mu.Lock()
	if g.chosen {
		g.mu.Unlock()
		cancel()
		return
	}
	g.cancels = append(g.cancels, cancel)
	g.mu.Unlock()
}

func (g *selectWaitGroup) isChosen() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.chosen
}

func (e *Executor) dispatchSelect(session *StackContext, task Task) error {
	plan, ok := task.Data.(*SelectData)
	if !ok || plan == nil {
		return errors.New("OpSelect missing SelectData")
	}
	operands := make([]SelectOperand, len(plan.Cases))
	for i := len(plan.Cases) - 1; i >= 0; i-- {
		switch plan.Cases[i].Kind {
		case SelectCommSend:
			operands[i].Value = session.ValueStack.Pop()
			operands[i].Chan = session.ValueStack.Pop()
		case SelectCommRecv:
			operands[i].Chan = session.ValueStack.Pop()
		}
	}

	defaultIndex := -1
	for i := range plan.Cases {
		c := plan.Cases[i]
		switch c.Kind {
		case SelectCommDefault:
			if defaultIndex < 0 {
				defaultIndex = i
			}
		case SelectCommRecv:
			ch, ok := asVMChannel(e.unwrapValue(operands[i].Chan))
			if !ok {
				continue
			}
			value, recvOK, ready, errText := ch.TryRecv()
			if ready {
				if errText != "" {
					return &VMError{Message: errText, IsPanic: true}
				}
				e.scheduleSelectCase(session, plan, i, value, recvOK)
				return nil
			}
			if endpoint := ch.Endpoint(); channelEndpointCanRecv(endpoint) {
				value, recvOK, ready, errText := e.tryExternalRecv(endpoint, ch.ElemType())
				if ready {
					if errText != "" {
						return &VMError{Message: errText, IsPanic: true}
					}
					e.scheduleSelectCase(session, plan, i, value, recvOK)
					return nil
				}
			}
		case SelectCommSend:
			ch, ok := asVMChannel(e.unwrapValue(operands[i].Chan))
			if !ok {
				continue
			}
			ready, errText := ch.TrySend(operands[i].Value)
			if ready {
				if errText != "" {
					return &VMError{Message: errText, IsPanic: true}
				}
				e.scheduleSelectCase(session, plan, i, nil, true)
				return nil
			}
			if endpoint := ch.Endpoint(); channelEndpointCanSend(endpoint) {
				ready, errText := e.tryExternalSend(endpoint, ch.ElemType(), operands[i].Value)
				if ready {
					if errText != "" {
						return &VMError{Message: errText, IsPanic: true}
					}
					e.scheduleSelectCase(session, plan, i, nil, true)
					return nil
				}
			}
		}
	}

	if defaultIndex >= 0 {
		e.scheduleSelectCase(session, plan, defaultIndex, nil, true)
		return nil
	}
	return e.parkSelect(session, task, plan, operands)
}

func (e *Executor) dispatchSelectCommit(session *StackContext, task Task) error {
	data, ok := task.Data.(*SelectCommitData)
	if !ok || data == nil || data.Plan == nil {
		return errors.New("OpSelectCommit missing SelectCommitData")
	}
	if data.Err != "" {
		return &VMError{Message: data.Err, IsPanic: true}
	}
	e.scheduleSelectCase(session, data.Plan, data.CaseIndex, data.Value, data.OK)
	return nil
}

func (e *Executor) parkSelect(session *StackContext, task Task, plan *SelectData, operands []SelectOperand) error {
	if e.scheduler == nil || e.scheduler.Current() == nil {
		return errors.New("select suspended without active VM scheduler")
	}
	commit := &SelectCommitData{Plan: plan, CaseIndex: -1}
	token, err := e.scheduler.ParkVM(Task{Op: OpSelectCommit, Source: task.Source, Data: commit}, nil, "select", 0)
	if err != nil {
		return err
	}
	group := &selectWaitGroup{token: token, commit: commit, scheduler: e.scheduler}
	registered := false
	hasExternal := false
	for i := range plan.Cases {
		if group.isChosen() {
			break
		}
		caseIndex := i
		switch plan.Cases[i].Kind {
		case SelectCommRecv:
			ch, ok := asVMChannel(e.unwrapValue(operands[i].Chan))
			if !ok {
				continue
			}
			if endpoint := ch.Endpoint(); channelEndpointCanRecv(endpoint) {
				ctx, cancel := context.WithCancel(session.Context)
				group.addCancel(cancel)
				elem := ch.ElemType()
				go func() {
					payload, recvOK, callErr := endpoint.Recv(ctx)
					if callErr != nil {
						group.choose(caseIndex, nil, false, callErr.Error())
						return
					}
					if !recvOK {
						group.choose(caseIndex, zeroVarForRuntimeType(elem), false, "")
						return
					}
					value, decodeErr := e.decodeChannelPayload(payload, elem)
					if decodeErr != nil {
						group.choose(caseIndex, nil, false, decodeErr.Error())
						return
					}
					group.choose(caseIndex, value, true, "")
				}()
				registered = true
				hasExternal = true
				continue
			}
			waiter := &channelRecvWaiter{
				token: token,
				deliver: func(value *Var, recvOK bool, errText string) {
					group.choose(caseIndex, value, recvOK, errText)
				},
				wake: func(uint64) bool { return true },
			}
			group.add(ch, token)
			ch.AddRecvWaiter(waiter)
			registered = true
		case SelectCommSend:
			ch, ok := asVMChannel(e.unwrapValue(operands[i].Chan))
			if !ok {
				continue
			}
			if endpoint := ch.Endpoint(); channelEndpointCanSend(endpoint) {
				payload, err := e.encodeChannelPayload(operands[i].Value, ch.ElemType())
				if err != nil {
					group.choose(caseIndex, nil, true, err.Error())
					registered = true
					continue
				}
				ctx, cancel := context.WithCancel(session.Context)
				group.addCancel(cancel)
				go func() {
					if callErr := endpoint.Send(ctx, payload); callErr != nil {
						group.choose(caseIndex, nil, true, callErr.Error())
						return
					}
					group.choose(caseIndex, nil, true, "")
				}()
				registered = true
				hasExternal = true
				continue
			}
			waiter := &channelSendWaiter{
				token: token,
				value: cloneVarForAssign(operands[i].Value),
				ack: func(errText string) {
					group.choose(caseIndex, nil, true, errText)
				},
				wake: func(uint64) bool { return true },
			}
			group.add(ch, token)
			ch.AddSendWaiter(waiter)
			registered = true
		}
	}
	if registered {
		kind := ffigo.WaitDependsOnVM
		if hasExternal {
			kind = ffigo.WaitExternal
		}
		wait := ffigoSelectWait(group, kind)
		if !e.scheduler.SetVMWait(token, wait) {
			wait.Cancel()
		}
	} else {
		e.scheduler.SetVMWait(token, ffigoNilChannelWait("empty select"))
	}
	return errExecutionContextSuspend
}

func (e *Executor) scheduleSelectCase(session *StackContext, plan *SelectData, index int, value *Var, ok bool) {
	if plan == nil || index < 0 || index >= len(plan.Cases) {
		return
	}
	c := plan.Cases[index]
	session.TaskStack = append(session.TaskStack, Task{Op: OpScopeExit})
	session.TaskStack = append(session.TaskStack, c.Body...)
	session.TaskStack = append(session.TaskStack, Task{
		Op: OpSelectScopeEnter,
		Data: &SelectScopeData{
			Plan:      plan,
			CaseIndex: index,
			Value:     value,
			OK:        ok,
		},
	})
}

func (e *Executor) dispatchSelectScopeEnter(session *StackContext, task Task) error {
	data, ok := task.Data.(*SelectScopeData)
	if !ok || data == nil || data.Plan == nil || data.CaseIndex < 0 || data.CaseIndex >= len(data.Plan.Cases) {
		return errors.New("OpSelectScopeEnter missing SelectScopeData")
	}
	c := data.Plan.Cases[data.CaseIndex]
	if err := session.ScopeApply("select_case"); err != nil {
		return err
	}
	if c.Kind != SelectCommRecv {
		return nil
	}
	if c.RecvName != "" && c.RecvName != "_" {
		if c.Define {
			if c.RecvSym.Kind == SymbolLocal {
				_ = session.DeclareSymbol(c.RecvSym, c.RecvType)
				_ = session.StoreSymbol(c.RecvSym, data.Value)
			} else {
				_ = session.AddVariable(c.RecvName, data.Value)
			}
		} else if c.RecvSym.Kind != SymbolUnknown {
			_ = session.StoreSymbol(c.RecvSym, data.Value)
		} else {
			_ = session.Store(c.RecvName, data.Value)
		}
	}
	if c.RecvOK != "" && c.RecvOK != "_" {
		okVal := NewBool(data.OK)
		if c.Define {
			if c.RecvOKSym.Kind == SymbolLocal {
				_ = session.DeclareSymbol(c.RecvOKSym, MustParseRuntimeType("Bool"))
				_ = session.StoreSymbol(c.RecvOKSym, okVal)
			} else {
				_ = session.AddVariable(c.RecvOK, okVal)
			}
		} else if c.RecvOKSym.Kind != SymbolUnknown {
			_ = session.StoreSymbol(c.RecvOKSym, okVal)
		} else {
			_ = session.Store(c.RecvOK, okVal)
		}
	}
	return nil
}

func ffigoSelectWait(group *selectWaitGroup, kind ffigo.WaitKind) ffigo.WaitHandle {
	return ffigo.NewWaitHandle(kind, "select", func() {
		if group == nil {
			return
		}
		group.mu.Lock()
		waiters := append([]selectRegisteredWaiter(nil), group.waiters...)
		cancels := append([]func(){}, group.cancels...)
		group.waiters = nil
		group.cancels = nil
		group.mu.Unlock()
		for _, waiter := range waiters {
			if waiter.ch != nil {
				waiter.ch.RemoveWaiter(waiter.token)
			}
		}
		runExecutionContextCancels(cancels)
	})
}
