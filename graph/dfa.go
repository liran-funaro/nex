package graph

import (
	"slices"
)

// BuildDfa NFA -> DFA
// DFA: Deterministic Finite Automaton
func BuildDfa(nfa []*Node) []*Node {
	b := dfaBuilder{
		nfa: nfa,
		tab: make(map[stKey]*Node),
	}
	b.constructAllNilList()
	b.constructEndNode()

	// The DFA start state is the state representing the nil-closure of the start
	// node in the NFA. Recall it has index 0.
	b.get(b.setToSt([]int{0}, nfAccepting))

	for len(b.todo) > 0 {
		v := b.nextTodo()
		alphabet, l, allAsserts := b.getDfaEdges(v)

		// Asserts.
		for _, a := range allAsserts {
			newAssertEdge(v, b.getAssertWithClosure(v, a), a)
		}

		// Singles.
		for _, r := range alphabet {
			newRuneEdge(v, b.getCb(v, func(e *Edge) bool {
				return (e.Kind == KRune && e.R == r) || e.Kind == KWild || (e.Kind == KClass && e.Lim.inClass(r))
			}), r)
		}

		// Ranges.
		for j := 0; j < len(l); j += 2 {
			newClassEdge(v, b.getCb(v, func(e *Edge) bool {
				return e.Kind == KWild || (e.Kind == KClass && e.Lim.inClass(l[j]))
			}), []rune{l[j], l[j+1]})
		}

		// Wild.
		newWildEdge(v, b.getKind(v, KWild))
	}

	sorted := make([]*Node, b.nextId)
	for _, v := range b.tab {
		if -1 != v.Id {
			sorted[v.Id] = v
		}
	}

	return sorted
}

type dfaBuilder struct {
	graphBuilder
	nfa         []*Node
	allNilNodes []int
	tab         map[stKey]*Node
	todo        []*Node
}

type stKey struct {
	key    string
	accept int
}

type nodeFlag uint32
type flagSet []nodeFlag

const (
	nfNotSet = iota
	nfNotAccepting
	nfAccepting
)

func (b *dfaBuilder) getDfaEdges(v *Node) ([]rune, limits, []Asserts) {
	alphabet := make(map[rune]any)
	var a Asserts
	var l limits
	for _, i := range v.Set {
		for _, e := range b.nfa[i].E {
			switch e.Kind {
			case KClass:
				for j := 0; j < len(e.Lim); j += 2 {
					if e.Lim[j] == e.Lim[j+1] {
						alphabet[e.Lim[j]] = nil
					} else {
						l = appendLimits(l, e.Lim[j], e.Lim[j+1])
					}
				}
			case KRune:
				alphabet[e.R] = nil
			case KAssert:
				a |= e.A
			}
		}
	}
	st := b.setToSt(v.Set, nfAccepting)
	b.closure(st, func(e *Edge) bool {
		return e.Kind == KAssert
	})
	for _, i := range stToSet(st) {
		for _, e := range b.nfa[i].E {
			if e.Kind == KAssert {
				a |= e.A
			}
		}
	}
	return sortedAlphabet(alphabet), l, getAssertsSubsets(a)
}

func stToSet(st flagSet) []int {
	var set []int
	for i, v := range st {
		if v != nfNotSet {
			set = append(set, i)
		}
	}
	return set
}

func (b *dfaBuilder) makeStKey(st flagSet) stKey {
	for _, i := range b.allNilNodes {
		st[i] = nfNotSet
	}
	buf := make([]rune, len(st))
	acc := -1
	for i, v := range st {
		if v == nfNotSet {
			buf[i] = '0'
			continue
		}
		buf[i] = '1'

		nodeAcc := b.nfa[i].Accept
		if v == nfAccepting && nodeAcc >= 0 && (acc < 0 || nodeAcc < acc) {
			acc = nodeAcc
		}
	}

	return stKey{
		key:    string(buf),
		accept: acc,
	}
}

func (b *dfaBuilder) newEmptySt() flagSet {
	return make(flagSet, len(b.nfa))
}

func (b *dfaBuilder) setToSt(set []int, value nodeFlag) flagSet {
	st := b.newEmptySt()
	for _, i := range set {
		st[i] = value
	}
	return st
}

// closure adds all nil and other steps to the state list
func (b *dfaBuilder) nilClosure(st flagSet) {
	b.closure(st, nil)
}

// closure adds all nil and other steps to the state list
func (b *dfaBuilder) closure(st flagSet, cb func(*Edge) bool) {
	bfs := stToSet(st)
	visited := make([]bool, len(b.nfa))
	for len(bfs) > 0 {
		i := bfs[0]
		bfs = bfs[1:]
		if visited[i] {
			continue
		}
		visited[i] = true
		for _, e := range b.nfa[i].E {
			if visited[e.Dst.Id] {
				continue
			}
			if e.Kind == KNil || (cb != nil && cb(e)) {
				st[e.Dst.Id] = nfAccepting
				bfs = append(bfs, e.Dst.Id)
			}
		}
	}
}

func (b *dfaBuilder) get(st flagSet) *Node {
	b.nilClosure(st)
	key := b.makeStKey(st)
	nNode, found := b.tab[key]
	if !found {
		nNode = b.newNode()
		nNode.Set = stToSet(st)
		nNode.Accept = key.accept
		b.tab[key] = nNode
	}
	if !found {
		b.todo = append(b.todo, nNode)
	}
	return nNode
}

func (b *dfaBuilder) nextTodo() *Node {
	v := b.todo[len(b.todo)-1]
	b.todo = b.todo[:len(b.todo)-1]
	return v
}

// constructEndNode Construct the node of no return.
func (b *dfaBuilder) constructEndNode() {
	b.tab[b.makeStKey(b.newEmptySt())] = &Node{Id: -1, Accept: -1}
}

func (b *dfaBuilder) constructAllNilList() {
	for i, n := range b.nfa {
		if n.Accept > -1 {
			continue
		}
		var haveNonNil bool
		for _, e := range n.E {
			if e.Kind != KNil {
				haveNonNil = true
				break
			}
		}
		if !haveNonNil {
			b.allNilNodes = append(b.allNilNodes, i)
		}
	}
}

func (b *dfaBuilder) getCb(v *Node, cb func(*Edge) bool) *Node {
	return b.get(b.makeSt(v, cb))
}

func (b *dfaBuilder) makeSt(v *Node, cb func(*Edge) bool) flagSet {
	st := b.newEmptySt()
	for _, i := range v.Set {
		for _, e := range b.nfa[i].E {
			if st[e.Dst.Id] != nfAccepting && cb(e) {
				st[e.Dst.Id] = nfAccepting
			}
		}
	}
	return st
}

func (b *dfaBuilder) makeKindSt(v *Node, kind ...int) flagSet {
	return b.makeSt(v, func(e *Edge) bool {
		return slices.Contains(kind, e.Kind)
	})
}

func (b *dfaBuilder) getKind(v *Node, kind ...int) *Node {
	return b.get(b.makeKindSt(v, kind...))
}

func (b *dfaBuilder) getAssertWithClosure(v *Node, a Asserts) *Node {
	st := b.setToSt(v.Set, nfNotAccepting)
	b.closure(st, func(e *Edge) bool {
		return e.Kind == KAssert && (e.A&a) != 0
	})
	return b.get(st)
}

func getAssertsSubsets(a Asserts) []Asserts {
	var options []Asserts
	for i := 0; a != 0; i++ {
		if a&1 != 0 {
			options = append(options, 1<<i)
		}
		a >>= 1
	}

	opSize := len(options)
	if opSize == 0 {
		return nil
	}
	if opSize == 1 {
		return options
	}
	allSubsetsSize := 1 << opSize
	perm := make([]Asserts, allSubsetsSize-1)
	for i := 1; i < allSubsetsSize; i++ {
		var set Asserts
		for j, v := range options {
			if i&(1<<j) != 0 {
				set |= v
			}
		}
		perm[i-1] = set
	}
	return perm
}

// appendLimits Insert a new range [l-r] into `lim`, breaking it up if it overlaps, and
// discarding it if it coincides with an existing range. We keep `lim` sorted.
func appendLimits(l limits, r1, r2 rune) limits {
	var i int
	for i = 0; i < len(l); i += 2 {
		if r1 <= l[i+1] {
			break
		}
	}
	if len(l) == i || r2 < l[i] {
		return insertLimits(l, i, r1, r2)
	}
	if r1 < l[i] {
		l = insertLimits(l, i, l[i]-1, r1)
		return appendLimits(l, l[i], r2)
	}
	if r1 > l[i] {
		l = insertLimits(l, i, l[i], r1-1)
		l[i+2] = r1
		return appendLimits(l, r1, r2)
	}
	// r1 == lim[i]
	if r2 == l[i+1] {
		return l
	}
	if r2 < l[i+1] {
		l = insertLimits(l, i, r1, r2)
		l[i+2] = r2 + 1
		return l
	}
	return appendLimits(l, l[i+1]+1, r2)
}
