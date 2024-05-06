package nex

import (
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"log"
	"os"
	"regexp"
	"strings"

	"golang.org/x/tools/imports"
)

const funMacro = "NN_FUN"

var (
	logger               = log.New(os.Stderr, "[nex] ", log.LstdFlags|log.Lshortfile)
	ErrExpectedLBrace    = errors.New("expected '{'")
	ErrUnmatchedLBrace   = errors.New("unmatched '{'")
	ErrUnexpectedEOF     = errors.New("unexpected EOF")
	ErrUnexpectedNewline = errors.New("unexpected newline")
	ErrUnexpectedLAngle  = errors.New("unexpected '<'")
	ErrUnmatchedRAngle   = errors.New("unmatched '>'")

	lexerCode, lexerLexMethodIntro, lexerLexMethodOutro, lexerErrorMethod = lexerText()
)

//go:embed lexer_template.go
var lexerTextFull string

func lexerText() (string, string, string, string) {
	s := regexp.MustCompile(
		`(?s)^.*?
// \[PREAMBLE PLACEHOLDER]
(.*?)
// \[LEX METHOD PLACEHOLDER]
(.*?)
	// \[LEX IMPLEMENTATION PLACEHOLDER]
(.*?)
// \[ERROR METHOD PLACEHOLDER]
(.*?)
// \[SUFFIX PLACEHOLDER]
.*$`,
	).FindStringSubmatch(lexerTextFull)
	return s[1], s[2], s[3], s[4]
}

type rule struct {
	regex     string
	code      string
	startCode string
	endCode   string
	kid       []*rule
	id        int
}

type Builder struct {
	Standalone   bool
	CustomError  bool
	CustomPrefix string
	Result       BuildResult

	in             *bufio.Reader
	out            *bufio.Writer
	replacer       *strings.Replacer
	lineno         int
	colno          int
	r              rune
	root           rule
	needRootRAngle bool
	err            error
}

type BuildResult struct {
	Lexer  []byte
	NfaDot []byte
	DfaDot []byte
}

func (b *Builder) flush() {
	b.err = b.out.Flush()
}

func (b *Builder) write(p []byte) {
	if b.err != nil {
		return
	}
	_, b.err = b.out.Write(p)
}

func (b *Builder) writeString(s string) {
	if b.err != nil {
		return
	}
	_, b.err = b.out.WriteString(s)
}

func (b *Builder) writeStringWithReplace(s string) {
	if b.err != nil {
		return
	}
	if b.replacer != nil {
		_, b.err = b.replacer.WriteString(b.out, s)
	} else {
		b.writeString(s)
	}
}

func (b *Builder) writeByte(c byte) {
	if b.err != nil {
		return
	}
	b.err = b.out.WriteByte(c)
}

func (b *Builder) writef(format string, a ...any) {
	if b.err != nil {
		return
	}
	_, b.err = fmt.Fprintf(b.out, format, a...)
}

func (b *Builder) writefWithReplace(format string, a ...any) {
	if b.err != nil {
		return
	}
	if b.replacer != nil {
		_, b.err = b.replacer.WriteString(b.out, fmt.Sprintf(format, a...))
	} else {
		b.writef(format, a...)
	}
}

func (b *Builder) genDFAs(x *rule) {
	// Regex -> NFA
	nfaGraph, err := BuildNfa(x)
	if err != nil {
		logger.Fatal(err)
	}
	b.Result.NfaDot = dumpDotGraph(nfaGraph.Start, fmt.Sprintf("NFA_%d", x.id))

	// NFA -> DFA
	dfaGraph := BuildDfa(nfaGraph)
	b.Result.DfaDot = dumpDotGraph(dfaGraph.Start, fmt.Sprintf("DFA_%d", x.id))

	// DFA -> Go
	b.writef("dfa{ // %v\n acc: map[int]int{", x.regex)
	for i, v := range dfaGraph.Nodes {
		if v.accept >= 0 {
			b.writef("%d:%d,", i, v.accept)
		}
	}
	b.writeString("},\n")

	m := map[int]int{}
	for i, v := range dfaGraph.Nodes {
		if e := v.getEdgeKind(kStart); len(e) > 0 && e[0].dst.id >= 0 {
			m[i] = e[0].dst.id
		}
	}
	if len(m) > 0 {
		b.writeString("startf: map[int]int{")
		for i, dst := range m {
			b.writef("%d:%d,", i, dst)
		}
		b.writeString("},\n")
	}

	m = map[int]int{}
	for i, v := range dfaGraph.Nodes {
		if e := v.getEdgeKind(kEnd); len(e) > 0 && e[0].dst.id >= 0 {
			m[i] = e[0].dst.id
		}
	}
	if len(m) > 0 {
		b.writeString("endf: map[int]int{")
		for i, dst := range m {
			b.writef("%d:%d,", i, dst)
		}
		b.writeString("},\n")
	}

	b.writeString("f: []func(rune) int{\n")
	for i, v := range dfaGraph.Nodes {
		b.writef("func(r rune) int { // State %d\n", i)
		wildDst := -1
		if wildE := v.getEdgeKind(kWild); len(wildE) > 0 {
			wildDst = wildE[0].dst.id
		}

		if runeE := v.getEdgeKind(kRune); len(runeE) > 0 {
			m := map[int][]string{}
			for _, e := range runeE {
				if e.dst.id == wildDst {
					continue
				}
				m[e.dst.id] = append(m[e.dst.id], fmt.Sprintf("%q", e.r))
			}
			if len(m) > 0 {
				b.writeString("switch(r) {\n")
				for ret, caseValue := range m {
					b.writef("case %s: return %d\n", strings.Join(caseValue, ","), ret)
				}
				b.writeString("}\n")
			}
		}

		if classE := v.getEdgeKind(kClass); len(classE) > 0 {
			m := map[int][]string{}
			for _, e := range classE {
				if e.dst.id == wildDst {
					continue
				}
				m[e.dst.id] = append(m[e.dst.id], fmt.Sprintf("%q <= r && r <= %q", e.lim[0], e.lim[1]))
			}
			if len(m) > 0 {
				b.writeString("switch {\n")
				for ret, caseValue := range m {
					if len(caseValue) > 1 {
						for i, c := range caseValue {
							caseValue[i] = fmt.Sprintf("(%s)", c)
						}
					}
					b.writef("case %s: return %d\n", strings.Join(caseValue, " || "), ret)
				}
				b.writeString("}\n")
			}
		}

		b.writef("return %d\n", wildDst)
		b.writeString("},\n")
	}
	b.writeString("},\n")
	haveNest := false
	for _, kid := range x.kid {
		if len(kid.kid) > 0 {
			if !haveNest {
				haveNest = true
				b.writeString("nest: map[int]dfa{\n")
			}
			b.writef("%d:", kid.id)
			b.genDFAs(kid)
			b.writeString(",\n")
		}
	}
	if haveNest {
		b.writeString("},\n")
	}
	b.writeString("}")
}

func (b *Builder) tab(lvl int) {
	for i := 0; i <= lvl; i++ {
		b.writeByte('\t')
	}
}

func (b *Builder) writeFamily(node *rule, lvl int) {
	if node.startCode != "" {
		b.writeStringWithReplace("if !yylex.stale {\n")
		b.writeString(node.startCode + "\n")
		b.writeString("}\n")
	}
	b.writef("OUTER_%d_%d:\n", node.id, lvl)
	b.writefWithReplace("for { switch yylex.next(%v) {\n", lvl)
	for _, x := range node.kid {
		b.writef("case %d: // %s\n", x.id, x.regex)
		if x.kid != nil {
			b.writeFamily(x, lvl+1)
		} else {
			b.writeString(x.code + "\n")
		}
	}
	b.writeString("default:\n")
	b.writef("break OUTER_%d_%d\n", node.id, lvl)
	b.writeString("}\n")
	b.writeString("}\n")
	b.writef("yylex.pop()\n")
	b.writeString(node.endCode + "\n")
}

func (b *Builder) writeLex(root rule) {
	if !b.CustomError {
		b.writeStringWithReplace(lexerErrorMethod)
	}
	b.writeStringWithReplace(lexerLexMethodIntro)
	b.writeFamily(&root, 0)
	b.writeString(lexerLexMethodOutro)
}

func (b *Builder) writeNNFun(root rule) {
	b.writeStringWithReplace("func(yylex *Lexer) {\n")
	b.writeFamily(&root, 0)
	b.writeString("}")
}

// read returns true if reached EOF
func (b *Builder) read() bool {
	var err error
	b.r, _, err = b.in.ReadRune()
	if err == io.EOF {
		return true
	}
	b.err = err
	if err != nil {
		logger.Fatal(err)
	}
	if b.r == '\n' {
		b.lineno++
		b.colno = 0
	} else {
		b.colno++
	}
	return false
}

// skipWs returns true if reached EOF
func (b *Builder) skipWs() bool {
	for !b.read() {
		if strings.IndexRune(" \n\t\r", b.r) == -1 {
			return false
		}
	}
	return true
}

func (b *Builder) readAll() string {
	var buf []rune
	for done := b.skipWs(); !done; done = b.read() {
		buf = append(buf, b.r)
	}
	return string(buf)
}

func (b *Builder) readCode() string {
	if '{' != b.r {
		logger.Fatalf("[%d:%d]: %s - got: %s", b.lineno, b.colno, ErrExpectedLBrace, b.r)
	}
	buf := []rune{b.r}
	nesting := 1
	for {
		if b.read() {
			logger.Fatal(ErrUnmatchedLBrace)
		}
		buf = append(buf, b.r)
		if '{' == b.r {
			nesting++
		} else if '}' == b.r {
			nesting--
			if 0 == nesting {
				break
			}
		}
	}
	return string(buf)
}

func (b *Builder) parse(node *rule) error {
	for {
		mustFunc(b.skipWs, ErrUnexpectedEOF)
		if '<' == b.r {
			if node != &b.root || len(node.kid) > 0 {
				logger.Fatal(ErrUnexpectedLAngle)
			}
			mustFunc(b.skipWs, ErrUnexpectedEOF)
			node.startCode = b.readCode()
			b.needRootRAngle = true
			continue
		} else if '>' == b.r {
			if node == &b.root {
				if !b.needRootRAngle {
					logger.Fatal(ErrUnmatchedRAngle)
				}
			}
			if b.skipWs() {
				return ErrUnexpectedEOF
			}
			node.endCode = b.readCode()
			return nil
		}
		delim := b.r
		mustFunc(b.read, ErrUnexpectedEOF)
		var regex []rune
		for {
			if b.r == delim && (len(regex) == 0 || regex[len(regex)-1] != '\\') {
				break
			}
			if '\n' == b.r {
				return ErrUnexpectedNewline
			}
			regex = append(regex, b.r)
			mustFunc(b.read, ErrUnexpectedEOF)
		}
		if "" == string(regex) {
			break
		}
		mustFunc(b.skipWs, ErrUnexpectedEOF)
		x := &rule{id: b.lineno, regex: string(regex)}
		node.kid = append(node.kid, x)
		if '<' == b.r {
			mustFunc(b.skipWs, ErrUnexpectedEOF)
			x.startCode = b.readCode()
			noError(b.parse(x), "sub-parse")
		} else {
			x.code = b.readCode()
		}
	}
	return nil
}

func (b *Builder) Process(inputSource io.Reader) error {
	b.in = bufio.NewReader(inputSource)
	var outputBuffer bytes.Buffer
	b.out = bufio.NewWriter(&outputBuffer)
	b.lineno = 1
	b.needRootRAngle = false
	if b.CustomPrefix != "" {
		b.replacer = strings.NewReplacer("yy", b.CustomPrefix)
	}

	if err := b.parse(&b.root); err != nil {
		return err
	}
	userCode := b.readAll()

	fs := token.NewFileSet()
	// Append a blank line to make things easier when there are only package and import declarations.
	t, err := parser.ParseFile(fs, "", userCode+"\n", parser.ImportsOnly)
	if err != nil {
		return err
	}

	b.writeString("// Code generated by ")
	b.writeString(strings.Join(os.Args, " "))
	b.writeString(" --- DO NOT EDIT.\n\n")
	if err = printer.Fprint(b.out, fs, t); err != nil {
		return err
	}
	b.writeStringWithReplace(lexerCode)

	skipLineCount := 0
	fs.Iterate(func(f *token.File) bool {
		skipLineCount = f.LineCount() - 1
		return true
	})
	// Skip over package and import declarations. This is why we appended a blank line above.
	userCode = userCode[findNthLineIndex(userCode, skipLineCount):]

	if !b.Standalone {
		b.writeLex(b.root)
	} else {
		for i := strings.Index(userCode, funMacro); i >= 0; i = strings.Index(userCode, funMacro) {
			b.writeString(userCode[:i])
			b.writeNNFun(b.root)
			userCode = userCode[i+len(funMacro):]
		}
	}

	b.writeString(userCode)

	// Write DFA states at the end of the file for readability.
	b.writeString("var programDfa = ")
	b.genDFAs(&b.root)
	b.writeString("\n")

	b.flush()
	if b.err != nil {
		return b.err
	}

	b.Result.Lexer, err = formatCode(outputBuffer.Bytes())
	if err != nil {
		return err
	}

	return nil
}

func formatCode(src []byte) ([]byte, error) {
	src, err := format.Source(src)
	if err != nil {
		return src, err
	}
	return imports.Process("main.go", src, &imports.Options{
		TabWidth:  8,
		TabIndent: true,
		Comments:  true,
		Fragment:  true,
	})
}
