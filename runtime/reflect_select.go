package runtime

import (
	"errors"
	"fmt"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

const (
	reflectSelectSend    = 1
	reflectSelectRecv    = 2
	reflectSelectDefault = 3
)

type reflectSelectCaseState struct {
	index   int
	dir     int
	channel vmValue
	send    vmValue
}

func reflectSelect(ctx intrinsicContext, args []vmValue) ([]vmValue, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("reflect.select expects 1 argument, got %d", len(args))
	}
	values, ok := sliceValues(args[0])
	if !ok {
		return reflectSelectError("reflect.Select: cases must be []SelectCase"), nil
	}
	module := reflectRelationModule(ctx)
	cases := make([]reflectSelectCaseState, 0, len(values))
	defaultIndex := -1
	for index, value := range values {
		fields, ok := materializeStructValue(value.Data)
		if !ok {
			return reflectSelectError(fmt.Sprintf("reflect.Select: invalid case %d", index)), nil
		}
		direction, err := asInt64(fields["Dir"])
		if err != nil || direction < reflectSelectSend || direction > reflectSelectDefault {
			return reflectSelectError(fmt.Sprintf("reflect.Select: invalid Dir for case %d", index)), nil
		}
		if direction == reflectSelectDefault {
			if defaultIndex >= 0 {
				return reflectSelectError("reflect.Select: multiple default cases"), nil
			}
			defaultIndex = index
			continue
		}
		channel, present, message := reflectCurrentValueArg(fields["Chan"], "Select")
		if !present {
			payload, payloadErr := reflectValuePayload(fields["Chan"])
			if payloadErr == nil && !reflectBoolField(payload.get("valid")) {
				continue
			}
			return reflectSelectError(message), nil
		}
		if module == nil || !module.isWaitableTypeName(channel.Type) {
			return reflectSelectError(fmt.Sprintf("reflect.Select: case %d uses non-chan value", index)), nil
		}
		state := reflectSelectCaseState{index: index, dir: int(direction), channel: channel}
		if direction == reflectSelectSend {
			send, present, message := reflectCurrentValueArg(fields["Send"], "Select")
			if !present {
				return reflectSelectError(message), nil
			}
			state.send = send
		}
		cases = append(cases, state)
	}

	readyCases := make([]int, 0, len(cases))
	for i := range cases {
		ready, err := reflectSelectReady(module, cases[i])
		if err != nil {
			return reflectSelectError(err.Error()), nil
		}
		if ready {
			readyCases = append(readyCases, i)
		}
	}
	if selected := ctx.vm.chooseReadyIndex(readyCases); selected >= 0 {
		return reflectCompleteSelect(ctx, cases[selected])
	}
	if defaultIndex >= 0 {
		return reflectSelectResult(defaultIndex, zeroReflectValueValue(), false), nil
	}

	if _, _, err := ctx.vm.checkCollectionSize(int64(len(cases)), int64(len(cases))); err != nil {
		return nil, err
	}
	if err := ctx.vm.chargeAllocationBytes((int64(len(cases))+1)*ir.RuntimeNodeBytes + int64(len(cases))*ir.RuntimeSlotBytes); err != nil {
		return nil, err
	}
	waitState := &waitSetState{Tokens: make([]*waitTokenState, 0, len(cases))}
	waitSet := newVMValue("WaitSet", waitState)
	selectedCases := make([]reflectSelectCaseState, 0, len(cases))
	for _, selected := range cases {
		token := ctx.vm.newWaitTokenValue()
		var err error
		if selected.dir == reflectSelectRecv {
			err = waitableWaitRecvValue(module, selected.channel, token)
		} else {
			err = waitableWaitSendValue(module, selected.channel, token)
		}
		if err != nil {
			_ = cancelWaitSet(waitSet)
			return reflectSelectError(err.Error()), nil
		}
		waitState.Tokens = append(waitState.Tokens, token.Data.(*waitTokenState))
		selectedCases = append(selectedCases, selected)
	}
	return nil, &reflectSelectRequest{waitSet: waitSet, ctx: ctx, cases: selectedCases}
}

func (request *reflectSelectRequest) complete(index int) ([]vmValue, error) {
	if index < 0 || index >= len(request.cases) {
		return nil, fmt.Errorf("reflect.Select: selected case index %d is invalid", index)
	}
	selected := request.cases[index]
	if err := cancelWaitSet(request.waitSet); err != nil {
		return nil, err
	}
	return reflectCompleteSelect(request.ctx, selected)
}

func reflectSelectReady(module *moduleInstance, selected reflectSelectCaseState) (bool, error) {
	resource, err := waitableValueData(module, selected.channel)
	if err != nil {
		return false, err
	}
	if selected.dir == reflectSelectRecv {
		if !module.waitableCanRecv(selected.channel.Type) {
			return false, fmt.Errorf("cannot receive from send-only channel %s", selected.channel.Type)
		}
		return waitableRecvReady(resource), nil
	}
	if !module.waitableCanSend(selected.channel.Type) {
		return false, fmt.Errorf("cannot send on receive-only channel %s", selected.channel.Type)
	}
	return waitableSendReady(resource), nil
}

func reflectCompleteSelect(ctx intrinsicContext, selected reflectSelectCaseState) ([]vmValue, error) {
	module := reflectRelationModule(ctx)
	if selected.dir == reflectSelectSend {
		ready, err := waitableTrySendValue(module, selected.channel, selected.send)
		if err != nil {
			return reflectSelectError(err.Error()), nil
		}
		if !ready {
			return nil, errors.New("reflect.Select: selected send is no longer ready")
		}
		return reflectSelectResult(selected.index, zeroReflectValueValue(), false), nil
	}
	value, received, closed, err := waitableTryRecvValue(module, selected.channel)
	if err != nil {
		return reflectSelectError(err.Error()), nil
	}
	if !received && !closed {
		return nil, errors.New("reflect.Select: selected receive is no longer ready")
	}
	snapshot, err := reflectValueSnapshot(ctx, value)
	if err != nil {
		return reflectSelectError(err.Error()), nil
	}
	return reflectSelectResult(selected.index, snapshot, received), nil
}

func reflectSelectResult(index int, value vmValue, received bool) []vmValue {
	return []vmValue{
		newVMValue("Int", int64(index)), value, newVMValue("Bool", received),
		newVMValue("String", ""), newVMValue("Bool", true),
	}
}

func reflectSelectError(message string) []vmValue {
	return []vmValue{
		newVMValue("Int", int64(0)), zeroReflectValueValue(), newVMValue("Bool", false),
		newVMValue("String", message), newVMValue("Bool", false),
	}
}
