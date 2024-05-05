package nex

// [PREAMBLE PLACEHOLDER]
import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type Lexer struct {
	// The lexer runs in its own goroutine, and communicates via channel 'ch'.
	ch     chan frame
	chStop chan bool
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
	yylex := &Lexer{
		ch:     make(chan frame),
		chStop: make(chan bool, 1),
	}
	if initFun != nil {
		initFun(yylex)
	}
	s := scanner{
		family: dfaStates,
		ch:     yylex.ch,
		chStop: yylex.chStop,
	}
	go s.scan(bufio.NewReader(in))
	return yylex
}

func (yylex *Lexer) Stop() {
	yylex.chStop <- true
}

// Text returns the matched text.
func (yylex *Lexer) Text() string {
	return yylex.stack[len(yylex.stack)-1].s
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
	i            int
	s            string
	line, column int
}

type dfa struct {
	acc          map[int]bool     // Accepting states.
	f            []func(rune) int // Transitions.
	startf, endf map[int]int      // Transitions at start and end of input.
	nest         []dfa
}

type state struct {
	dfaIndex   int
	stateIndex int
}

type scanner struct {
	family                                   []dfa
	line, column                             int
	matchDfaIndex, matchBufferPos, bufferPos int
	ch                                       chan frame
	chStop                                   chan bool
}

func (s *scanner) checkStateAccept(state state) bool {
	return s.checkAccept(state.dfaIndex, state.stateIndex)
}

func (s *scanner) checkAccept(dfaIndex int, st int) bool {
	// Higher precedence match? DFAs are run in parallel, so matchBufferPos is at most len(buf),
	// hence we may omit the length equality check.
	if s.family[dfaIndex].acc[st] && (s.matchBufferPos < s.bufferPos || dfaIndex < s.matchDfaIndex) {
		s.matchDfaIndex, s.matchBufferPos = dfaIndex, s.bufferPos
		return true
	}
	return false
}

func (s *scanner) lcUpdate(r rune) {
	if r == '\n' {
		s.line++
		s.column = 0
	} else {
		s.column++
	}
}

func (s *scanner) scan(in *bufio.Reader) {
	s.matchDfaIndex = 0
	s.matchBufferPos = -1
	s.bufferPos = 0

	var states []state
	for i, f := range s.family {
		mark := make(map[int]bool)
		// Every DFA starts at state 0.
		st := 0
		for {
			states = append(states, state{i, st})
			mark[st] = true
			// As we're at the start of input, follow all ^ transitions and append to our list of start states.
			ok := false
			if f.startf != nil {
				st, ok = f.startf[st]
			}
			if !ok || -1 == st || mark[st] {
				break
			}
			// We only check for a match after at least one transition.
			s.checkAccept(i, st)
		}
	}

	var buf []rune
	atEOF := false
	stopped := false
	for {
		if len(buf) == s.bufferPos && !atEOF {
			r, _, err := in.ReadRune()
			switch err {
			case io.EOF:
				atEOF = true
			case nil:
				buf = append(buf, r)
			default:
				panic(err)
			}
		}
		if !atEOF {
			r := buf[s.bufferPos]
			s.bufferPos++
			var nextStates []state
			for _, x := range states {
				x.stateIndex = s.family[x.dfaIndex].f[x.stateIndex](r)
				if -1 == x.stateIndex {
					continue
				}
				nextStates = append(nextStates, x)
				s.checkStateAccept(x)
			}
			states = nextStates
		} else {
		dollar: // Handle $.
			for _, x := range states {
				mark := make(map[int]bool)
				for {
					mark[x.stateIndex] = true
					ok := false
					endf := s.family[x.dfaIndex].endf
					if endf != nil {
						x.stateIndex, ok = s.family[x.dfaIndex].endf[x.stateIndex]
					}
					if !ok || -1 == x.stateIndex || mark[x.stateIndex] {
						break
					}
					if s.checkStateAccept(x) {
						// Unlike before, we can break off the search.
						// Now that we're at the end, there's no need to maintain the state of each DFA.
						break dollar
					}
				}
			}
			states = nil
		}

		if states == nil {
			// All DFAs stuck. Return last match if it exists, otherwise advance by one rune and restart all DFAs.
			if s.matchBufferPos == -1 {
				if len(buf) == 0 { // This can only happen at the end of input.
					break
				}
				s.lcUpdate(buf[0])
				buf = buf[1:]
			} else {
				text := string(buf[:s.matchBufferPos])
				buf = buf[s.matchBufferPos:]
				s.matchBufferPos = -1
				select {
				case s.ch <- frame{s.matchDfaIndex, text, s.line, s.column}:
				case stopped = <-s.chStop:
				}
				if stopped {
					break
				}
				nested := s.family[s.matchDfaIndex].nest
				if len(nested) > 0 {
					ns := scanner{
						family: nested,
						line:   s.line,
						column: s.column,
						ch:     s.ch,
						chStop: s.chStop,
					}
					ns.scan(bufio.NewReader(strings.NewReader(text)))
				}
				if atEOF {
					break
				}
				for _, r := range text {
					s.lcUpdate(r)
				}
			}
			s.bufferPos = 0
			for i := 0; i < len(s.family); i++ {
				states = append(states, state{i, 0})
			}
		}
	}
	s.ch <- frame{-1, "", s.line, s.column}
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
	return yylex.stack[lvl].i
}

func (yylex *Lexer) pop() {
	yylex.stack = yylex.stack[:len(yylex.stack)-1]
}

// [LEX METHOD PLACEHOLDER]

// Lex runs the lexer. Always returns 0.
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

var dfaStates []dfa
