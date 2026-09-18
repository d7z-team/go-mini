package runtime

import (
	"errors"
	"fmt"
)

type reflectMakeFuncTarget struct {
	functionType  vmType
	params        []vmType
	results       []vmType
	variadic      bool
	handler       functionRef
	handlerModule *moduleInstance
}

func reflectMakeFunc(ctx intrinsicContext, args []vmValue) ([]vmValue, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("reflect.make_func expects 2 arguments, got %d", len(args))
	}
	info, err := reflectResolvedTypeInfo(ctx, args[0])
	if err != nil {
		return reflectValueError(err.Error()), nil
	}
	module := reflectRelationModule(ctx)
	if module == nil {
		return reflectValueError("reflect: MakeFunc requires module context"), nil
	}
	functionType := module.resolvedRuntimeType(info.Key)
	function, ok := functionType.FunctionInfo()
	if !ok {
		return reflectValueError("reflect: call of MakeFunc with non-Func type"), nil
	}
	underlying := functionType.Underlying()
	if !underlying.Valid() {
		return reflectValueError("reflect: MakeFunc requires a valid function type"), nil
	}
	params := make([]vmType, len(function.Signature.Params))
	for index, param := range function.Signature.Params {
		params[index] = vmType{Ref: param.Type, Table: underlying.Table}
	}
	results := make([]vmType, len(function.Signature.Results))
	for index, result := range function.Signature.Results {
		results[index] = vmType{Ref: result, Table: underlying.Table}
	}
	if ctx.vm == nil {
		return reflectValueError("reflect: MakeFunc requires VM context"), nil
	}
	handlerModule, handler, err := ctx.vm.resolveFunctionValue(reflectRelationModule(ctx), args[1])
	if err != nil {
		return reflectValueError("reflect: MakeFunc handler is not callable"), nil
	}
	handlerFunction := handlerModule.executable.Functions[handler.FunctionID]
	if len(handlerFunction.Decl.Signature.Params) != 1 || len(handlerFunction.Decl.Signature.Results) != 1 {
		return reflectValueError("reflect: MakeFunc handler must have type func([]reflect.Value) []reflect.Value"), nil
	}
	handler.exact = handlerModule
	target := reflectMakeFuncTarget{
		functionType: functionType, params: params, results: results, variadic: function.Signature.Variadic,
		handler: handler, handlerModule: handlerModule,
	}
	value := newVMValue(functionType, target)
	out, err := reflectValueSnapshot(intrinsicContext{vm: ctx.vm, module: handlerModule}, value)
	if err != nil {
		return reflectValueError(err.Error()), nil
	}
	return reflectValueOK(out), nil
}

func (target reflectMakeFuncTarget) callbackRequest(vm *vm, args []vmValue) (*artifactCallbackRequest, error) {
	if vm == nil || target.handlerModule == nil {
		return nil, errors.New("reflect: MakeFunc target has no VM context")
	}
	if len(args) != len(target.params) {
		return nil, fmt.Errorf("reflect: MakeFunc call received %d arguments, want %d", len(args), len(target.params))
	}
	reflected := make([]vmValue, 0, len(args))
	ctx := intrinsicContext{vm: vm, module: target.handlerModule}
	for index, arg := range args {
		value, err := reflectValueSnapshot(ctx, arg)
		if err != nil {
			return nil, fmt.Errorf("reflect: MakeFunc argument %d: %w", index, err)
		}
		reflected = append(reflected, value)
	}
	handlerFunction, ok := target.handlerModule.executable.Functions[target.handler.FunctionID]
	if !ok {
		return nil, fmt.Errorf("reflect: MakeFunc handler %s is missing", target.handler.FunctionID)
	}
	request := &artifactCallbackRequest{
		module: target.handlerModule, functionID: target.handler.FunctionID,
		args:     []vmValue{newSliceValue("Slice<reflect.Value>", reflected)},
		upvalues: target.handler.upvalues, resultCount: len(handlerFunction.ResultTypes),
	}
	request.resume = func(values []vmValue) ([]vmValue, error) {
		if len(values) != 1 {
			return nil, fmt.Errorf("reflect: MakeFunc handler returned %d values, want one result slice", len(values))
		}
		reflectedResults, err := reflectValueCallArguments(values[0])
		if err != nil {
			return nil, fmt.Errorf("reflect: MakeFunc handler result: %w", err)
		}
		if len(reflectedResults) != len(target.results) {
			return nil, fmt.Errorf("reflect: wrong return count from function created by MakeFunc: got %d, want %d", len(reflectedResults), len(target.results))
		}
		out := make([]vmValue, 0, len(target.results))
		for index, reflectedResult := range reflectedResults {
			result, err := reflectArgumentRuntimeValue(reflectedResult)
			if err != nil {
				return nil, fmt.Errorf("reflect: MakeFunc result %d: %w", index, err)
			}
			normalized, err := target.handlerModule.coerceAssignableValue(result, target.results[index])
			if err != nil {
				return nil, fmt.Errorf("reflect: MakeFunc result %d: %w", index, err)
			}
			out = append(out, normalized)
		}
		return out, nil
	}
	return request, nil
}
