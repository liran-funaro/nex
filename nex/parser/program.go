package parser

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/liran-funaro/nex/nex/graph"
)

const rootNodeId = 0

var (
	ErrUnmatchedRBrace   = errors.New("unmatched '}'")
	ErrUnmatchedLBrace   = errors.New("unmatched '{'")
	ErrUnexpectedEOF     = errors.New("unexpected EOF")
	ErrUnexpectedNewline = errors.New("unexpected newline")
	ErrUnexpectedLAngle  = errors.New("unexpected '<'")
	ErrUnmatchedRAngle   = errors.New("unmatched '>'")
)

type NexProgram struct {
	Id        int
	Regex     string
	Code      string
	StartCode string
	EndCode   string
	Children  []*NexProgram
	NFA       []*graph.Node
	DFA       []*graph.Node
}

func (r *NexProgram) GetRegex() string {
	return r.Regex
}

func (r *NexProgram) GetId() int {
	return r.Id
}

func (r *NexProgram) WriteNFADotGraph(writer io.Writer) error {
	graph.WriteDotGraph(writer, r.NFA[0], fmt.Sprintf("NFA_%d", r.Id))
	for _, c := range r.Children {
		if err := c.WriteNFADotGraph(writer); err != nil {
			return err
		}
	}
	return nil
}

func (r *NexProgram) WriteDFADotGraph(writer io.Writer) error {
	graph.WriteDotGraph(writer, r.DFA[0], fmt.Sprintf("DFA_%d", r.Id))
	for _, c := range r.Children {
		if err := c.WriteDFADotGraph(writer); err != nil {
			return err
		}
	}
	return nil
}

func ParseNex(in io.Reader) (*NexProgram, error) {
	p := parser{
		in:               bufio.NewReader(in),
		line:             1,
		char:             1,
		expectRootRAngle: false,
	}
	program := &NexProgram{}
	p.parse(program)
	program.Code = p.readRemaining()
	if p.err != nil {
		return nil, p.err
	}
	return program, genGraphs(program)
}

func genGraphs(x *NexProgram) error {
	if len(x.Children) == 0 {
		return nil
	}

	// Regex -> NFA
	var err error
	x.NFA, err = graph.BuildNfa(x.Children)
	if err != nil {
		return err
	}

	// NFA -> DFA
	x.DFA = graph.BuildDfa(x.NFA)

	for _, kid := range x.Children {
		if err = genGraphs(kid); err != nil {
			return err
		}
	}
	return nil
}

type parser struct {
	in               *bufio.Reader
	line             int
	col              int
	char             int
	r                rune
	expectRootRAngle bool
	err              error
	eof              bool
}

// read returns true if successful.
func (b *parser) read() bool {
	if b.err != nil || b.eof {
		return false
	}

	var err error
	b.r, _, err = b.in.ReadRune()
	if err != nil {
		if errors.Is(err, io.EOF) {
			b.eof = true
		} else {
			b.err = err
		}
		return false
	}

	b.char++
	if b.r == '\n' {
		b.line++
		b.col = 0
	} else {
		b.col++
	}
	return true
}

// readNextNonWs returns true if successful.
func (b *parser) readNextNonWs() bool {
	for b.read() {
		if strings.IndexRune(" \n\t\r", b.r) == -1 {
			return true
		}
	}
	return false
}

func (b *parser) readRemaining() string {
	var buf []rune
	for ok := b.readNextNonWs(); ok; ok = b.read() {
		buf = append(buf, b.r)
	}
	return string(buf)
}

func (b *parser) reportError(err error) {
	// We only report the first error.
	if b.err != nil {
		return
	}
	b.err = fmt.Errorf("%d:%d: %w", b.line, b.col, err)
}

func (b *parser) readCode() string {
	nesting := 0
	var buf []rune
	for ok := true; ok && (b.r != '\n' || nesting > 0); ok = b.read() {
		buf = append(buf, b.r)
		switch b.r {
		case '{':
			nesting++
		case '}':
			nesting--
			if nesting < 0 {
				b.reportError(ErrUnmatchedRBrace)
				return ""
			}
		}
	}

	if nesting > 0 {
		b.reportError(ErrUnmatchedLBrace)
	}
	return string(buf)
}

func (b *parser) readRegex() string {
	delim := b.r
	var regex []rune
	isEscape := false
	for ok := b.read(); ok && '\n' != b.r && (b.r != delim || isEscape); ok = b.read() {
		isEscape = '\\' == b.r
		regex = append(regex, b.r)
	}

	if b.err != nil {
		return ""
	} else if b.eof {
		b.reportError(ErrUnexpectedEOF)
		return ""
	} else if '\n' == b.r {
		b.reportError(ErrUnexpectedNewline)
		return ""
	}
	return string(regex)
}

func (b *parser) parse(node *NexProgram) {
	for b.readNextNonWs() {
		if '<' == b.r {
			if node.Id != rootNodeId || len(node.Children) > 0 {
				b.reportError(ErrUnexpectedLAngle)
				return
			}
			if !b.readNextNonWs() {
				b.reportError(ErrUnexpectedEOF)
				return
			}
			node.StartCode = b.readCode()
			b.expectRootRAngle = true
			continue
		} else if '>' == b.r {
			if node.Id == rootNodeId && !b.expectRootRAngle {
				b.reportError(ErrUnmatchedRAngle)
				return
			}
			if !b.readNextNonWs() {
				b.reportError(ErrUnexpectedEOF)
				return
			}
			node.EndCode = b.readCode()
			return
		}

		regex := b.readRegex()
		if "" == regex {
			return
		}

		child := &NexProgram{Id: b.line, Regex: regex}
		node.Children = append(node.Children, child)

		if !b.readNextNonWs() {
			b.reportError(ErrUnexpectedEOF)
			return
		}
		if '<' == b.r {
			if !b.readNextNonWs() {
				b.reportError(ErrUnexpectedEOF)
				return
			}
			child.StartCode = b.readCode()
			b.parse(child)
		} else {
			child.Code = b.readCode()
		}
	}

	b.reportError(ErrUnexpectedEOF)
}
