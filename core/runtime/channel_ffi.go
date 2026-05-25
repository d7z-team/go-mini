package runtime

import (
	"context"
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
	return e.deserializeParsedType(nil, reader, elem, nil)
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
	endpoint := ffigo.ChannelEndpointFuncs{
		Elem: elem.Raw.String(),
		Dir:  direction,
	}
	if direction.CanRecv() {
		endpoint.OnRecv = func(ctx context.Context) ([]byte, bool, error) {
			value, ok, err := ch.RecvExternal(ctx)
			if err != nil || !ok {
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
			return ch.SendExternal(ctx, value)
		}
		endpoint.OnTrySend = func(payload []byte) (bool, error) {
			value, err := e.decodeChannelPayload(payload, elem)
			if err != nil {
				return false, err
			}
			ready, errText := ch.TrySend(value)
			if errText != "" {
				return ready, fmt.Errorf("%s", errText)
			}
			return ready, nil
		}
		endpoint.OnClose = ch.Close
	}
	return e.channelRegistry().RegisterChannel(endpoint)
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
