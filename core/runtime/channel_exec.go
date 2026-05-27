package runtime

import (
	"context"
	"errors"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func (e *Executor) dispatchChanRecv(session *StackContext, task Task) error {
	data, _ := task.Data.(*ChanRecvData)
	if data == nil {
		data = &ChanRecvData{ResultType: MustParseRuntimeType("Any")}
	}
	chVar := session.ValueStack.Pop()
	ch, ok := asVMChannel(e.unwrapValue(chVar))
	if ok {
		if value, recvOK, ready, errText := ch.TryRecv(); ready {
			if errText != "" {
				return &VMError{Message: errText, IsPanic: true}
			}
			session.ValueStack.Push(formatChannelRecvResult(value, recvOK, data.Multi, data.ResultType))
			return nil
		}
		if endpoint := ch.Endpoint(); channelEndpointCanRecv(endpoint) {
			elem := ch.ElemType()
			if value, recvOK, ready, errText := e.tryExternalRecv(endpoint, elem); ready {
				if errText != "" {
					return &VMError{Message: errText, IsPanic: true}
				}
				session.ValueStack.Push(formatChannelRecvResult(value, recvOK, data.Multi, data.ResultType))
				return nil
			}
			return e.parkExternalChannelRecv(session, task, commitChannelRecv(data), endpoint, elem, "external channel receive")
		}
	}

	if e.scheduler == nil || e.scheduler.Current() == nil {
		return errors.New("channel receive suspended without active VM scheduler")
	}
	commit := &ChanRecvCommitData{Multi: data.Multi, ResultType: data.ResultType}
	resume := Task{Op: OpChanRecvCommit, Source: task.Source, Data: commit}
	reason := "channel receive"
	if ch == nil {
		reason = "nil channel receive"
	}
	token, err := e.scheduler.ParkVM(resume, nil, reason, 0)
	if err != nil {
		return err
	}
	if ch != nil {
		waiter := &channelRecvWaiter{
			token: token,
			deliver: func(value *Var, recvOK bool, errText string) {
				commit.Value = value
				commit.OK = recvOK
				commit.Err = errText
			},
			wake: e.scheduler.WakeVM,
		}
		ch.AddRecvWaiter(waiter)
		if !e.scheduler.SetVMWait(token, channelWaitHandle(ch, token, reason)) {
			ch.RemoveWaiter(token)
		}
	} else {
		e.scheduler.SetVMWait(token, ffigoNilChannelWait(reason))
	}
	return errExecutionContextSuspend
}

func (e *Executor) dispatchChanRecvCommit(session *StackContext, task Task) error {
	data, ok := task.Data.(*ChanRecvCommitData)
	if !ok || data == nil {
		return errors.New("OpChanRecvCommit missing ChanRecvCommitData")
	}
	if data.Err != "" {
		return &VMError{Message: data.Err, IsPanic: true}
	}
	session.ValueStack.Push(formatChannelRecvResult(data.Value, data.OK, data.Multi, data.ResultType))
	return nil
}

func (e *Executor) dispatchChanSend(session *StackContext, task Task) error {
	value := session.ValueStack.Pop()
	chVar := session.ValueStack.Pop()
	ch, ok := asVMChannel(e.unwrapValue(chVar))
	if ok {
		prepared, err := e.prepareValueForType(session, value, ch.ElemType())
		if err != nil {
			return fmt.Errorf("channel send: %w", err)
		}
		value = prepared
		if ready, errText := ch.TrySend(value); ready {
			if errText != "" {
				return &VMError{Message: errText, IsPanic: true}
			}
			return nil
		}
		if endpoint := ch.Endpoint(); channelEndpointCanSend(endpoint) {
			elem := ch.ElemType()
			if ready, errText := e.tryExternalSend(endpoint, elem, value); ready {
				if errText != "" {
					return &VMError{Message: errText, IsPanic: true}
				}
				return nil
			}
			return e.parkExternalChannelSend(session, task, endpoint, elem, value, "external channel send")
		}
	}

	if e.scheduler == nil || e.scheduler.Current() == nil {
		return errors.New("channel send suspended without active VM scheduler")
	}
	commit := &ChanSendCommitData{}
	resume := Task{Op: OpChanSendCommit, Source: task.Source, Data: commit}
	reason := "channel send"
	if ch == nil {
		reason = "nil channel send"
	}
	token, err := e.scheduler.ParkVM(resume, nil, reason, 0)
	if err != nil {
		return err
	}
	if ch != nil {
		waiter := &channelSendWaiter{
			token: token,
			value: cloneVarForAssign(value),
			ack: func(errText string) {
				commit.Err = errText
			},
			wake: e.scheduler.WakeVM,
		}
		ch.AddSendWaiter(waiter)
		if !e.scheduler.SetVMWait(token, channelWaitHandle(ch, token, reason)) {
			ch.RemoveWaiter(token)
		}
	} else {
		e.scheduler.SetVMWait(token, ffigoNilChannelWait(reason))
	}
	return errExecutionContextSuspend
}

func (e *Executor) dispatchChanSendCommit(_ *StackContext, task Task) error {
	data, ok := task.Data.(*ChanSendCommitData)
	if !ok || data == nil {
		return errors.New("OpChanSendCommit missing ChanSendCommitData")
	}
	if data.Err != "" {
		return &VMError{Message: data.Err, IsPanic: true}
	}
	return nil
}

func (e *Executor) parkRangeChannel(session *StackContext, task Task, rData *RangeData, ch *VMChannel) error {
	if e.scheduler == nil || e.scheduler.Current() == nil {
		return errors.New("channel range suspended without active VM scheduler")
	}
	commit := &RangeChanCommitData{Range: rData}
	reason := "channel range"
	if ch == nil {
		reason = "nil channel range"
	}
	token, err := e.scheduler.ParkVM(Task{Op: OpRangeChanCommit, Source: task.Source, Data: commit}, nil, reason, 0)
	if err != nil {
		return err
	}
	if ch != nil {
		if endpoint := ch.Endpoint(); channelEndpointCanRecv(endpoint) {
			return e.parkExternalRangeChannel(session, token, commit, endpoint, ch.ElemType(), reason)
		}
		waiter := &channelRecvWaiter{
			token: token,
			deliver: func(value *Var, recvOK bool, errText string) {
				commit.Value = value
				commit.OK = recvOK
				commit.Err = errText
			},
			wake: e.scheduler.WakeVM,
		}
		ch.AddRecvWaiter(waiter)
		if !e.scheduler.SetVMWait(token, channelWaitHandle(ch, token, reason)) {
			ch.RemoveWaiter(token)
		}
	} else {
		e.scheduler.SetVMWait(token, ffigoNilChannelWait(reason))
	}
	return errExecutionContextSuspend
}

func commitChannelRecv(data *ChanRecvData) *ChanRecvCommitData {
	if data == nil {
		return &ChanRecvCommitData{}
	}
	return &ChanRecvCommitData{Multi: data.Multi, ResultType: data.ResultType}
}

func (e *Executor) parkExternalChannelRecv(session *StackContext, task Task, commit *ChanRecvCommitData, endpoint ffigo.ChannelEndpoint, elem RuntimeType, reason string) error {
	if e.scheduler == nil || e.scheduler.Current() == nil {
		return errors.New("external channel receive suspended without active VM scheduler")
	}
	resume := Task{Op: OpChanRecvCommit, Source: task.Source, Data: commit}
	token, err := e.scheduler.ParkVM(resume, nil, reason, 0)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(session.Context)
	if !e.scheduler.SetVMWait(token, ffigo.NewWaitHandle(ffigo.WaitExternal, reason, cancel)) {
		cancel()
		return errExecutionContextSuspend
	}
	go func() {
		payload, recvOK, callErr := endpoint.Recv(ctx)
		if callErr != nil {
			commit.Err = callErr.Error()
		} else if recvOK {
			value, decodeErr := e.decodeChannelPayload(payload, elem)
			if decodeErr != nil {
				commit.Err = decodeErr.Error()
			} else {
				commit.Value = value
				commit.OK = true
			}
		} else {
			commit.Value = zeroVarForRuntimeType(elem)
			commit.OK = false
		}
		e.scheduler.WakeVM(token)
	}()
	return errExecutionContextSuspend
}

func (e *Executor) parkExternalChannelSend(session *StackContext, task Task, endpoint ffigo.ChannelEndpoint, elem RuntimeType, value *Var, reason string) error {
	if e.scheduler == nil || e.scheduler.Current() == nil {
		return errors.New("external channel send suspended without active VM scheduler")
	}
	payload, err := e.encodeChannelPayload(value, elem)
	if err != nil {
		return err
	}
	commit := &ChanSendCommitData{}
	resume := Task{Op: OpChanSendCommit, Source: task.Source, Data: commit}
	token, err := e.scheduler.ParkVM(resume, nil, reason, 0)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(session.Context)
	if !e.scheduler.SetVMWait(token, ffigo.NewWaitHandle(ffigo.WaitExternal, reason, cancel)) {
		cancel()
		return errExecutionContextSuspend
	}
	go func() {
		if callErr := endpoint.Send(ctx, payload); callErr != nil {
			commit.Err = callErr.Error()
		}
		e.scheduler.WakeVM(token)
	}()
	return errExecutionContextSuspend
}

func (e *Executor) parkExternalRangeChannel(session *StackContext, token uint64, commit *RangeChanCommitData, endpoint ffigo.ChannelEndpoint, elem RuntimeType, reason string) error {
	if e.scheduler == nil {
		return errors.New("external channel range suspended without active VM scheduler")
	}
	ctx, cancel := context.WithCancel(session.Context)
	if !e.scheduler.SetVMWait(token, ffigo.NewWaitHandle(ffigo.WaitExternal, reason, cancel)) {
		cancel()
		return errExecutionContextSuspend
	}
	go func() {
		payload, recvOK, callErr := endpoint.Recv(ctx)
		if callErr != nil {
			commit.Err = callErr.Error()
		} else if recvOK {
			value, decodeErr := e.decodeChannelPayload(payload, elem)
			if decodeErr != nil {
				commit.Err = decodeErr.Error()
			} else {
				commit.Value = value
				commit.OK = true
			}
		} else {
			commit.OK = false
		}
		e.scheduler.WakeVM(token)
	}()
	return errExecutionContextSuspend
}

func formatChannelRecvResult(value *Var, ok, multi bool, resultType RuntimeType) *Var {
	if value == nil {
		if resultType.Kind == RuntimeTypeTuple && len(resultType.Params) > 0 {
			value = zeroVarForRuntimeType(resultType.Params[0])
		} else if !resultType.IsEmpty() && !resultType.IsAny() {
			value = zeroVarForRuntimeType(resultType)
		}
	}
	if !multi {
		return value
	}
	elemType := RuntimeType{}
	if resultType.Kind == RuntimeTypeTuple && len(resultType.Params) > 0 {
		elemType = resultType.Params[0]
	}
	if value == nil && !elemType.IsEmpty() {
		value = zeroVarForRuntimeType(elemType)
	}
	res := &Var{VType: TypeArray, Ref: &VMArray{Data: []*Var{value, NewBool(ok)}}}
	if !resultType.IsEmpty() {
		res.SetRuntimeType(resultType)
	}
	return res
}

func ffigoNilChannelWait(reason string) ffigo.WaitHandle {
	return ffigo.NewWaitHandle(ffigo.WaitDependsOnVM, reason, nil)
}
