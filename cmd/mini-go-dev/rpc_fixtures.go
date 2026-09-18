package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"

	"github.com/d7z-team/mini-go/tooling/rpccheck"
)

func runRPCFixtures(args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("rpc-fixtures", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "testdata/rpc", "RPC fixture directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	outputs, err := rpccheck.GenerateFixtures(os.DirFS(*root))
	if err != nil {
		return err
	}
	for name, data := range outputs {
		if err := writeGeneratedFile(filepath.Join(*root, filepath.FromSlash(name)), data); err != nil {
			return err
		}
	}
	return nil
}
