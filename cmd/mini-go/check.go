package main

import (
	"errors"
	"io"
)

func runCheck(environment commandEnvironment, args []string, stderr io.Writer) error {
	options, operands, err := parseBuildOptions("mini-go check", args, stderr, false, false)
	if err != nil {
		return err
	}
	build, err := loadBuildContext(environment, options, operands)
	if err != nil {
		return err
	}
	session, err := build.openCompiler()
	if err != nil {
		return err
	}
	checked, err := session.CheckPackages(build.roots)
	if err != nil {
		return err
	}
	if !checked.OK() {
		writeDiagnostics(stderr, checked.Diagnostics)
		return errors.New("compilation failed")
	}
	return nil
}
