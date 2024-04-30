// Package nex has substantial copy-and-paste from src/pkg/regexp.
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

var (
	ErrInternal            = errors.New("internal error")
	ErrUnmatchedLpar       = errors.New("unmatched '('")
	ErrUnmatchedRpar       = errors.New("unmatched ')'")
	ErrUnmatchedLbkt       = errors.New("unmatched '['")
	ErrUnmatchedRbkt       = errors.New("unmatched ']'")
	ErrBadRange            = errors.New("bad range in character class")
	ErrExtraneousBackslash = errors.New("extraneous backslash")
	ErrBareClosure         = errors.New("closure applies to nothing")
	ErrBadBackslash        = errors.New("illegal backslash escape")
	ErrExpectedLBrace      = errors.New("expected '{'")
	ErrUnmatchedLBrace     = errors.New("unmatched '{'")
	ErrUnexpectedEOF       = errors.New("unexpected EOF")
	ErrUnexpectedNewline   = errors.New("unexpected newline")
	ErrUnexpectedLAngle    = errors.New("unexpected '<'")
	ErrUnmatchedLAngle     = errors.New("unmatched '<'")
	ErrUnmatchedRAngle     = errors.New("unmatched '>'")

	escapeMap = map[rune]rune{
		'a': '\a',
		'b': '\b',
		'f': '\f',
		'n': '\n',
		'r': '\r',
		't': '\t',
		'v': '\v',
	}
	punctuationMarks = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"

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

func ispunct(c rune) bool {
	return strings.ContainsRune(punctuationMarks, c)
}

func escape(c rune) rune {
	if b, ok := escapeMap[c]; ok {
		return b
	}
	return -1
}

type rule struct {
	regex     []rune
	code      string
	startCode string
	endCode   string
	kid       []*rule
	id        string
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
	nfaGraph := BuildNfa(x)
	b.Result.NfaDot = dumpDotGraph(nfaGraph.Start, "NFA_"+x.id)

	// NFA -> DFA
	dfaGraph := BuildDfa(nfaGraph)
	b.Result.DfaDot = dumpDotGraph(dfaGraph.Start, "DFA_"+x.id)

	// DFA -> Go
	b.writef("\n{ // %v\n acc: []bool{", string(x.regex))
	for _, v := range dfaGraph.Nodes {
		b.writef("%t,", v.accept)
	}
	b.writeString("},\n startf: []int{")
	for _, v := range dfaGraph.Nodes {
		if e := v.getEdgeKind(kStart); len(e) > 0 {
			b.writef("%d,", e[0].dst.n)
		} else {
			b.writeString("-1,")
		}
	}
	b.writeString("},\n endf: []int{")
	for _, v := range dfaGraph.Nodes {
		if e := v.getEdgeKind(kEnd); len(e) > 0 {
			b.writef("%d,", e[0].dst.n)
		} else {
			b.writeString("-1,")
		}
	}
	b.writeString("},\n f: []func(rune) int{\n")
	for _, v := range dfaGraph.Nodes {
		b.writeString("func(r rune) int {\n")
		if runeE := v.getEdgeKind(kRune); len(runeE) > 0 {
			b.writeString("switch(r) {\n")
			for _, e := range runeE {
				b.writef("case %q: return %d\n", e.r, e.dst.n)
			}
			b.writeString("}\n")
		}
		if classE := v.getEdgeKind(kClass); len(classE) > 0 {
			b.writeString("switch {\n")
			for _, e := range classE {
				b.writef("case %q <= r && r <= %q: return %d\n", e.lim[0], e.lim[1], e.dst.n)
			}
			b.writeString("}\n")
		}
		if wildE := v.getEdgeKind(kWild); len(wildE) > 0 {
			b.writef("return %d\n", wildE[len(wildE)-1].dst.n)
		} else {
			b.writeString("return -1\n")
		}
		b.writeString("\n},\n")
	}
	b.writeString("},\n")
	if len(x.kid) > 0 {
		b.writeString("nest: []dfa{")
		for _, kid := range x.kid {
			b.genDFAs(kid)
		}
		b.writeString("}")
	}
	b.writeString("},\n")
}

func (b *Builder) tab(lvl int) {
	for i := 0; i <= lvl; i++ {
		b.writeByte('\t')
	}
}

func (b *Builder) writeFamily(node *rule, lvl int) {
	if node.startCode != "" {
		b.writeStringWithReplace("if !yylex.stale {\n")
		b.writeString("\t" + node.startCode + "\n")
		b.writeString("}\n")
	}
	b.writef("OUTER%s%d:\n", node.id, lvl)
	b.writefWithReplace("for { switch yylex.next(%v) {\n", lvl)
	for i, x := range node.kid {
		b.writef("\tcase %d:\n", i)
		if x.kid != nil {
			b.writeFamily(x, lvl+1)
		} else {
			b.writeString("\t" + x.code + "\n")
		}
	}
	b.writeString("\tdefault:\n")
	b.writef("\t\t break OUTER%s%d\n", node.id, lvl)
	b.writeString("\t}\n")
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

func (b *Builder) read() bool {
	var err error
	b.r, _, err = b.in.ReadRune()
	if err == io.EOF {
		return true
	}
	b.err = err
	if err != nil {
		log.Fatal(err)
	}
	if b.r == '\n' {
		b.lineno++
	}
	return false
}

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
		log.Fatal(ErrExpectedLBrace)
	}
	buf := []rune{b.r}
	nesting := 1
	for {
		if b.read() {
			log.Fatal(ErrUnmatchedLBrace)
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
				log.Fatal(ErrUnexpectedLAngle)
			}
			mustFunc(b.skipWs, ErrUnexpectedEOF)
			node.startCode = b.readCode()
			b.needRootRAngle = true
			continue
		} else if '>' == b.r {
			if node == &b.root {
				if !b.needRootRAngle {
					log.Fatal(ErrUnmatchedRAngle)
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
		x := new(rule)
		x.id = fmt.Sprintf("%d", b.lineno)
		node.kid = append(node.kid, x)
		x.regex = make([]rune, len(regex))
		copy(x.regex, regex)
		if '<' == b.r {
			mustFunc(b.skipWs, ErrUnexpectedEOF)
			x.startCode = b.readCode()
			// TODO: Why is this error ignored?
			b.parse(x)
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
	b.writeString("var dfaStates = []dfa{")
	for _, kid := range b.root.kid {
		b.genDFAs(kid)
	}
	b.writeString("}")

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
