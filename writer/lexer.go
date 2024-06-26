package writer

// [PREAMBLE PLACEHOLDER]
import (
	"bufio"
	"context"
	"fmt"
	"io"
)

type Lexer struct {
	// The lexer runs in a goroutine, and communicates via a channel.
	ch       chan *frame
	ctx      context.Context
	cancel   context.CancelFunc
	curFrame *frame

	parseResult any
	parseError  error

	// [NEX END OF LEXER STRUCT]
}

// NewLexer creates a new lexer without init.
//
//goland:noinspection GoUnusedExportedFunction
func NewLexer(in io.Reader) *Lexer {
	return NewLexerWithInit(in, nil)
}

// NewLexerWithInit creates a new Lexer object, runs the given callback on it,
// then returns it.
func NewLexerWithInit(in io.Reader, initFun func(*Lexer)) *Lexer {
	ctx, cancel := context.WithCancel(context.Background())
	yylex := &Lexer{
		ch:     make(chan *frame),
		ctx:    ctx,
		cancel: cancel,
	}
	if initFun != nil {
		initFun(yylex)
	}
	go yylex.scanRoot(in)
	return yylex
}

// Stop cancels the background scanner.
func (yylex *Lexer) Stop() {
	yylex.cancel()
}

// Text returns the matched text.
func (yylex *Lexer) Text() string {
	if yylex.curFrame == nil {
		return ""
	}
	return string(yylex.curFrame.text)
}

// Line returns the current line number.
// The first line is 0.
func (yylex *Lexer) Line() int {
	if yylex.curFrame == nil {
		return 0
	}
	return yylex.curFrame.line
}

// Column returns the current column number.
// The first column is 0.
func (yylex *Lexer) Column() int {
	if yylex.curFrame == nil {
		return 0
	}
	return yylex.curFrame.column
}

func (yylex *Lexer) appendFrame(kind frameKind, state int, text []rune, line int, column int) {
	select {
	case <-yylex.ctx.Done():
	case yylex.ch <- &frame{frameKey{kind, state}, text, line, column}:
	}
}

func (yylex *Lexer) scanRoot(in io.Reader) {
	defer close(yylex.ch)
	yylex.appendFrame(kStartCode, 0, nil, 0, 0)
	yylex.scan(&scanner{dfa: &programDfa, in: bufio.NewReader(in)})
	yylex.appendFrame(kEndCode, 0, nil, 0, 0)
}

func (yylex *Lexer) scan(s *scanner) {
	if s == nil {
		return
	}

	for yylex.ctx.Err() == nil {
		// The DFA starts at state 0.
		st := 0
		s.matchPos = -1
		s.matchAccept = -1

		madeProgress := true
		for madeProgress && st >= 0 {
			madeProgress = false
			if curState := &s.dfa.states[st]; curState.assertStep != nil {
				if a := s.consumeAsserts(curState.assertMask); a != 0 {
					st = curState.assertStep(a)
					s.checkAccept(st)
					madeProgress = true
				}
			}

			if st < 0 {
				break
			}

			if curState := &s.dfa.states[st]; curState.runeStep != nil {
				if r, ok := s.consumeRune(); ok {
					st = curState.runeStep(r)
					s.checkAccept(st)
					madeProgress = true
				}
			}
		}

		// DFA is stuck. Return last match if it exists, otherwise advance by one rune and restart.
		if s.matchPos < s.minCapture {
			if len(s.runes) == 0 {
				// This can only happen at the end of input.
				return
			}
			s.resetBuffer(1)
		} else {
			text := s.runes[:s.matchPos]
			yylex.appendFrame(kStartCode, s.matchAccept, text, s.line, s.column)
			yylex.scan(s.getNest(s.matchAccept, text))
			yylex.appendFrame(kEndCode, s.matchAccept, text, s.line, s.column)
			s.resetBuffer(s.matchPos)
		}
	}
}

type asserts = uint64

const (
	aStartText asserts = 1 << iota
	aEndText
	aStartLine
	aEndLine
	aWordBoundary
	aNoWordBoundary
)

type frameKind int

const (
	kStartCode frameKind = iota
	kEndCode
)

type frameKey struct {
	kind  frameKind
	state int
}

type frame struct {
	key          frameKey
	text         []rune
	line, column int
}

type state struct {
	accept     int               // Accept index.
	assertMask asserts           // We only apply assert-transition with masked bits.
	assertStep func(asserts) int // Assert transition.
	runeStep   func(rune) int    // Rune transition.
}

type dfa struct {
	states []state
	nest   map[int]dfa
}

type scanner struct {
	dfa *dfa

	// in should be nil when EOF is reached
	in *bufio.Reader

	runes          []rune
	asserts        []asserts
	pos            int
	consumedAssert bool
	minCapture     int

	matchPos, matchAccept int
	line, column          int
}

func (s *scanner) loadNext() {
	s.loadNextRune()
	s.loadNextAsserts()
}

func (s *scanner) loadNextRune() {
	if s.pos < len(s.runes) || s.in == nil {
		return
	}

	r, _, err := s.in.ReadRune()
	switch err {
	case nil:
		s.runes = append(s.runes, r)
	case io.EOF:
		s.in = nil
	default:
		panic(err)
	}
}

func isWord(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// loadNextAsserts must be called after loadNextRune()
func (s *scanner) loadNextAsserts() {
	if s.pos < len(s.asserts) {
		return
	}

	var a asserts
	var r1, r2 rune
	if s.pos == 0 {
		a |= aStartText | aStartLine
	} else {
		r1 = s.runes[s.pos-1]
	}

	if s.pos == len(s.runes) {
		a |= aEndText | aEndLine
	} else {
		r2 = s.runes[s.pos]
	}

	if r1 == '\n' {
		a |= aStartLine
	}
	if r2 == '\n' {
		a |= aEndLine
	}

	if isWord(r1) != isWord(r2) {
		a |= aWordBoundary
	} else {
		a |= aNoWordBoundary
	}

	s.asserts = append(s.asserts, a)
}

func (s *scanner) consumeRune() (rune, bool) {
	s.loadNext()
	if s.pos == len(s.runes) {
		return 0, false
	}

	i := s.pos
	s.pos++
	s.consumedAssert = false
	return s.runes[i], true
}

func (s *scanner) consumeAsserts(mask asserts) asserts {
	s.loadNext()
	if s.consumedAssert || s.pos == len(s.asserts) {
		return 0
	}

	s.consumedAssert = true
	return s.asserts[s.pos] & mask
}

func (s *scanner) checkAccept(st int) {
	if st < 0 {
		return
	}
	accIndex := s.dfa.states[st].accept
	// Higher precedence match
	if accIndex > 0 && (s.matchPos < s.pos || accIndex < s.matchAccept) {
		s.matchAccept, s.matchPos = accIndex, s.pos
	}
}

func (s *scanner) resetBuffer(i int) {
	// We make sure to consume enough runes to discard them.
	// We load one additional rune before shifting the buffers
	// because we need both the previous and next runes to correctly
	// calculate the next assert.
	for ok := true; ok && s.pos <= i; _, ok = s.consumeRune() {
	}

	for _, r := range s.runes[:i] {
		if r == '\n' {
			s.line++
			s.column = 0
		} else {
			s.column++
		}
	}

	s.runes = s.runes[i:]
	s.asserts = s.asserts[i:]
	s.pos = 0
	s.consumedAssert = false
	if i == 0 {
		s.minCapture = 1
	} else {
		s.minCapture = 0
	}
}

func (s *scanner) attemptMapFunc(st int, f map[int]int) int {
	if f == nil {
		return st
	}

	if nextSt, ok := f[st]; ok {
		s.checkAccept(nextSt)
		return nextSt
	}

	return st
}

func (s *scanner) getNest(st int, text []rune) *scanner {
	if s.dfa.nest == nil {
		return nil
	}
	nestedDfa, ok := s.dfa.nest[st]
	if !ok {
		return nil
	}
	return &scanner{
		dfa:    &nestedDfa,
		runes:  text,
		line:   s.line,
		column: s.column,
	}
}

// [LEX METHOD PLACEHOLDER]

// Lex runs the lexer.
//
//goland:noinspection GoUnusedParameter
func (yylex *Lexer) Lex(lval *yySymType) int {
	// [LEX IMPLEMENTATION PLACEHOLDER]
	return 0
}

// [ERROR METHOD PLACEHOLDER]

// Error is used to report an error to the lexer.
func (yylex *Lexer) Error(e string) {
	yylex.parseError = fmt.Errorf("%d:%d %s", yylex.Line(), yylex.Column(), e)
}

// [SUFFIX PLACEHOLDER]

type yySymType any

var programDfa dfa
