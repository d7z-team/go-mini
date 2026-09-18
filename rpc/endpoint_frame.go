package rpc

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// EndpointProtocol identifies the binary Endpoint wire contract.
const EndpointProtocol = "minigo.rpc.endpoint.v12"

const (
	endpointFrameMagic   = "MGRP"
	endpointFrameVersion = byte(1)
)

const (
	endpointKindHello uint64 = iota + 1
	endpointKindReady
	endpointKindBind
	endpointKindCall
	endpointKindDecision
	endpointKindDrop
	endpointKindClose
	endpointKindCancel
	_
	endpointKindRenew
	endpointKindRenewAck
	endpointKindAccepted
	endpointKindOffer
	endpointKindDone
)

var endpointKinds = map[string]uint64{
	"hello": endpointKindHello, "ready": endpointKindReady, "bind": endpointKindBind,
	"call": endpointKindCall, "decision": endpointKindDecision, "drop": endpointKindDrop,
	"close": endpointKindClose, "cancel": endpointKindCancel,
	"renew": endpointKindRenew, "renew_ack": endpointKindRenewAck, "accepted": endpointKindAccepted,
	"offer": endpointKindOffer, "done": endpointKindDone,
}

type endpointFrame struct {
	Protocol         string
	Limits           *wireLimits
	Kind             string
	Origin           string
	ID               uint64
	TargetID         uint64
	Binding          uint64
	Reply            bool
	Epoch            uint64
	Contract         *Contract
	Options          BindOptions
	Hops             int
	Call             *wireCall
	Ref              *ResourceRef
	Values           []byte
	Accept           bool
	Code             Code
	Message          string
	Timeout          int64
	LeaseTTL         int64
	AdmissionTimeout int64
	MaxCallDuration  int64
	queuedAt         time.Time
	writeDone        chan error
}

type wireLimits struct {
	FrameBytes      int
	MessageBytes    int
	InFlightBytes   int
	Bindings        int
	PendingCalls    int
	PendingResults  int
	PendingControls int
	Resources       int
	Methods         int
	ValueDepth      int
	ValueElements   int
}

type wireCall struct {
	Method    Method
	Receiver  *ResourceRef
	Arguments []byte
}

func validateEndpointFrame(frame endpointFrame) error {
	if frame.Origin == "" || !utf8.ValidString(frame.Origin) {
		return errors.New("rpc frame has no origin")
	}
	if frame.LeaseTTL < 0 || frame.AdmissionTimeout < 0 || frame.MaxCallDuration < 0 {
		return errors.New("rpc frame has invalid lease duration")
	}
	if !utf8.ValidString(frame.Protocol) || !utf8.ValidString(string(frame.Code)) || !utf8.ValidString(frame.Message) ||
		!utf8.ValidString(frame.Options.AffinityKey) {
		return errors.New("rpc frame contains invalid UTF-8")
	}
	for key, value := range frame.Options.Labels {
		if !utf8.ValidString(key) || !utf8.ValidString(value) {
			return errors.New("rpc frame label contains invalid UTF-8")
		}
	}
	if frame.Contract != nil {
		if !utf8.ValidString(frame.Contract.Protocol) {
			return errors.New("rpc contract protocol contains invalid UTF-8")
		}
		for _, method := range frame.Contract.Methods {
			if !validWireMethodText(method) {
				return errors.New("rpc contract method contains invalid UTF-8")
			}
		}
	}
	if frame.Call != nil && (!validWireMethodText(frame.Call.Method) || frame.Call.Receiver != nil && !utf8.ValidString(frame.Call.Receiver.TypeHash)) {
		return errors.New("rpc call contains invalid UTF-8")
	}
	if frame.Ref != nil && !utf8.ValidString(frame.Ref.TypeHash) {
		return errors.New("rpc resource contains invalid UTF-8")
	}
	switch frame.Kind {
	case "hello":
		if frame.Reply || frame.Protocol == "" || frame.Limits == nil || frame.ID != 0 || frame.TargetID != 0 {
			return errors.New("invalid rpc hello frame")
		}
		if frame.LeaseTTL < int64(time.Millisecond) || frame.AdmissionTimeout < int64(time.Millisecond) {
			return errors.New("invalid rpc lease policy")
		}
		return validateWireLimits(*frame.Limits)
	case "ready":
		if frame.Reply || frame.ID != 0 || frame.TargetID != 0 {
			return errors.New("invalid rpc ready frame")
		}
		return nil
	case "bind", "call", "decision", "drop", "close", "cancel":
	case "renew", "renew_ack", "accepted", "offer", "done":
	default:
		return fmt.Errorf("unknown rpc frame kind %q", frame.Kind)
	}
	if frame.Reply {
		if frame.TargetID == 0 || frame.ID != 0 || frame.Kind == "cancel" {
			return errors.New("invalid rpc response frame")
		}
		if frame.Code == "" && frame.Kind == "bind" && (frame.Binding == 0 || frame.Epoch == 0) {
			return errors.New("invalid rpc bind response")
		}
		if frame.Code == "" && frame.Kind == "call" && frame.TargetID == 0 {
			return errors.New("invalid rpc call response")
		}
		if frame.Code == "" && frame.Kind == "offer" && frame.TargetID == 0 {
			return errors.New("invalid rpc offer response")
		}
		return nil
	}
	if frame.Timeout < 0 {
		return errors.New("invalid rpc timeout budget")
	}
	if frame.Kind == "cancel" {
		if frame.ID != 0 || frame.TargetID == 0 {
			return errors.New("invalid rpc cancel frame")
		}
		return nil
	}
	if frame.ID == 0 || frame.TargetID != 0 && frame.Kind != "decision" {
		return errors.New("invalid rpc request frame")
	}
	switch frame.Kind {
	case "bind":
		if frame.Contract == nil || frame.Contract.Protocol != ContractProtocol || len(frame.Contract.Methods) == 0 {
			return errors.New("invalid rpc bind contract")
		}
	case "call":
		if frame.Binding == 0 || frame.Call == nil {
			return errors.New("invalid rpc call frame")
		}
	case "decision":
		if frame.Binding == 0 || frame.TargetID == 0 {
			return errors.New("invalid rpc decision frame")
		}
	case "drop":
		if frame.Binding == 0 || frame.Ref == nil {
			return errors.New("invalid rpc drop frame")
		}
	case "close":
		if frame.Binding == 0 {
			return errors.New("invalid rpc close frame")
		}
	case "renew", "renew_ack", "accepted", "offer", "done":
		if frame.Kind == "renew_ack" || frame.Kind == "accepted" || frame.Kind == "offer" || frame.Kind == "done" {
			return errors.New("invalid rpc terminal frame direction")
		}
	}
	return nil
}

func validWireMethodText(method Method) bool {
	return utf8.ValidString(method.ID) && utf8.ValidString(method.Service) && utf8.ValidString(method.Name) &&
		utf8.ValidString(method.ContractHash) && utf8.ValidString(method.ResourceTypeHash)
}

func limitsToWire(limits Limits) wireLimits {
	return wireLimits{
		FrameBytes: limits.MaxFrameBytes, MessageBytes: limits.MaxMessageBytes, InFlightBytes: limits.MaxInFlightBytes,
		Bindings: limits.MaxBindings, PendingCalls: limits.MaxPendingCalls, PendingResults: limits.MaxPendingResults,
		PendingControls: limits.MaxPendingControls, Resources: limits.MaxResources, Methods: limits.MaxMethods,
		ValueDepth: limits.MaxValueDepth, ValueElements: limits.MaxValueElements,
	}
}

func limitsFromWire(limits wireLimits) Limits {
	return Limits{
		MaxFrameBytes: limits.FrameBytes, MaxMessageBytes: limits.MessageBytes, MaxInFlightBytes: limits.InFlightBytes,
		MaxBindings: limits.Bindings, MaxPendingCalls: limits.PendingCalls, MaxPendingResults: limits.PendingResults,
		MaxPendingControls: limits.PendingControls, MaxResources: limits.Resources, MaxMethods: limits.Methods,
		MaxValueDepth: limits.ValueDepth, MaxValueElements: limits.ValueElements,
	}
}

func validateWireLimits(limits wireLimits) error {
	if limits.FrameBytes < 256 || limits.MessageBytes <= 0 || limits.InFlightBytes < limits.MessageBytes ||
		limits.Bindings <= 0 || limits.PendingCalls <= 0 || limits.PendingResults <= 0 || limits.PendingControls <= 0 ||
		limits.Resources <= 0 || limits.Methods <= 0 || limits.ValueDepth <= 0 || limits.ValueElements <= 0 {
		return errors.New("invalid rpc endpoint limits")
	}
	return nil
}

func minLimits(local, peer Limits) Limits {
	result := local
	result.MaxFrameBytes = min(local.MaxFrameBytes, peer.MaxFrameBytes)
	result.MaxMessageBytes = min(local.MaxMessageBytes, peer.MaxMessageBytes)
	result.MaxInFlightBytes = min(local.MaxInFlightBytes, peer.MaxInFlightBytes)
	result.MaxBindings = min(local.MaxBindings, peer.MaxBindings)
	result.MaxPendingCalls = min(local.MaxPendingCalls, peer.MaxPendingCalls)
	result.MaxPendingResults = min(local.MaxPendingResults, peer.MaxPendingResults)
	result.MaxPendingControls = min(local.MaxPendingControls, peer.MaxPendingControls)
	result.MaxResources = min(local.MaxResources, peer.MaxResources)
	result.MaxMethods = min(local.MaxMethods, peer.MaxMethods)
	result.MaxValueDepth = min(local.MaxValueDepth, peer.MaxValueDepth)
	result.MaxValueElements = min(local.MaxValueElements, peer.MaxValueElements)
	return result
}
