package nex

import (
	"log"
	"slices"
)

// Nfa - NFA: Nondeterministic Finite Automaton
type Nfa struct {
	Regexp  []rune
	Nodes   []*node
	Lim     []rune
	Singles []rune
	Start   *node
}

// BuildNfa Regex -> NFA
// We cannot have our alphabet be all Unicode characters. Instead,
// we compute an alphabet for each regex:
//
//  1. Singles: we add single runes used in the regex: any rune not in a
//     range. These are held in `singles`.
//
//  2. Ranges: entire ranges become elements of the alphabet. If ranges in
//     the same expression overlap, we break them up into non-overlapping
//     ranges. The generated code checks singles before ranges, so there's no
//     need to break up a range if it contains a single. These are maintained
//     in sorted order in `lim`.
//
//  3. Wild: we add an element representing all other runes.
//
// e.g. the alphabet of /[0-9]*[Ee][2-5]*/ is singles: { E, e },
// lim: { [0-1], [2-5], [6-9] } and the wild element.
func BuildNfa(x *rule) *Nfa {
	b := nfaBuilder{
		regexp:  x.regex,
		singles: make(map[rune]bool),
	}

	start, end := b.pRe()
	end.accept = true

	// Compute shortlist of nodes (reachable nodes), as we may have discarded
	// nodes left over from parsing. Also, make short[0] the start node.
	b.nodes = compactGraph(start)

	return &Nfa{
		Regexp:  x.regex,
		Nodes:   b.nodes,
		Lim:     b.lim,
		Singles: b.getSortedSingles(),
		Start:   start,
	}
}

type nfaBuilder struct {
	regexp   []rune
	lim      []rune
	singles  map[rune]bool
	pos      int
	n        int
	isNested bool
	nodes    []*node
}

func (b *nfaBuilder) newNode() *node {
	res := &node{n: b.n}
	b.n++
	return res
}

func (b *nfaBuilder) getSortedSingles() []rune {
	runes := make([]rune, 0, len(b.singles))
	for r := range b.singles {
		runes = append(runes, r)
	}
	slices.Sort(runes)
	return runes
}

// insertLimits Insert a new range [l-r] into `lim`, breaking it up if it overlaps, and
// discarding it if it coincides with an existing range. We keep `lim` sorted.
func (b *nfaBuilder) insertLimits(l, r rune) {
	var i int
	for i = 0; i < len(b.lim); i += 2 {
		if l <= b.lim[i+1] {
			break
		}
	}
	if len(b.lim) == i || r < b.lim[i] {
		b.lim = append(b.lim, 0, 0)
		copy(b.lim[i+2:], b.lim[i:])
		b.lim[i] = l
		b.lim[i+1] = r
		return
	}
	if l < b.lim[i] {
		b.lim = append(b.lim, 0, 0)
		copy(b.lim[i+2:], b.lim[i:])
		b.lim[i+1] = b.lim[i] - 1
		b.lim[i] = l
		b.insertLimits(b.lim[i], r)
		return
	}
	if l > b.lim[i] {
		b.lim = append(b.lim, 0, 0)
		copy(b.lim[i+2:], b.lim[i:])
		b.lim[i+1] = l - 1
		b.lim[i+2] = l
		b.insertLimits(l, r)
		return
	}
	// l == lim[i]
	if r == b.lim[i+1] {
		return
	}
	if r < b.lim[i+1] {
		b.lim = append(b.lim, 0, 0)
		copy(b.lim[i+2:], b.lim[i:])
		b.lim[i] = l
		b.lim[i+1] = r
		b.lim[i+2] = r + 1
		return
	}
	b.insertLimits(b.lim[i+1]+1, r)
}

func (b *nfaBuilder) maybeEscape() rune {
	c := b.regexp[b.pos]
	if '\\' == c {
		b.pos++
		if len(b.regexp) == b.pos {
			log.Fatal(ErrExtraneousBackslash)
		}
		c = b.regexp[b.pos]
		switch {
		case ispunct(c):
		case escape(c) >= 0:
			c = escape(b.regexp[b.pos])
		default:
			log.Fatal(ErrBadBackslash)
		}
	}
	return c
}

func (b *nfaBuilder) pCharClass() (start, end *node) {
	start, end = b.newNode(), b.newNode()
	e := newClassEdge(start, end)
	// Ranges consisting of a single element are a special case:
	singletonRange := func(c rune) {
		// 1. The edge-specific 'lim' field always expects endpoints in pairs,
		// so we must give 'c' as the beginning and the end of the range.
		e.lim = append(e.lim, c, c)
		// 2. Instead of updating the regex-wide 'lim' interval set, we add a singleton.
		b.singles[c] = true
	}
	if len(b.regexp) > b.pos && '^' == b.regexp[b.pos] {
		e.negate = true
		b.pos++
	}
	var left rune
	leftLive := false
	justSawDash := false
	first := true
	// Allow '-' at the beginning and end, and in ranges.
	for b.pos < len(b.regexp) && b.regexp[b.pos] != ']' {
		switch c := b.maybeEscape(); c {
		case '-':
			if first {
				singletonRange('-')
				break
			}
			justSawDash = true
		default:
			if justSawDash {
				if !leftLive || left > c {
					log.Fatal(ErrBadRange)
				}
				e.lim = append(e.lim, left, c)
				if left == c {
					b.singles[c] = true
				} else {
					b.insertLimits(left, c)
				}
				leftLive = false
			} else {
				if leftLive {
					singletonRange(left)
				}
				left = c
				leftLive = true
			}
			justSawDash = false
		}
		first = false
		b.pos++
	}
	if leftLive {
		singletonRange(left)
	}
	if justSawDash {
		singletonRange('-')
	}
	return
}

func (b *nfaBuilder) pTerm() (start, end *node) {
	if len(b.regexp) == b.pos || b.regexp[b.pos] == '|' {
		end = b.newNode()
		start = end
		return
	}
	switch b.regexp[b.pos] {
	case '*', '+', '?':
		log.Fatal(ErrBareClosure)
	case ')':
		if !b.isNested {
			log.Fatal(ErrUnmatchedRpar)
		}
		end = b.newNode()
		start = end
		return
	case '(':
		b.pos++
		oldIsNested := b.isNested
		b.isNested = true
		start, end = b.pRe()
		b.isNested = oldIsNested
		if len(b.regexp) == b.pos || ')' != b.regexp[b.pos] {
			log.Fatal(ErrUnmatchedLpar)
		}
	case '.':
		start, end = b.newNode(), b.newNode()
		newWildEdge(start, end)
	case '^':
		start, end = b.newNode(), b.newNode()
		newStartEdge(start, end)
	case '$':
		start, end = b.newNode(), b.newNode()
		newEndEdge(start, end)
	case ']':
		log.Fatal(ErrUnmatchedRbkt)
	case '[':
		b.pos++
		start, end = b.pCharClass()
		if len(b.regexp) == b.pos || ']' != b.regexp[b.pos] {
			log.Fatal(ErrUnmatchedLbkt)
		}
	default:
		start, end = b.newNode(), b.newNode()
		r := b.maybeEscape()
		newRuneEdge(start, end, r)
		b.singles[r] = true
	}
	b.pos++
	return
}

func (b *nfaBuilder) pClosure() (start, end *node) {
	start, end = b.pTerm()
	if start == end {
		return
	}
	if len(b.regexp) == b.pos {
		return
	}
	switch b.regexp[b.pos] {
	case '*':
		newNilEdge(end, start)
		nEnd := b.newNode()
		newNilEdge(end, nEnd)
		start, end = end, nEnd
	case '+':
		newNilEdge(end, start)
		nEnd := b.newNode()
		newNilEdge(end, nEnd)
		end = nEnd
	case '?':
		nStart := b.newNode()
		newNilEdge(nStart, start)
		start = nStart
		newNilEdge(start, end)
	default:
		return
	}
	b.pos++
	return
}

func (b *nfaBuilder) pCat() (start, end *node) {
	for {
		nStart, nEnd := b.pClosure()
		if start == nil {
			start, end = nStart, nEnd
		} else if nStart != nEnd {
			end.e = make([]*edge, len(nStart.e))
			copy(end.e, nStart.e)
			end = nEnd
		}
		if nStart == nEnd {
			return
		}
	}
}

func (b *nfaBuilder) pRe() (start, end *node) {
	start, end = b.pCat()
	for b.pos < len(b.regexp) && b.regexp[b.pos] != ')' {
		if b.regexp[b.pos] != '|' {
			log.Fatal(ErrInternal)
		}
		b.pos++
		nStart, nEnd := b.pCat()
		tmp := b.newNode()
		newNilEdge(tmp, start)
		newNilEdge(tmp, nStart)
		start = tmp
		tmp = b.newNode()
		newNilEdge(end, tmp)
		newNilEdge(nEnd, tmp)
		end = tmp
	}
	return
}
