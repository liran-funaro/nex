package main

import (
	"flag"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/liran-funaro/nex/nex"
)

func main() {
	b := &nex.Builder{}
	var nfadotFile, dfadotFile, outFilename string
	var autorun bool
	flag.StringVar(&b.CustomPrefix, "p", "", `name prefix to use in generated code`)
	flag.BoolVar(&b.Standalone, "s", false, `standalone code; NN_FUN macro substitution, no Lex() method`)
	flag.BoolVar(&b.CustomError, "e", false, `custom error func; no Error() method`)
	flag.StringVar(&outFilename, "o", "", `output file`)
	flag.StringVar(&nfadotFile, "nfadot", "", `show NFA graph in DOT format`)
	flag.StringVar(&dfadotFile, "dfadot", "", `show DFA graph in DOT format`)
	flag.BoolVar(&autorun, "r", false, `run generated program`)
	flag.Parse()

	nex.Mustf(flag.NArg() < 2, "extraneous arguments after %s", flag.Arg(0))

	if autorun {
		tmpdir, err := os.MkdirTemp("", "nex")
		nex.NoError(err, "tempdir")
		defer func() {
			_ = os.RemoveAll(tmpdir)
		}()
		if outFilename == "" {
			outFilename = path.Join(tmpdir, "lets.go")
		}
	}

	infile := os.Stdin
	if flag.NArg() > 0 {
		basename := flag.Arg(0)
		basename = strings.TrimSuffix(basename, path.Ext(basename))
		var err error
		infile, err = os.Open(flag.Arg(0))
		nex.NoError(err, "open")
		defer func() {
			_ = infile.Close()
		}()
		if outFilename == "" {
			outFilename = basename + ".nn.go"
		}
	}

	nex.NoError(b.Process(infile), "process")

	if nfadotFile != "" && b.Result.NfaDot != nil {
		nex.NoError(os.WriteFile(nfadotFile, b.Result.NfaDot, 0666), "write")
	}
	if dfadotFile != "" && b.Result.DfaDot != nil {
		nex.NoError(os.WriteFile(dfadotFile, b.Result.DfaDot, 0666), "write")
	}
	if outFilename == "" || b.Result.Lexer == nil {
		return
	}

	nex.NoError(os.WriteFile(outFilename, b.Result.Lexer, 0666), "write")

	if autorun {
		c := exec.Command("go", "run", outFilename)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		nex.NoError(c.Run(), "go run")
	}
}
