package bootstrap

import (
	"errors"
	"fmt"

	artifact "github.com/d7z-team/mini-go/runtime/bytecode"
)

func BuildCompilerImage(options BuildOptions) (artifact.ExecutionImage, error) {
	_, session, err := newCompilerSession(options)
	if err != nil {
		return artifact.ExecutionImage{}, err
	}
	defer session.Close()
	prepared, err := session.Prepare(compilerImage.Root, compilerImage.Entries)
	if err != nil {
		return artifact.ExecutionImage{}, err
	}
	if !prepared.Checked.OK() {
		diagnostic := prepared.Checked.Diagnostics[0]
		return artifact.ExecutionImage{}, fmt.Errorf("compile compiler image: %s:%d:%d: %s: %s", diagnostic.Primary.Start.File,
			diagnostic.Primary.Start.Line, diagnostic.Primary.Start.Column, diagnostic.Code, diagnostic.Message)
	}
	if prepared.Image == nil {
		return artifact.ExecutionImage{}, errors.New("compiler image has no execution image")
	}
	return *prepared.Image, nil
}
