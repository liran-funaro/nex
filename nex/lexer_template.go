package nex

// [PREAMBLE PLACEHOLDER]
import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
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
	return yylex.stack[len(yylex.stack)-1].text
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

type frame struct {
	state        int
	text         string
	line, column int
}

type dfa struct {
	acc          map[int]int      // Accepting states.
	f            []func(rune) int // Transitions.
	startf, endf map[int]int      // Transitions at start and end of input.
	nest         map[int]dfa
}

type scanner struct {
	dfa                   *dfa
	ch                    chan frame
	ctx                   context.Context
	in                    *bufio.Reader
	buf                   []rune
	eof                   bool
	pos                   int
	matchPos, matchAccept int
	line, column          int
}

func (s *scanner) next() (rune, bool) {
	if len(s.buf) > s.pos {
		r := s.buf[s.pos]
		s.pos++
		return r, false
	}
	if s.eof {
		return 0, true
	}
	r, _, err := s.in.ReadRune()
	switch err {
	case nil:
		s.buf = append(s.buf, r)
		s.pos++
		return r, false
	case io.EOF:
		s.eof = true
		return 0, true
	default:
		panic(err)
	}
}

func (s *scanner) isEOF() bool {
	return s.eof && len(s.buf) == s.pos
}
func (s *scanner) isStopped() bool {
	return s.ctx.Err() != nil || s.isEOF()
}

func (s *scanner) checkAccept(st int) {
	if st < 0 {
		return
	}
	accIndex, ok := s.dfa.acc[st]
	// Higher precedence match
	if ok && (s.matchPos < s.pos || accIndex < s.matchAccept) {
		s.matchAccept, s.matchPos = accIndex, s.pos
	}
}

func (s *scanner) resetBuffer(i int) {
	for _, r := range s.buf[:i] {
		if r == '\n' {
			s.line++
			s.column = 0
		} else {
			s.column++
		}
	}

	s.buf = s.buf[i:]
	s.pos = 0
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

func (s *scanner) nextState(st int, r rune) int {
	nextSt := s.dfa.f[st](r)
	s.checkAccept(nextSt)
	return nextSt
}

func (s *scanner) getNest(st int) (*dfa, bool) {
	if s.dfa.nest == nil {
		return nil, false
	}
	nestedDfa, ok := s.dfa.nest[st]
	return &nestedDfa, ok
}

func (s *scanner) appendFrame(state int, text string) {
	select {
	case s.ch <- frame{state, text, s.line, s.column}:
	case <-s.ctx.Done():
	}
}

func (s *scanner) scan() {
	isStart := true

	// The DFA starts at state 0.
	for !s.isStopped() {
		st := 0
		s.matchPos = -1
		s.matchAccept = -1
		if isStart {
			// As we're at the start of input, follow the ^ transition.
			st = s.attemptMapFunc(st, s.dfa.startf)
			isStart = false
		}
		for st >= 0 {
			if r, eof := s.next(); !eof {
				st = s.nextState(st, r)
			} else { // Handle $.
				s.attemptMapFunc(st, s.dfa.endf)
				break
			}
		}

		// All DFAs stuck. Return last match if it exists, otherwise advance by one rune and restart.
		if s.matchPos == -1 {
			if len(s.buf) == 0 { // This can only happen at the end of input.
				break
			}
			s.resetBuffer(1)
			continue
		}

		text := string(s.buf[:s.matchPos])
		s.appendFrame(s.matchAccept, text)
		if nestedDfa, ok := s.getNest(s.matchAccept); ok {
			ns := scanner{
				dfa:    nestedDfa,
				in:     bufio.NewReader(strings.NewReader(text)),
				line:   s.line,
				column: s.column,
				ch:     s.ch,
				ctx:    s.ctx,
			}
			ns.scan()
		}
		s.resetBuffer(s.matchPos)
	}
	s.appendFrame(-1, "")
}

func (yylex *Lexer) next(lvl int) int {
	if lvl == len(yylex.stack) {
		l, c := 0, 0
		if lvl > 0 {
			l, c = yylex.stack[lvl-1].line, yylex.stack[lvl-1].column
		}
		yylex.stack = append(yylex.stack, frame{0, "", l, c})
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
