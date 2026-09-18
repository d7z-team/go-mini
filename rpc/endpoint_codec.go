package rpc

import (
	"errors"
	"fmt"
)

func encodeEndpointFrame(frame endpointFrame, maxBytes int) ([]byte, error) {
	kind, ok := endpointKinds[frame.Kind]
	if !ok {
		return nil, StatusError{Code: CodeProtocol, Message: fmt.Sprintf("unknown rpc frame kind %q", frame.Kind)}
	}
	if err := validateEndpointFrame(frame); err != nil {
		return nil, StatusError{Code: CodeProtocol, Message: err.Error()}
	}
	var encoder wireEncoder
	encoder.data = append(encoder.data, endpointFrameMagic...)
	encoder.data = append(encoder.data, endpointFrameVersion)
	encoder.Uint(kind)
	encoder.String(frame.Protocol)
	encodeWireLimits(&encoder, frame.Limits)
	encoder.Int(frame.LeaseTTL)
	encoder.Int(frame.AdmissionTimeout)
	encoder.Int(frame.MaxCallDuration)
	encoder.String(frame.Origin)
	encoder.Uint(frame.ID)
	encoder.Uint(frame.TargetID)
	encoder.Uint(frame.Binding)
	encoder.Bool(frame.Reply)
	encoder.Uint(frame.Epoch)
	encodeWireContract(&encoder, frame.Contract)
	encoder.String(frame.Options.AffinityKey)
	encodeWireLabels(&encoder, frame.Options.Labels)
	encoder.Int(int64(frame.Hops))
	encodeWireCall(&encoder, frame.Call)
	encodeWireResource(&encoder, frame.Ref)
	encoder.Raw(frame.Values)
	encoder.Bool(frame.Accept)
	encoder.String(string(frame.Code))
	encoder.String(frame.Message)
	encoder.Int(frame.Timeout)
	if len(encoder.data) == 0 || len(encoder.data) > maxBytes {
		return nil, StatusError{Code: CodeResourceExhausted, Message: "rpc message size limit exceeded"}
	}
	return encoder.data, nil
}

func decodeEndpointFrame(payload []byte, limits Limits) (endpointFrame, error) {
	limits = normalizeLimits(limits)
	if len(payload) == 0 || len(payload) > limits.MaxMessageBytes {
		return endpointFrame{}, StatusError{Code: CodeProtocol, Message: "invalid rpc message size"}
	}
	decoder := newWireDecoder(payload, limits.MaxMessageBytes)
	magic, err := decoder.fixed(len(endpointFrameMagic))
	if err != nil || string(magic) != endpointFrameMagic {
		return endpointFrame{}, StatusError{Code: CodeProtocol, Message: "invalid rpc frame magic"}
	}
	version, err := decoder.byte()
	if err != nil || version != endpointFrameVersion {
		return endpointFrame{}, StatusError{Code: CodeProtocol, Message: "invalid rpc frame version"}
	}
	kind, err := decoder.Uint()
	if err != nil {
		return endpointFrame{}, protocolDecodeError(err)
	}
	frame := endpointFrame{Kind: endpointKindName(kind)}
	if frame.Kind == "" {
		return endpointFrame{}, StatusError{Code: CodeProtocol, Message: "unknown rpc frame kind"}
	}
	frame.Protocol, err = decoder.String()
	if err == nil {
		frame.Limits, err = decodeWireLimits(decoder)
	}
	if err == nil {
		frame.LeaseTTL, err = decoder.Int()
	}
	if err == nil {
		frame.AdmissionTimeout, err = decoder.Int()
	}
	if err == nil {
		frame.MaxCallDuration, err = decoder.Int()
	}
	if err == nil {
		frame.Origin, err = decoder.String()
	}
	if err == nil {
		frame.ID, err = decoder.Uint()
	}
	if err == nil {
		frame.TargetID, err = decoder.Uint()
	}
	if err == nil {
		frame.Binding, err = decoder.Uint()
	}
	if err == nil {
		frame.Reply, err = decoder.Bool()
	}
	if err == nil {
		frame.Epoch, err = decoder.Uint()
	}
	if err == nil {
		frame.Contract, err = decodeWireContract(decoder, limits.MaxMethods)
	}
	if err == nil {
		frame.Options.AffinityKey, err = decoder.String()
	}
	if err == nil {
		frame.Options.Labels, err = decodeWireLabels(decoder, limits.MaxMethods)
	}
	var hops int64
	if err == nil {
		hops, err = decoder.Int()
	}
	if err == nil && int64(int(hops)) != hops {
		err = errors.New("rpc frame hop count overflows int")
	}
	frame.Hops = int(hops)
	if err == nil {
		frame.Call, err = decodeWireCall(decoder)
	}
	if err == nil {
		frame.Ref, err = decodeWireResource(decoder)
	}
	if err == nil {
		frame.Values, err = decoder.rawView()
	}
	if err == nil {
		frame.Accept, err = decoder.Bool()
	}
	if err == nil {
		var code string
		code, err = decoder.String()
		frame.Code = Code(code)
	}
	if err == nil {
		frame.Message, err = decoder.String()
	}
	if err == nil {
		frame.Timeout, err = decoder.Int()
	}
	if err == nil {
		err = decoder.Done()
	}
	if err != nil {
		return endpointFrame{}, protocolDecodeError(err)
	}
	if err := validateEndpointFrame(frame); err != nil {
		return endpointFrame{}, StatusError{Code: CodeProtocol, Message: err.Error()}
	}
	return frame, nil
}

func endpointKindName(kind uint64) string {
	for name, value := range endpointKinds {
		if value == kind {
			return name
		}
	}
	return ""
}

func protocolDecodeError(err error) error {
	return StatusError{Code: CodeProtocol, Message: "invalid rpc frame: " + err.Error()}
}

func encodeWireLimits(encoder *wireEncoder, limits *wireLimits) {
	encoder.Bool(limits != nil)
	if limits == nil {
		return
	}
	for _, value := range []int{
		limits.FrameBytes, limits.MessageBytes, limits.InFlightBytes, limits.Bindings,
		limits.PendingCalls, limits.PendingResults, limits.PendingControls, limits.Resources, limits.Methods,
		limits.ValueDepth, limits.ValueElements,
	} {
		encoder.Uint(uint64(value))
	}
}

func decodeWireLimits(decoder *wireDecoder) (*wireLimits, error) {
	present, err := decoder.Bool()
	if err != nil || !present {
		return nil, err
	}
	values := make([]int, 11)
	for index := range values {
		value, decodeErr := decoder.Uint()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if value > uint64(^uint(0)>>1) {
			return nil, errors.New("rpc endpoint limit overflows int")
		}
		values[index] = int(value)
	}
	return &wireLimits{
		FrameBytes: values[0], MessageBytes: values[1], InFlightBytes: values[2], Bindings: values[3],
		PendingCalls: values[4], PendingResults: values[5], PendingControls: values[6], Resources: values[7],
		Methods: values[8], ValueDepth: values[9], ValueElements: values[10],
	}, nil
}

func encodeWireContract(encoder *wireEncoder, contract *Contract) {
	encoder.Bool(contract != nil)
	if contract == nil {
		return
	}
	encoder.String(contract.Protocol)
	encoder.Uint(uint64(len(contract.Methods)))
	for _, method := range contract.Methods {
		encodeWireMethod(encoder, method)
	}
}

func decodeWireContract(decoder *wireDecoder, maxMethods int) (*Contract, error) {
	present, err := decoder.Bool()
	if err != nil || !present {
		return nil, err
	}
	contract := &Contract{}
	contract.Protocol, err = decoder.String()
	var count uint64
	if err == nil {
		count, err = decoder.Uint()
	}
	if err == nil && count > uint64(maxMethods) {
		err = errors.New("rpc contract method limit exceeded")
	}
	if err != nil {
		return nil, err
	}
	contract.Methods = make([]Method, int(count))
	for index := range contract.Methods {
		contract.Methods[index], err = decodeWireMethod(decoder)
		if err != nil {
			return nil, err
		}
	}
	return contract, nil
}

func encodeWireCall(encoder *wireEncoder, call *wireCall) {
	encoder.Bool(call != nil)
	if call == nil {
		return
	}
	encodeWireMethod(encoder, call.Method)
	encodeWireResource(encoder, call.Receiver)
	encoder.Raw(call.Arguments)
}

func decodeWireCall(decoder *wireDecoder) (*wireCall, error) {
	present, err := decoder.Bool()
	if err != nil || !present {
		return nil, err
	}
	call := &wireCall{}
	call.Method, err = decodeWireMethod(decoder)
	if err == nil {
		call.Receiver, err = decodeWireResource(decoder)
	}
	if err == nil {
		call.Arguments, err = decoder.rawView()
	}
	return call, err
}
