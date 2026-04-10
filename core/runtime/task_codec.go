package runtime

import (
	"encoding/json"
	"fmt"
)

type taskJSON struct {
	Op       OpCode          `json:"op"`
	Source   *SourceRef      `json:"source,omitempty"`
	DataKind string          `json:"data_kind,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

type literalTaskValue struct {
	Type   RuntimeType `json:"type"`
	VType  VarType     `json:"vtype"`
	I64    int64       `json:"i64,omitempty"`
	F64    float64     `json:"f64,omitempty"`
	Str    string      `json:"str,omitempty"`
	B      []byte      `json:"b,omitempty"`
	Bool   bool        `json:"bool,omitempty"`
	Handle uint32      `json:"handle,omitempty"`
}

func (t Task) MarshalJSON() ([]byte, error) {
	payload := taskJSON{
		Op:     t.Op,
		Source: t.Source,
	}
	if t.Data != nil {
		kind, raw, err := marshalTaskData(t.Op, t.Data)
		if err != nil {
			return nil, err
		}
		payload.DataKind = kind
		payload.Data = raw
	}
	return json.Marshal(payload)
}

func (t *Task) UnmarshalJSON(data []byte) error {
	var payload taskJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	taskData, err := unmarshalTaskData(payload.Op, payload.DataKind, payload.Data)
	if err != nil {
		return err
	}
	t.Op = payload.Op
	t.Source = payload.Source
	t.Data = taskData
	return nil
}

func marshalTaskData(op OpCode, data interface{}) (string, json.RawMessage, error) {
	switch op {
	case OpDeclareVar:
		return marshalTaskDataValue("declare_var", data)
	case OpApplyBinary, OpApplyUnary, OpIncDec, OpInterrupt, OpMember, OpInitVar, OpScopeEnter:
		return marshalTaskDataValue("string", data)
	case OpMultiAssign, OpReturn:
		return marshalTaskDataValue("int", data)
	case OpScheduleDefer:
		return marshalTaskDataValue("defer", data)
	case OpFinally:
		return marshalTaskDataValue("finally", data)
	case OpCatchBoundary:
		return marshalTaskDataValue("catch", data)
	case OpLoopBoundary:
		switch data.(type) {
		case *ForData:
			return marshalTaskDataValue("for", data)
		case *SwitchData:
			return marshalTaskDataValue("switch", data)
		default:
			return "", nil, fmt.Errorf("unsupported loop boundary payload: %T", data)
		}
	case OpForCond:
		return marshalTaskDataValue("for", data)
	case OpRangeInit, OpRangeIter:
		return marshalTaskDataValue("range", data)
	case OpJumpIf:
		return marshalTaskDataValue("jump", data)
	case OpBranchIf:
		return marshalTaskDataValue("branch", data)
	case OpCall:
		return marshalTaskDataValue("call", data)
	case OpComposite:
		return marshalTaskDataValue("composite", data)
	case OpIndex:
		return marshalTaskDataValue("index", data)
	case OpSlice:
		return marshalTaskDataValue("slice", data)
	case OpLoadVar:
		return marshalTaskDataValue("load_var", data)
	case OpLoadLocal, OpLoadUpvalue, OpStoreLocal, OpStoreUpvalue:
		return marshalTaskDataValue("symbol", data)
	case OpImportInit:
		return marshalTaskDataValue("import", data)
	case OpSwitchTag:
		return marshalTaskDataValue("switch", data)
	case OpAssert:
		return marshalTaskDataValue("assert", data)
	case OpPush:
		if data == nil {
			return "nil", nil, nil
		}
		v, ok := data.(*Var)
		if !ok {
			return "", nil, fmt.Errorf("unsupported push payload: %T", data)
		}
		literal := literalTaskValue{
			Type:   v.RuntimeType(),
			VType:  v.VType,
			I64:    v.I64,
			F64:    v.F64,
			Str:    v.Str,
			B:      v.B,
			Bool:   v.Bool,
			Handle: v.Handle,
		}
		raw, err := json.Marshal(literal)
		return "literal_var", raw, err
	case OpMakeClosure:
		return marshalTaskDataValue("closure", data)
	case OpEvalLHS:
		return marshalTaskDataValue("lhs", data)
	case OpLineStep, OpAssign, OpPop, OpScopeExit, OpLoopContinue:
		return "", nil, nil
	default:
		return "", nil, fmt.Errorf("opcode %s is not serializable", op.String())
	}
}

func marshalTaskDataValue(kind string, data interface{}) (string, json.RawMessage, error) {
	raw, err := json.Marshal(data)
	return kind, raw, err
}

func unmarshalTaskData(op OpCode, kind string, raw json.RawMessage) (interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	switch op {
	case OpDeclareVar:
		return decodeTaskData[*DeclareVarData](raw)
	case OpApplyBinary, OpApplyUnary, OpIncDec, OpInterrupt, OpMember, OpInitVar, OpScopeEnter:
		return decodeTaskData[string](raw)
	case OpMultiAssign, OpReturn:
		return decodeTaskData[int](raw)
	case OpScheduleDefer:
		return decodeTaskData[*DeferData](raw)
	case OpFinally:
		return decodeTaskData[*FinallyData](raw)
	case OpCatchBoundary:
		return decodeTaskData[*CatchData](raw)
	case OpLoopBoundary:
		switch kind {
		case "for":
			return decodeTaskData[*ForData](raw)
		case "switch":
			return decodeTaskData[*SwitchData](raw)
		default:
			return nil, fmt.Errorf("unsupported loop boundary kind: %s", kind)
		}
	case OpForCond:
		return decodeTaskData[*ForData](raw)
	case OpRangeInit, OpRangeIter:
		return decodeTaskData[*RangeData](raw)
	case OpJumpIf:
		return decodeTaskData[*JumpData](raw)
	case OpBranchIf:
		return decodeTaskData[*BranchData](raw)
	case OpCall:
		return decodeTaskData[*CallData](raw)
	case OpComposite:
		return decodeTaskData[*CompositeData](raw)
	case OpIndex:
		return decodeTaskData[*IndexData](raw)
	case OpSlice:
		return decodeTaskData[*SliceData](raw)
	case OpLoadVar:
		return decodeTaskData[*LoadVarData](raw)
	case OpLoadLocal, OpLoadUpvalue, OpStoreLocal, OpStoreUpvalue:
		return decodeTaskData[SymbolRef](raw)
	case OpImportInit:
		return decodeTaskData[*ImportInitData](raw)
	case OpSwitchTag:
		return decodeTaskData[*SwitchData](raw)
	case OpAssert:
		return decodeTaskData[*AssertData](raw)
	case OpPush:
		if kind == "nil" {
			return nil, nil
		}
		literal, err := decodeTaskData[literalTaskValue](raw)
		if err != nil {
			return nil, err
		}
		v := &Var{
			VType:  literal.VType,
			I64:    literal.I64,
			F64:    literal.F64,
			Str:    literal.Str,
			B:      literal.B,
			Bool:   literal.Bool,
			Handle: literal.Handle,
		}
		v.SetRuntimeType(literal.Type)
		return v, nil
	case OpMakeClosure:
		return decodeTaskData[*ClosureData](raw)
	case OpEvalLHS:
		return decodeTaskData[*LHSData](raw)
	case OpLineStep, OpAssign, OpPop, OpScopeExit, OpLoopContinue:
		return nil, nil
	default:
		return nil, fmt.Errorf("opcode %s is not deserializable", op.String())
	}
}

func decodeTaskData[T any](raw json.RawMessage) (T, error) {
	var out T
	err := json.Unmarshal(raw, &out)
	return out, err
}
