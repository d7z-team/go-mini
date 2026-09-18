package runtime

import "context"

type runControllerContextKey struct{}

func ContextWithRunController(ctx context.Context, controller *RunController) context.Context {
	if controller == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, runControllerContextKey{}, controller)
}

func RunControllerFromContext(ctx context.Context) *RunController {
	if ctx == nil {
		return nil
	}
	controller, _ := ctx.Value(runControllerContextKey{}).(*RunController)
	return controller
}

func VMTimerServiceFromContext(ctx context.Context) *VMTimerService {
	controller := RunControllerFromContext(ctx)
	if controller == nil {
		return nil
	}
	return controller.TimerService()
}
