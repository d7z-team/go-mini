package engine_test

import engine "gopkg.d7z.net/go-mini/core"

type pipelineLoader struct {
	name string
	load func(*engine.MiniExecutor) (*engine.ExecutableProgram, error)
}

func pipelineLoaders(code string) []pipelineLoader {
	return []pipelineLoader{
		{
			name: "source",
			load: func(exec *engine.MiniExecutor) (*engine.ExecutableProgram, error) {
				return exec.NewRuntimeByGoCode(code)
			},
		},
		{
			name: "compiled",
			load: func(exec *engine.MiniExecutor) (*engine.ExecutableProgram, error) {
				compiled, err := exec.CompileGoCode(code)
				if err != nil {
					return nil, err
				}
				return exec.NewRuntimeByCompiled(compiled)
			},
		},
		{
			name: "bytecode_json",
			load: func(exec *engine.MiniExecutor) (*engine.ExecutableProgram, error) {
				compiled, err := exec.CompileGoCode(code)
				if err != nil {
					return nil, err
				}
				payload, err := compiled.MarshalBytecodeJSON()
				if err != nil {
					return nil, err
				}
				return exec.NewRuntimeByBytecodeJSON(payload)
			},
		},
	}
}
