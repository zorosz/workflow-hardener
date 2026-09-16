package hardener

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

type fileFlags []string

func (f *fileFlags) String() string         { return fmt.Sprint([]string(*f)) }
func (f *fileFlags) Set(value string) error { *f = append(*f, value); return nil }

const usage = "usage: hardener scan --root DIR --file RELPATH [--file RELPATH]"

// Run parses CLI arguments, scans the requested files, and writes a JSON report.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	if args[0] != "scan" {
		fmt.Fprintln(stderr, "unknown command; "+usage)
		return 2
	}
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "input root directory")
	var names fileFlags
	flags.Var(&names, "file", "slash-separated path relative to root (repeatable)")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	in, err := OpenInputs(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer in.Close()
	report, err := Scan(in, names)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := writeJSON(stdout, report); err != nil {
		fmt.Fprintln(stderr, "cannot write scan report")
		return 2
	}
	return report.ExitCode
}
