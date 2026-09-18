package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	miniformat "github.com/d7z-team/mini-go/tooling/format"
)

func runFormat(environment commandEnvironment, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("mini-go fmt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	write := flags.Bool("w", false, "write result to source files")
	list := flags.Bool("l", false, "list files whose formatting differs")
	diff := flags.Bool("d", false, "display formatting differences")
	check := flags.Bool("check", false, "report files that are not formatted")
	modulePath := flags.String("module", "command-line", "module path used for parsing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *write && *check {
		return errors.New("-w and -check cannot be used together")
	}
	paths := flags.Args()
	if len(paths) == 0 {
		if *write || *list || *diff || *check {
			return errors.New("-w, -l, -d and -check require file paths")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		result := miniformat.Source(*modulePath, "stdin.mgo", string(data))
		if len(result.Diagnostics) != 0 {
			return fmt.Errorf("stdin: %s: %s", result.Diagnostics[0].Code, result.Diagnostics[0].Message)
		}
		_, err = io.WriteString(stdout, result.Text)
		return err
	}
	type formattedFile struct {
		name     string
		path     string
		original string
		text     string
		mode     fs.FileMode
	}
	files := make([]formattedFile, 0, len(paths))
	for _, name := range paths {
		if !strings.HasSuffix(name, ".mgo") && !strings.HasSuffix(name, ".mrpc") {
			return fmt.Errorf("format input %q must use .mgo or .mrpc", name)
		}
		path := environment.path(name)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		result := miniformat.Source(strings.TrimSpace(*modulePath), name, string(data))
		if len(result.Diagnostics) != 0 {
			return fmt.Errorf("%s: %s: %s", name, result.Diagnostics[0].Code, result.Diagnostics[0].Message)
		}
		files = append(files, formattedFile{name: name, path: path, original: string(data), text: result.Text, mode: info.Mode()})
	}
	changed := false
	for _, file := range files {
		if file.text == file.original {
			continue
		}
		changed = true
		if *list || *check {
			fmt.Fprintln(stdout, file.name)
		}
		if *diff {
			fmt.Fprintf(stdout, "--- %s\n+++ %s (formatted)\n%s", file.name, file.name, file.text)
		}
		if *write {
			if err := writeFileAtomically(file.path, []byte(file.text), file.mode); err != nil {
				return err
			}
		}
		if !*write && !*list && !*diff {
			if *check {
				continue
			}
			if _, err := io.WriteString(stdout, file.text); err != nil {
				return err
			}
		}
	}
	if *check && changed {
		return errors.New("Mini-Go source is not formatted")
	}
	return nil
}
