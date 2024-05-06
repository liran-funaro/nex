package nex

import (
	"flag"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
)

type ExecParams struct {
	Standalone           bool
	CustomError          bool
	CustomPrefix         string
	InputFilename        string
	OutputFilename       string
	NfaDotOutputFilename string
	DfaDotOutputFilename string
	RunProgram           bool
	Stdin                io.Reader
	Stdout               io.Writer
	Stderr               io.Writer
}

func Exec(name string, args ...string) {
	f := flag.NewFlagSet(name, flag.ExitOnError)
	p := &ExecParams{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	f.StringVar(&p.CustomPrefix, "p", "", `name prefix to use in generated code`)
	f.BoolVar(&p.Standalone, "s", false, `standalone code; NN_FUN macro substitution, no Lex() method`)
	f.BoolVar(&p.CustomError, "e", false, `custom error func; no Error() method`)
	f.StringVar(&p.OutputFilename, "o", "", `output file`)
	f.StringVar(&p.NfaDotOutputFilename, "nfadot", "", `show NFA graph in DOT format`)
	f.StringVar(&p.DfaDotOutputFilename, "dfadot", "", `show DFA graph in DOT format`)
	f.BoolVar(&p.RunProgram, "r", false, `run generated program`)

	// Ignore errors; CommandLine is set for ExitOnError.
	_ = f.Parse(args)

	mustf(f.NArg() < 2, "extraneous arguments after %s", f.Arg(0))
	if f.NArg() > 0 {
		p.InputFilename = f.Arg(0)
	}

	ExecWithParams(p)
}

func ExecWithParams(p *ExecParams) {
	b := &Builder{
		CustomPrefix: p.CustomPrefix,
		Standalone:   p.Standalone,
		CustomError:  p.CustomError,
	}

	if p.RunProgram && p.OutputFilename == "" {
		tmpdir, err := os.MkdirTemp("", "nex")
		noError(err, "tempdir")
		defer func() {
			_ = os.RemoveAll(tmpdir)
		}()
		p.OutputFilename = path.Join(tmpdir, "lets.go")
	}

	infile := os.Stdin
	if p.InputFilename != "" {
		var err error
		infile, err = os.Open(p.InputFilename)
		noError(err, "open")
		defer func() {
			_ = infile.Close()
		}()
		if p.OutputFilename == "" {
			basename := strings.TrimSuffix(p.InputFilename, path.Ext(p.InputFilename))
			p.OutputFilename = basename + ".nn.go"
		}
	}

	noError(b.Process(infile), "process")

	if p.NfaDotOutputFilename != "" && b.Result.NfaDot != nil {
		noError(os.WriteFile(p.NfaDotOutputFilename, b.Result.NfaDot, 0666), "write")
	}
	if p.DfaDotOutputFilename != "" && b.Result.DfaDot != nil {
		noError(os.WriteFile(p.DfaDotOutputFilename, b.Result.DfaDot, 0666), "write")
	}
	if p.OutputFilename == "" || b.Result.Lexer == nil {
		return
	}

	noError(os.WriteFile(p.OutputFilename, b.Result.Lexer, 0666), "write")

	if p.RunProgram {
		c := exec.Command("go", "run", p.OutputFilename)
		c.Stdin, c.Stdout, c.Stderr = p.Stdin, p.Stdout, p.Stderr
		noError(c.Run(), "go run")
	}
}
