package writer

// [PREAMBLE PLACEHOLDER]
import (
	"bufio"
	"context"
	"fmt"
	"io"
)

type Lexer struct {
	// The lexer runs in its own goroutine, and communicates via channel 'ch'.
	ch     chan frame
	cancel context.CancelFunc

	// We record the level of nesting because the action could return, and a
	// subsequent call expects to pick up where it left off. In other words,
	// we're simulating a coroutine.
	// TODO: Support a channel-based variant that compatible with Go's yacc.
	stack []frame
	stale bool

	parseResult any
	parseError  error

	// The following line makes it easy for scripts to insert fields in the
	// generated code.
	// [NEX_END_OF_LEXER_STRUCT]
}

// NewLexer creates a new lexer without init.
func NewLexer(in io.Reader) *Lexer {
	return NewLexerWithInit(in, nil)
}

// NewLexerWithInit creates a new Lexer object, runs the given callback on it,
// then returns it.
func NewLexerWithInit(in io.Reader, initFun func(*Lexer)) *Lexer {
	ctx, cancel := context.WithCancel(context.Background())
	yylex := &Lexer{
		ch:     make(chan frame),
		cancel: cancel,
	}
	if initFun != nil {
		initFun(yylex)
	}
	s := scanner{
		dfa: &programDfa,
		in:  bufio.NewReader(in),
		ch:  yylex.ch,
		ctx: ctx,
	}
	go s.scan()
	return yylex
}

func (yylex *Lexer) Stop() {
	yylex.cancel()
}

// Text returns the matched text.
func (yylex *Lexer) Text() string {
	if len(yylex.stack) == 0 {
		return ""
	}
	return string(yylex.stack[len(yylex.stack)-1].text)
}

// Line returns the current line number.
// The first line is 0.
func (yylex *Lexer) Line() int {
	if len(yylex.stack) == 0 {
		return 0
	}
	return yylex.stack[len(yylex.stack)-1].line
}

// Column returns the current column number.
// The first column is 0.
func (yylex *Lexer) Column() int {
	if len(yylex.stack) == 0 {
		return 0
	}
	return yylex.stack[len(yylex.stack)-1].column
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

type frame struct {
	state        int
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
	ch  chan frame
	ctx context.Context

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
	for _, r := range s.runes[:i] {
		if r == '\n' {
			s.line++
			s.column = 0
		} else {
			s.column++
		}
	}

	// We load the next rune and asserts before shifting the buffers
	// to avoid identifying the next rune as the beginning of the text.
	s.loadNext()

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

func (s *scanner) getNest(st int) (*dfa, bool) {
	if s.dfa.nest == nil {
		return nil, false
	}
	nestedDfa, ok := s.dfa.nest[st]
	return &nestedDfa, ok
}

func (s *scanner) appendFrame(state int, text []rune) {
	select {
	case s.ch <- frame{state, text, s.line, s.column}:
	case <-s.ctx.Done():
	}
}

func (s *scanner) scan() {
	for s.ctx.Err() == nil {
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

			if curState := &s.dfa.states[st]; curState.runeStep != nil {
				if r, ok := s.consumeRune(); ok {
					st = curState.runeStep(r)
					s.checkAccept(st)
					madeProgress = true
				}
			}
		}

		// All DFAs stuck. Return last match if it exists, otherwise advance by one rune and restart.
		if s.matchPos < s.minCapture {
			if len(s.runes) == 0 { // This can only happen at the end of input.
				break
			}
			s.resetBuffer(1)
			continue
		}

		text := s.runes[:s.matchPos]
		s.appendFrame(s.matchAccept, text)
		if nestedDfa, ok := s.getNest(s.matchAccept); ok {
			ns := scanner{
				dfa:    nestedDfa,
				runes:  text,
				line:   s.line,
				column: s.column,
				ch:     s.ch,
				ctx:    s.ctx,
			}
			ns.scan()
		}
		s.resetBuffer(s.matchPos)
	}
	s.appendFrame(-1, nil)
}

func (yylex *Lexer) next(lvl int) int {
	if lvl == len(yylex.stack) {
		l, c := 0, 0
		if lvl > 0 {
			l, c = yylex.stack[lvl-1].line, yylex.stack[lvl-1].column
		}
		yylex.stack = append(yylex.stack, frame{0, nil, l, c})
	}
	if lvl == len(yylex.stack)-1 {
		yylex.stack[lvl] = <-yylex.ch
		yylex.stale = false
	} else {
		yylex.stale = true
	}
	return yylex.stack[lvl].state
}

func (yylex *Lexer) pop() {
	yylex.stack = yylex.stack[:len(yylex.stack)-1]
}

// [LEX METHOD PLACEHOLDER]

// Lex runs the lexer.
// When the -s option is given, this function is not generated;
// instead, the NN_FUN macro runs the lexer.
func (yylex *Lexer) Lex(lval *yySymType) int {
	// [LEX IMPLEMENTATION PLACEHOLDER]
	return 0
}

// [ERROR METHOD PLACEHOLDER]

func (yylex *Lexer) Error(e string) {
	yylex.parseError = fmt.Errorf(fmt.Sprintf("%d:%d %s", yylex.Line(), yylex.Column(), e))
}

// [SUFFIX PLACEHOLDER]

type yySymType any

var programDfa dfa
