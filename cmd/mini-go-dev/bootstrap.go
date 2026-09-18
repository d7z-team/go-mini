package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/d7z-team/mini-go/compiler/bootstrap"
	"github.com/d7z-team/mini-go/compiler/cache"
)

func runBootstrap(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("mini-go-dev bootstrap", flag.ContinueOnError)
	if stderr != nil {
		flags.SetOutput(stderr)
	}
	var repository string
	var output string
	var compressed bool
	flags.StringVar(&repository, "root", ".", "repository root")
	flags.StringVar(&output, "out", "", "write the compiler execution image")
	flags.BoolVar(&compressed, "gzip", false, "compress the compiler execution image deterministically")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(repository) == "" {
		return errors.New("bootstrap accepts only -root, -out and -gzip")
	}
	cacheRoot := strings.TrimSpace(os.Getenv("MINIGO_CACHE"))
	if cacheRoot != "" && !filepath.IsAbs(cacheRoot) {
		return errors.New("MINIGO_CACHE must be an absolute path")
	}
	root, err := cache.ResolveDiskRoot(cacheRoot)
	if err != nil {
		return err
	}
	backend := cache.NewDiskBackend(root)
	_ = backend.Trim()
	options := bootstrap.BuildOptions{Filesystem: os.DirFS(repository), Root: ".", Cache: backend}
	image, err := bootstrap.BuildCompilerImage(options)
	if err != nil {
		return err
	}
	if output != "" {
		var data bytes.Buffer
		encoder := json.NewEncoder(&data)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(image); err != nil {
			return err
		}
		write := writeGeneratedFile
		if compressed {
			write = writeGeneratedGzip
		}
		if err := write(output, data.Bytes()); err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"compiler": image.CompilerID, "image": image.Hash})
}
