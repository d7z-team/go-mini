package rpc

import (
	"errors"
)

// FFIProtocol identifies the binary MRPC envelope carried by FFIRoute.
const FFIProtocol = "minigo.mrpc.ffi/v3"

type ffiRequest struct {
	Version      string
	Operation    string
	Target       string
	Lease        uint64
	Contract     Contract
	Options      BindOptions
	Method       Method
	Receiver     *ResourceRef
	Payload      []byte
	Resource     *ResourceRef
	RequestID    uint64
	Code         Code
	Message      string
	TimeoutNanos int64
}

type ffiResponse struct {
	Version   string
	Operation string
	Lease     uint64
	RequestID uint64
	Method    Method
	Receiver  *ResourceRef
	Payload   []byte
	Code      Code
	Message   string
}

func decodeFFIRequest(data []byte, limits Limits) (ffiRequest, error) {
	limits = normalizeLimits(limits)
	if len(data) > limits.MaxMessageBytes {
		return ffiRequest{}, errors.New("MRPC FFI request exceeds message limit")
	}
	decoder := newWireDecoder(data, limits.MaxMessageBytes)
	request := ffiRequest{}
	var err error
	request.Version, err = decoder.String()
	if err == nil {
		request.Operation, err = decoder.String()
	}
	if err == nil {
		request.Target, err = decoder.String()
	}
	if err == nil {
		request.Lease, err = decoder.Uint()
	}
	if err == nil {
		request.Contract.Protocol, err = decoder.String()
	}
	var methodCount uint64
	if err == nil {
		methodCount, err = decoder.Uint()
	}
	if err == nil && methodCount > uint64(limits.MaxMethods) {
		err = errors.New("MRPC FFI contract exceeds method limit")
	}
	if err == nil {
		request.Contract.Methods = make([]Method, int(methodCount))
		for index := range request.Contract.Methods {
			request.Contract.Methods[index], err = decodeWireMethod(decoder)
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		request.Options.AffinityKey, err = decoder.String()
	}
	if err == nil {
		request.Options.Labels, err = decodeWireLabels(decoder, limits.MaxMethods)
	}
	if err == nil {
		request.Method, err = decodeWireMethod(decoder)
	}
	if err == nil {
		request.Receiver, err = decodeWireResource(decoder)
	}
	if err == nil {
		request.Payload, err = decoder.rawView()
	}
	if err == nil {
		request.Resource, err = decodeWireResource(decoder)
	}
	if err == nil {
		request.RequestID, err = decoder.Uint()
	}
	if err == nil {
		var code string
		code, err = decoder.String()
		request.Code = Code(code)
	}
	if err == nil {
		request.Message, err = decoder.String()
	}
	if err == nil {
		request.TimeoutNanos, err = decoder.Int()
	}
	if err == nil {
		err = decoder.Done()
	}
	return request, err
}

func encodeFFIRequest(request ffiRequest) []byte {
	var encoder wireEncoder
	encoder.String(request.Version)
	encoder.String(request.Operation)
	encoder.String(request.Target)
	encoder.Uint(request.Lease)
	encoder.String(request.Contract.Protocol)
	encoder.Uint(uint64(len(request.Contract.Methods)))
	for _, method := range request.Contract.Methods {
		encodeWireMethod(&encoder, method)
	}
	encoder.String(request.Options.AffinityKey)
	encodeWireLabels(&encoder, request.Options.Labels)
	encodeWireMethod(&encoder, request.Method)
	encodeWireResource(&encoder, request.Receiver)
	encoder.Raw(request.Payload)
	encodeWireResource(&encoder, request.Resource)
	encoder.Uint(request.RequestID)
	encoder.String(string(request.Code))
	encoder.String(request.Message)
	encoder.Int(request.TimeoutNanos)
	return encoder.data
}

func encodeFFIResponse(response ffiResponse) []byte {
	var encoder wireEncoder
	encoder.String(response.Version)
	encoder.String(response.Operation)
	encoder.Uint(response.Lease)
	encoder.Uint(response.RequestID)
	encodeWireMethod(&encoder, response.Method)
	encodeWireResource(&encoder, response.Receiver)
	encoder.Raw(response.Payload)
	encoder.String(string(response.Code))
	encoder.String(response.Message)
	return encoder.data
}

func decodeFFIResponse(data []byte) (ffiResponse, error) {
	limits := normalizeLimits(Limits{})
	if len(data) > limits.MaxMessageBytes {
		return ffiResponse{}, errors.New("MRPC FFI response exceeds message limit")
	}
	decoder := newWireDecoder(data, limits.MaxMessageBytes)
	response := ffiResponse{}
	var err error
	response.Version, err = decoder.String()
	if err == nil {
		response.Operation, err = decoder.String()
	}
	if err == nil {
		response.Lease, err = decoder.Uint()
	}
	if err == nil {
		response.RequestID, err = decoder.Uint()
	}
	if err == nil {
		response.Method, err = decodeWireMethod(decoder)
	}
	if err == nil {
		response.Receiver, err = decodeWireResource(decoder)
	}
	if err == nil {
		response.Payload, err = decoder.Raw()
	}
	var code string
	if err == nil {
		code, err = decoder.String()
		response.Code = Code(code)
	}
	if err == nil {
		response.Message, err = decoder.String()
	}
	if err == nil {
		err = decoder.Done()
	}
	return response, err
}
