package exec

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/liran-funaro/nex/parser"
	"github.com/liran-funaro/nex/writer"
)

type Params struct {
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

func ParseParams(name string, args ...string) (*Params, error) {
	f := flag.NewFlagSet(name, flag.ExitOnError)
	p := &Params{
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

	if f.NArg() > 1 {
		return nil, fmt.Errorf("extraneous arguments after %s", f.Arg(0))
	}
	if f.NArg() > 0 {
		p.InputFilename = f.Arg(0)
	}
	return p, nil
}

func Execute(name string, args ...string) error {
	p, err := ParseParams(name, args...)
	if err != nil {
		return fmt.Errorf("parse-params: %w", err)
	}
	return ExecuteWithParams(p)
}

func ExecuteWithParams(p *Params) error {
	var err error
	program, err := p.parseNex()
	if err != nil {
		return fmt.Errorf("parse-program: %w", err)
	}
	if err = writeWithWriter(p.NfaDotOutputFilename, program.WriteNFADotGraph); err != nil {
		return err
	}
	if err = writeWithWriter(p.DfaDotOutputFilename, program.WriteDFADotGraph); err != nil {
		return err
	}

	if p.RunProgram && p.OutputFilename == "" {
		tmpdir, err := os.MkdirTemp("", "nex")
		if err != nil {
			return fmt.Errorf("temp-dir: %w", err)
		}
		defer func() {
			_ = os.RemoveAll(tmpdir)
		}()
		p.OutputFilename = path.Join(tmpdir, "lets.go")
	}

	if p.InputFilename != "" && p.OutputFilename == "" {
		basename := strings.TrimSuffix(p.InputFilename, path.Ext(p.InputFilename))
		p.OutputFilename = basename + ".nn.go"
	}
	if p.OutputFilename == "" {
		return nil
	}

	b := &writer.LexerBuilder{
		CustomPrefix: p.CustomPrefix,
		Standalone:   p.Standalone,
		CustomError:  p.CustomError,
	}
	code, err := b.DumpFormattedLexer(program)
	if err != nil {
		return fmt.Errorf("dump lexer: %w", err)
	}
	if code == nil {
		return nil
	}

	if err := os.WriteFile(p.OutputFilename, code, 0666); err != nil {
		return fmt.Errorf("write lexer: %w", err)
	}

	if !p.RunProgram {
		return nil
	}

	c := exec.Command("go", "run", p.OutputFilename)
	c.Stdin, c.Stdout, c.Stderr = p.Stdin, p.Stdout, p.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("run lexer: %w", err)
	}
	return nil
}

func closeFile(f *os.File) {
	_ = f.Close()
}

func (p *Params) parseNex() (*parser.NexProgram, error) {
	infile := os.Stdin
	var err error
	if p.InputFilename != "" {
		infile, err = os.Open(p.InputFilename)
		if err != nil {
			return nil, fmt.Errorf("open input: %w", err)
		}
		defer closeFile(infile)
	}

	program, err := parser.ParseNex(infile)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return program, nil
}

func writeWithWriter(filepath string, writer func(io.Writer) error) error {
	if filepath == "" {
		return nil
	}
	f, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("write graph: %w", err)
	}
	defer closeFile(f)
	return writer(f)
}
