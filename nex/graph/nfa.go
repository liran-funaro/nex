package graph

import (
	"fmt"
	"regexp/syntax"
	"slices"
)

type Expression interface {
	GetRegex() string
	GetId() int
}

// BuildNfa Regex -> NFA (Nondeterministic Finite Automaton)
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
func BuildNfa[E Expression](expressions []E) ([]*Node, error) {
	b := nfaBuilder{}
	rootNode := b.newNode()

	for _, x := range expressions {
		r, err := syntax.Parse(x.GetRegex(), syntax.Perl)
		if err != nil {
			return nil, err
		}
		sNfa, err := b.build(r)
		if err != nil {
			return nil, err
		}
		sNfa.end.Accept = x.GetId()
		newNilEdge(rootNode, sNfa.start)
	}

	// Compute shortlist of nodes (reachable nodes), as we may have discarded
	// nodes left over from parsing. Also, make `nfaRoot` the start node.
	return compactGraph(rootNode), nil
}

type nfaBuilder struct {
	graphBuilder
}

type subNfa struct {
	start, end *Node
}

func (b *nfaBuilder) newSubNfa() subNfa {
	return subNfa{start: b.newNode(), end: b.newNode()}
}

func sortedAlphabet(singles map[rune]any) []rune {
	runes := make([]rune, 0, len(singles))
	for r := range singles {
		runes = append(runes, r)
	}
	slices.Sort(runes)
	return runes
}

func (b *nfaBuilder) build(r *syntax.Regexp) (subNfa, error) {
	switch r.Op {
	case syntax.OpNoMatch: // matches no strings
		return subNfa{}, fmt.Errorf("OpNoMatch is not implemented")
	case syntax.OpEmptyMatch: // matches empty string
		nfa := b.newSubNfa()
		newNilEdge(nfa.start, nfa.end)
		return nfa, nil
	case syntax.OpLiteral: // matches Runes sequence
		start := b.newNode()
		curEnd := start
		for _, curRune := range r.Rune {
			n := b.newNode()
			newRuneEdge(curEnd, n, curRune)
			if r.Flags&syntax.FoldCase != 0 && curRune >= 'A' && curRune <= 'Z' {
				curRune += 'a' - 'A'
				newRuneEdge(curEnd, n, curRune)
			}
			curEnd = n
		}
		return subNfa{start, curEnd}, nil
	case syntax.OpCharClass: // matches Runes interpreted as range pair list
		nfa := b.newSubNfa()
		newClassEdge(nfa.start, nfa.end, r.Rune)
		return nfa, nil
	case syntax.OpAnyCharNotNL: // matches any character except newline
		fallthrough
	case syntax.OpAnyChar: // matches any character
		nfa := b.newSubNfa()
		newWildEdge(nfa.start, nfa.end)
		return nfa, nil
	case syntax.OpBeginLine: // matches empty string at beginning of line
		nfa := b.newSubNfa()
		newAssertEdge(nfa.start, nfa.end, AStartLine)
		return nfa, nil
	case syntax.OpEndLine: // matches empty string at end of line
		nfa := b.newSubNfa()
		newAssertEdge(nfa.start, nfa.end, AEndLine)
		return nfa, nil
	case syntax.OpBeginText: // matches empty string at beginning of text
		nfa := b.newSubNfa()
		newAssertEdge(nfa.start, nfa.end, AStartText)
		return nfa, nil
	case syntax.OpEndText: // matches empty string at end of text
		nfa := b.newSubNfa()
		newAssertEdge(nfa.start, nfa.end, AEndText)
		return nfa, nil
	case syntax.OpWordBoundary: // matches word boundary `\b`
		nfa := b.newSubNfa()
		newAssertEdge(nfa.start, nfa.end, AWordBoundary)
		return nfa, nil
	case syntax.OpNoWordBoundary: // matches word non-boundary `\B`
		nfa := b.newSubNfa()
		newAssertEdge(nfa.start, nfa.end, ANoWordBoundary)
		return nfa, nil
	case syntax.OpCapture: // capturing subexpression with index Cap, optional name Name
		return b.build(r.Sub[0])
	case syntax.OpPlus: // matches Sub[0] one or more times
		nfa, err := b.build(r.Sub[0])
		if err != nil {
			return subNfa{}, err
		}
		newNilEdge(nfa.end, nfa.start)
		return nfa, nil
	case syntax.OpStar: // matches Sub[0] zero or more times
		nfa, err := b.build(r.Sub[0])
		if err != nil {
			return subNfa{}, err
		}
		newNilEdge(nfa.end, nfa.start)
		nfa.start = nfa.end
		return nfa, nil
	case syntax.OpQuest: // matches Sub[0] zero or one times
		nfa, err := b.build(r.Sub[0])
		if err != nil {
			return subNfa{}, err
		}
		newNilEdge(nfa.start, nfa.end)
		return nfa, nil
	case syntax.OpRepeat: // matches Sub[0] at least Min times, at most Max (Max == -1 is no limit)
		nfa := b.newSubNfa()
		lastNfa := &nfa
		prevEnd := nfa.start
		for i := 0; i < r.Min; i++ {
			rNfa, err := b.build(r.Sub[0])
			if err != nil {
				return subNfa{}, err
			}
			newNilEdge(prevEnd, rNfa.start)
			prevEnd = rNfa.end
			lastNfa = &rNfa
		}
		newNilEdge(prevEnd, nfa.end)
		if r.Max < 0 {
			newNilEdge(prevEnd, lastNfa.start)
			return nfa, nil
		}
		for i := 0; i < (r.Max - r.Min); i++ {
			rNfa, err := b.build(r.Sub[0])
			if err != nil {
				return subNfa{}, err
			}
			newNilEdge(prevEnd, rNfa.start)
			newNilEdge(rNfa.end, nfa.end)
			prevEnd = rNfa.end
		}
		return nfa, nil
	case syntax.OpConcat: // matches concatenation of Subs
		start := b.newNode()
		curStart := start
		for _, s := range r.Sub {
			nfa, err := b.build(s)
			if err != nil {
				return subNfa{}, err
			}
			newNilEdge(curStart, nfa.start)
			curStart = nfa.end
		}
		return subNfa{start, curStart}, nil
	case syntax.OpAlternate: // matches alternation of Subs
		nfa := b.newSubNfa()
		for _, s := range r.Sub {
			sNfa, err := b.build(s)
			if err != nil {
				return subNfa{}, err
			}
			newNilEdge(nfa.start, sNfa.start)
			newNilEdge(sNfa.end, nfa.end)
		}
		return nfa, nil
	}
	return subNfa{}, fmt.Errorf("unreconized op: '%d'", r.Op)
}
