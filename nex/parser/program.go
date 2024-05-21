package parser

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/liran-funaro/nex/nex/graph"
)

const rootNodeId = 0

var logger = log.New(os.Stderr, "[nex-parser] ", log.LstdFlags|log.Lshortfile)

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
		lineno:           1,
		expectRootRAngle: false,
	}
	program := &NexProgram{}
	if err := p.parse(program); err != nil {
		return nil, err
	}
	program.Code = p.readRemaining()
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
	lineno           int
	colno            int
	r                rune
	expectRootRAngle bool
	err              error
}

// read returns true if reached EOF
func (b *parser) read() bool {
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
func (b *parser) skipWs() bool {
	for !b.read() {
		if strings.IndexRune(" \n\t\r", b.r) == -1 {
			return false
		}
	}
	return true
}

func (b *parser) readRemaining() string {
	var buf []rune
	for done := b.skipWs(); !done; done = b.read() {
		buf = append(buf, b.r)
	}
	return string(buf)
}

func (b *parser) readCode() string {
	nesting := 0
	var buf []rune
	for {
		if b.r == '\n' && nesting == 0 {
			break
		}
		buf = append(buf, b.r)
		if '{' == b.r {
			nesting++
		} else if '}' == b.r {
			nesting--
			if nesting < 0 {
				logger.Fatal(fmt.Errorf("%d:%d: %w", b.lineno, b.colno, ErrUnmatchedRBrace))
			}
		}

		if b.read() {
			if nesting > 0 {
				logger.Fatal(fmt.Errorf("%d:%d: %w", b.lineno, b.colno, ErrUnmatchedLBrace))
			} else {
				break
			}
		}
	}
	return string(buf)
}

func (b *parser) readRegex() (string, error) {
	delim := b.r
	if b.read() {
		return "", ErrUnexpectedEOF
	}
	var regex []rune
	for b.r != delim || (len(regex) > 0 && regex[len(regex)-1] == '\\') {
		if '\n' == b.r {
			return "", ErrUnexpectedNewline
		}
		regex = append(regex, b.r)
		if b.read() {
			return "", ErrUnexpectedEOF
		}
	}

	return string(regex), nil
}

func (b *parser) parse(node *NexProgram) error {
	for !b.skipWs() {
		if '<' == b.r {
			if node.Id != rootNodeId || len(node.Children) > 0 {
				return ErrUnexpectedLAngle
			}
			if b.skipWs() {
				return ErrUnexpectedEOF
			}
			node.StartCode = b.readCode()
			b.expectRootRAngle = true
			continue
		} else if '>' == b.r {
			if node.Id == rootNodeId && !b.expectRootRAngle {
				return ErrUnmatchedRAngle
			}
			if b.skipWs() {
				return ErrUnexpectedEOF
			}
			node.EndCode = b.readCode()
			return nil
		}

		regex, err := b.readRegex()
		if err != nil {
			return err
		}
		if "" == regex {
			return nil
		}

		child := &NexProgram{Id: b.lineno, Regex: regex}
		node.Children = append(node.Children, child)

		if b.skipWs() {
			return ErrUnexpectedEOF
		}
		if '<' == b.r {
			if b.skipWs() {
				return ErrUnexpectedEOF
			}
			child.StartCode = b.readCode()
			if err := b.parse(child); err != nil {
				return fmt.Errorf("sub-parse: %w", err)
			}
		} else {
			child.Code = b.readCode()
		}
	}

	return ErrUnexpectedEOF
}
