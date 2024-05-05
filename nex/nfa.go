package nex

import (
	"fmt"
	"regexp/syntax"
	"slices"
)

// Nfa - NFA: Nondeterministic Finite Automaton
type Nfa struct {
	Start   *node
	Nodes   []*node
	Lim     []rune
	Singles []rune
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
func BuildNfa(x *rule) (*Nfa, error) {
	r, err := syntax.Parse(x.regex, syntax.Perl)
	if err != nil {
		return nil, err
	}

	b := nfaBuilder{singles: make(map[rune]bool)}
	nfa := b.build(r)
	nfa.end.accept = true

	return &Nfa{
		Start: nfa.start,
		// Compute shortlist of nodes (reachable nodes), as we may have discarded
		// nodes left over from parsing. Also, make short[0] the start node.
		Nodes:   compactGraph(nfa.start),
		Lim:     b.lim,
		Singles: b.getSortedSingles(),
	}, nil
}

type nfaBuilder struct {
	graphBuilder
	lim     []rune
	singles map[rune]bool
}

type subNfa struct {
	start, end *node
}

func (b *nfaBuilder) newSubNfa() subNfa {
	return subNfa{start: b.newNode(), end: b.newNode()}
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

func (b *nfaBuilder) build(r *syntax.Regexp) subNfa {
	switch r.Op {
	case syntax.OpNoMatch: // matches no strings
		panic("OpNoMatch is not implemented")
	case syntax.OpEmptyMatch: // matches empty string
		nfa := b.newSubNfa()
		newNilEdge(nfa.start, nfa.end)
		return nfa
	case syntax.OpLiteral: // matches Runes sequence
		start := b.newNode()
		curStart := start
		for _, curRune := range r.Rune {
			n := b.newNode()
			newRuneEdge(curStart, n, curRune)
			b.singles[curRune] = true
			if r.Flags&syntax.FoldCase != 0 && curRune >= 'A' && curRune <= 'Z' {
				curRune += 'a' - 'A'
				newRuneEdge(curStart, n, curRune)
				b.singles[curRune] = true
			}
			curStart = n
		}
		return subNfa{start, curStart}
	case syntax.OpCharClass: // matches Runes interpreted as range pair list
		nfa := b.newSubNfa()
		e := newClassEdge(nfa.start, nfa.end)
		e.lim = r.Rune
		for i := 0; i < len(r.Rune); i += 2 {
			if r.Rune[i] == r.Rune[i+1] {
				b.singles[r.Rune[i]] = true
			} else {
				b.insertLimits(r.Rune[i], r.Rune[i+1])
			}
		}
		return nfa
	case syntax.OpAnyCharNotNL: // matches any character except newline
		fallthrough
	case syntax.OpAnyChar: // matches any character
		nfa := b.newSubNfa()
		newWildEdge(nfa.start, nfa.end)
		return nfa
	case syntax.OpBeginLine: // matches empty string at beginning of line
		nfa := b.newSubNfa()
		newStartEdge(nfa.start, nfa.end)
		return nfa
	case syntax.OpEndLine: // matches empty string at end of line
		nfa := b.newSubNfa()
		newEndEdge(nfa.start, nfa.end)
		return nfa
	case syntax.OpBeginText: // matches empty string at beginning of text
		nfa := b.newSubNfa()
		newStartEdge(nfa.start, nfa.end)
		return nfa
	case syntax.OpEndText: // matches empty string at end of text
		nfa := b.newSubNfa()
		newEndEdge(nfa.start, nfa.end)
		return nfa
	case syntax.OpWordBoundary: // matches word boundary `\b`
		panic("OpWordBoundary is not implemented")
	case syntax.OpNoWordBoundary: // matches word non-boundary `\B`
		panic("OpNoWordBoundary is not implemented")
	case syntax.OpCapture: // capturing subexpression with index Cap, optional name Name
		return b.build(r.Sub[0])
	case syntax.OpPlus: // matches Sub[0] one or more times
		nfa := b.build(r.Sub[0])
		newNilEdge(nfa.end, nfa.start)
		nEnd := b.newNode()
		newNilEdge(nfa.end, nEnd)
		nfa.end = nEnd
		return nfa
	case syntax.OpStar: // matches Sub[0] zero or more times
		nfa := b.build(r.Sub[0])
		newNilEdge(nfa.end, nfa.start)
		nEnd := b.newNode()
		newNilEdge(nfa.end, nEnd)
		nfa.start, nfa.end = nfa.end, nEnd
		return nfa
	case syntax.OpQuest: // matches Sub[0] zero or one times
		nfa := b.build(r.Sub[0])
		nStart := b.newNode()
		newNilEdge(nStart, nfa.start)
		nfa.start = nStart
		newNilEdge(nfa.start, nfa.end)
		return nfa
	case syntax.OpRepeat: // matches Sub[0] at least Min times, at most Max (Max == -1 is no limit)
		nfa := b.newSubNfa()
		lastNfa := &nfa
		prevEnd := nfa.start
		for i := 0; i < r.Min; i++ {
			rNfa := b.build(r.Sub[0])
			newNilEdge(prevEnd, rNfa.start)
			prevEnd = rNfa.end
			lastNfa = &rNfa
		}
		newNilEdge(prevEnd, nfa.end)
		if r.Max < 0 {
			newNilEdge(prevEnd, lastNfa.start)
			return nfa
		}
		for i := 0; i < (r.Max - r.Min); i++ {
			rNfa := b.build(r.Sub[0])
			newNilEdge(prevEnd, rNfa.start)
			newNilEdge(rNfa.end, nfa.end)
			prevEnd = rNfa.end
		}
		return nfa
	case syntax.OpConcat: // matches concatenation of Subs
		start := b.newNode()
		curStart := start
		for _, s := range r.Sub {
			nfa := b.build(s)
			newNilEdge(curStart, nfa.start)
			curStart = nfa.end
		}
		return subNfa{start, curStart}
	case syntax.OpAlternate: // matches alternation of Subs
		nfa := b.newSubNfa()
		for _, s := range r.Sub {
			sNfa := b.build(s)
			newNilEdge(nfa.start, sNfa.start)
			newNilEdge(sNfa.end, nfa.end)
		}
		return nfa
	}
	panic(fmt.Sprintf("Unreconized op: '%d'", r.Op))
}
