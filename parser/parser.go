package parser

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/liran-funaro/nex/graph"
)

var (
	ErrUnmatchedRBrace   = errors.New("unmatched '}'")
	ErrUnmatchedLBrace   = errors.New("unmatched '{'")
	ErrUnexpectedEOF     = errors.New("unexpected EOF")
	ErrUnexpectedNewline = errors.New("unexpected newline")
)

func ParseNex(in io.Reader) (*NexProgram, error) {
	p := parser{
		in:   bufio.NewReader(in),
		line: 1,
		char: 0,
	}
	program := p.parseRoot()
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
	in       *bufio.Reader
	line     int
	col      int
	char     int
	r        rune
	err      error
	eof      bool
	isUnread bool
}

func (p *parser) reportError(err error) {
	if err == nil {
		return
	}

	// We only report the first error.
	if p.err != nil {
		return
	}
	p.err = fmt.Errorf("%d:%d: %w", p.line, p.col, err)
}

// read returns true if successful.
func (p *parser) read() bool {
	if p.err != nil || p.eof {
		return false
	}

	if p.isUnread {
		p.isUnread = false
		return true
	}

	var err error
	p.r, _, err = p.in.ReadRune()
	if err != nil {
		if errors.Is(err, io.EOF) {
			p.eof = true
		} else {
			p.reportError(err)
		}
		return false
	}

	p.char++
	if p.r == '\n' {
		p.line++
		p.col = 0
	} else {
		p.col++
	}
	return true
}

func (p *parser) unread() {
	p.isUnread = true
}

func isSpace(r rune) bool {
	return strings.IndexRune(" \n\t\r", r) != -1
}

// readNextNonWs returns true if successful.
func (p *parser) readNextNonWs() bool {
	for p.read() {
		if !isSpace(p.r) {
			return true
		}
	}
	return false
}

// mustRead returns true if successful.
func (p *parser) mustRead() bool {
	ok := p.read()
	if !ok {
		p.reportError(ErrUnexpectedEOF)
	}
	return ok
}

// mustReadNextNonWs returns true if successful.
func (p *parser) mustReadNextNonWs() bool {
	ok := p.readNextNonWs()
	if !ok {
		p.reportError(ErrUnexpectedEOF)
	}
	return ok
}

func (p *parser) readRemaining() string {
	var buf []rune
	for ok := p.readNextNonWs(); ok; ok = p.read() {
		buf = append(buf, p.r)
	}
	return string(append(trimSpaces(buf), '\n'))
}

func trimSpaces(buf []rune) []rune {
	var s, e int
	for s = 0; s < len(buf) && isSpace(buf[s]); s++ {
	}
	if s == len(buf) {
		return nil
	}
	for e = len(buf) - 1; e > s && isSpace(buf[e]); e-- {
	}
	return buf[s : e+1]
}

func (p *parser) readCode() string {
	nesting := 0
	var buf []rune
	for ok := p.mustReadNextNonWs(); ok && (p.r != '\n' || nesting > 0); ok = p.read() {
		buf = append(buf, p.r)
		switch p.r {
		case '{':
			nesting++
		case '}':
			nesting--
			if nesting < 0 {
				p.reportError(ErrUnmatchedRBrace)
				return ""
			}
		}
	}

	if nesting > 0 {
		p.reportError(ErrUnmatchedLBrace)
		return ""
	}
	buf = trimSpaces(buf)
	if len(buf) == 0 {
		return ""
	}
	if buf[0] == '{' && buf[len(buf)-1] == '}' {
		buf = trimSpaces(buf[1 : len(buf)-1])
	}
	if len(buf) == 0 {
		return ""
	}
	return string(append(buf, '\n'))
}

func (p *parser) readRegex(delim rune) *NexProgram {
	var regex []rune
	isEscape := false
	for ok := p.mustRead(); ok && (p.r != delim || isEscape); ok = p.mustRead() {
		if '\n' == p.r {
			p.reportError(ErrUnexpectedNewline)
			return nil
		}
		isEscape = '\\' == p.r
		regex = append(regex, p.r)
	}

	if p.err != nil {
		return nil
	}
	return &NexProgram{Id: p.char, Regex: string(regex)}
}

func (p *parser) isNextSubExp() bool {
	if !p.mustReadNextNonWs() {
		return false
	}
	isSubExp := '<' == p.r
	if !isSubExp {
		p.unread()
	}
	return isSubExp
}

/*
Nex Program Grammar
===================

ROOT:
	(1) EXP-LIST
		EMPTY-REGEXP
		USER-CODE
	(2) SUB-EXP
		USER-CODE

EXP:
	(1) REGEXP CODE
	(2) REGEXP SUB-EXP

EXP-LIST:
	EXP
	EXP
	...

SUB-EXP:
		< CODE
			EXP-LIST
		> CODE

REGEXP: DELIM expression DELIM

CODE:
	(1) one line of code
	(2) { multi line code }

*/

func (p *parser) parseRoot() *NexProgram {
	node := &NexProgram{}
	if p.isNextSubExp() {
		p.parseSubExp(node)
	} else {
		node.Children = p.parseExpList(false)
	}
	node.UserCode = p.readRemaining()
	return node
}

func (p *parser) parseSubExp(node *NexProgram) {
	node.StartCode = p.readCode()
	node.Children = p.parseExpList(true)
	node.EndCode = p.readCode()
}

func (p *parser) parseExpList(isSubExp bool) []*NexProgram {
	var items []*NexProgram
	for p.mustReadNextNonWs() {
		if isSubExp && '>' == p.r {
			break
		}

		child := p.readRegex(p.r)
		if child == nil || (!isSubExp && child.Regex == "") {
			break
		}
		p.parseExp(child)
		items = append(items, child)
	}
	return items
}

func (p *parser) parseExp(child *NexProgram) {
	if p.isNextSubExp() {
		p.parseSubExp(child)
	} else {
		child.StartCode = p.readCode()
	}
}
