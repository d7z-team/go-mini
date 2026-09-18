package runtime

import (
	"context"
	"errors"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func channelDirectionFromRuntimeType(typ RuntimeType) ffigo.ChannelDirection {
	switch {
	case typ.IsRecvChan():
		return ffigo.ChannelRecv
	case typ.IsSendChan():
		return ffigo.ChannelSend
	default:
		return ffigo.ChannelBoth
	}
}

func channelEndpointCanRecv(endpoint ffigo.ChannelEndpoint) bool {
	return endpoint != nil && endpoint.Direction().CanRecv()
}

func channelEndpointCanSend(endpoint ffigo.ChannelEndpoint) bool {
	return endpoint != nil && endpoint.Direction().CanSend()
}

func validateChannelEndpointDirection(typ RuntimeType, endpoint ffigo.ChannelEndpoint) error {
	if endpoint == nil {
		return errors.New("missing FFI channel endpoint")
	}
	required := channelDirectionFromRuntimeType(typ)
	actual := endpoint.Direction()
	switch required {
	case ffigo.ChannelRecv:
		if !actual.CanRecv() {
			return fmt.Errorf("FFI channel direction mismatch: endpoint %s cannot satisfy schema %s", channelDirectionName(actual), channelDirectionName(required))
		}
	case ffigo.ChannelSend:
		if !actual.CanSend() {
			return fmt.Errorf("FFI channel direction mismatch: endpoint %s cannot satisfy schema %s", channelDirectionName(actual), channelDirectionName(required))
		}
	default:
		if !actual.CanRecv() || !actual.CanSend() {
			return fmt.Errorf("FFI channel direction mismatch: endpoint %s cannot satisfy schema %s", channelDirectionName(actual), channelDirectionName(required))
		}
	}
	return nil
}

func channelDirectionName(direction ffigo.ChannelDirection) string {
	switch direction {
	case ffigo.ChannelRecv:
		return "RecvChan"
	case ffigo.ChannelSend:
		return "SendChan"
	case ffigo.ChannelBoth:
		return "Chan"
	default:
		return "unknown"
	}
}

func (e *Executor) encodeChannelPayload(value *Var, elem RuntimeType) ([]byte, error) {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	if err := e.serializeParsedType(buf, value, elem); err != nil {
		return nil, err
	}
	return append([]byte(nil), buf.Bytes()...), nil
}

func (e *Executor) decodeChannelPayload(payload []byte, elem RuntimeType) (*Var, error) {
	reader := ffigo.NewReader(payload)
	value, err := e.deserializeParsedType(nil, reader, elem, nil)
	if err != nil {
		return nil, err
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	return value, nil
}

func (e *Executor) registerVMChannelEndpoint(ch *VMChannel, typ RuntimeType) uint64 {
	if e == nil || ch == nil {
		return 0
	}
	elem, ok := typ.ReadChanElemType()
	if !ok || elem.IsEmpty() {
		elem = ch.ElemType()
	}
	direction := channelDirectionFromRuntimeType(typ)
	registry := e.channelRegistry()
	var endpointID uint64
	unregister := func() {
		if registry != nil && endpointID != 0 {
			registry.UnregisterChannel(endpointID)
		}
	}
	endpoint := ffigo.ChannelEndpointFuncs{
		Elem: elem.Raw.String(),
		Dir:  direction,
	}
	if direction.CanRecv() {
		endpoint.OnRecv = func(ctx context.Context) ([]byte, bool, error) {
			value, ok, err := ch.RecvExternal(ctx)
			if err != nil || !ok {
				if !ok {
					unregister()
				}
				return nil, ok, err
			}
			payload, err := e.encodeChannelPayload(value, elem)
			return payload, true, err
		}
		endpoint.OnTryRecv = func() ([]byte, bool, bool, error) {
			value, ok, ready, errText := ch.TryRecv()
			if !ready {
				return nil, false, false, nil
			}
			if errText != "" {
				return nil, false, true, fmt.Errorf("%s", errText)
			}
			if !ok {
				unregister()
				return nil, false, true, nil
			}
			payload, err := e.encodeChannelPayload(value, elem)
			return payload, true, true, err
		}
	}
	if direction.CanSend() {
		endpoint.OnSend = func(ctx context.Context, payload []byte) error {
			value, err := e.decodeChannelPayload(payload, elem)
			if err != nil {
				return err
			}
			err = ch.SendExternal(ctx, value)
			if err != nil && err.Error() == "send on closed channel" {
				unregister()
			}
			return err
		}
		endpoint.OnTrySend = func(payload []byte) (bool, error) {
			value, err := e.decodeChannelPayload(payload, elem)
			if err != nil {
				return false, err
			}
			ready, errText := ch.TrySend(value)
			if errText != "" {
				if errText == "send on closed channel" {
					unregister()
				}
				return ready, fmt.Errorf("%s", errText)
			}
			return ready, nil
		}
		endpoint.OnClose = func() error {
			defer unregister()
			return ch.Close()
		}
	}
	endpointID = registry.RegisterChannel(endpoint)
	return endpointID
}

func (e *Executor) tryExternalRecv(endpoint ffigo.ChannelEndpoint, elem RuntimeType) (*Var, bool, bool, string) {
	if !channelEndpointCanRecv(endpoint) {
		return nil, false, false, ""
	}
	tryer, ok := endpoint.(ffigo.ChannelTryReceiver)
	if !ok {
		return nil, false, false, ""
	}
	payload, recvOK, ready, err := tryer.TryRecv()
	if err != nil {
		return nil, false, ready, err.Error()
	}
	if !ready {
		return nil, false, false, ""
	}
	if !recvOK {
		return zeroVarForRuntimeType(elem), false, true, ""
	}
	value, err := e.decodeChannelPayload(payload, elem)
	if err != nil {
		return nil, false, true, err.Error()
	}
	return value, true, true, ""
}

func (e *Executor) tryExternalSend(endpoint ffigo.ChannelEndpoint, elem RuntimeType, value *Var) (bool, string) {
	if !channelEndpointCanSend(endpoint) {
		return false, ""
	}
	tryer, ok := endpoint.(ffigo.ChannelTrySender)
	if !ok {
		return false, ""
	}
	payload, err := e.encodeChannelPayload(value, elem)
	if err != nil {
		return true, err.Error()
	}
	ready, err := tryer.TrySend(payload)
	if err != nil {
		return ready, err.Error()
	}
	return ready, ""
}
