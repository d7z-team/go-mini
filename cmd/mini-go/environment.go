package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type commandEnvironment struct {
	workingDir string
	getenv     func(string) string
	tempDir    string
	ctx        context.Context
	stdin      io.Reader
}

func newCommandEnvironment(directory string) (commandEnvironment, error) {
	if directory == "" {
		directory = "."
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return commandEnvironment{}, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return commandEnvironment{}, err
	}
	if !info.IsDir() {
		return commandEnvironment{}, fmt.Errorf("working directory %q is not a directory", absolute)
	}
	return commandEnvironment{workingDir: absolute, getenv: os.Getenv, tempDir: os.TempDir(), ctx: context.Background()}, nil
}

func (environment commandEnvironment) context() context.Context {
	if environment.ctx == nil {
		return context.Background()
	}
	return environment.ctx
}

func (environment commandEnvironment) path(name string) string {
	if name == "" || filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(environment.workingDir, name)
}

func (environment commandEnvironment) variable(name string) string {
	if environment.getenv == nil {
		return os.Getenv(name)
	}
	return environment.getenv(name)
}

func (environment commandEnvironment) temporaryDirectory() string {
	if environment.tempDir != "" {
		return environment.tempDir
	}
	return os.TempDir()
}
