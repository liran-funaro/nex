package nex

import (
	"slices"
)

// Dfa - DFA: Deterministic Finite Automaton
type Dfa struct {
	Nodes []*node
	Start *node
}

// BuildDfa NFA -> DFA
func BuildDfa(nfaRoot *node) *Dfa {
	b := dfaBuilder{
		// Compute shortlist of nodes (reachable nodes), as we may have discarded
		// nodes left over from parsing. Also, make `nfaRoot` the start node.
		nfa: compactGraph(nfaRoot),
		tab: make(map[stKey]*node),
	}
	b.constructAllNilList()
	b.constructEndNode()

	// The DFA start state is the state representing the nil-closure of the start
	// node in the NFA. Recall it has index 0.
	dfaStart := b.get(b.setToSt([]int{0}, nfAccepting))

	for len(b.todo) > 0 {
		v := b.nextTodo()
		alphabet, l, allAsserts := b.getDfaEdges(v)

		// Asserts.
		for _, a := range allAsserts {
			newAssertEdge(v, b.getAssertWithClosure(v, a), a)
		}

		// Singles.
		for _, r := range alphabet {
			newRuneEdge(v, b.getCb(v, func(e *edge) bool {
				return (e.kind == kRune && e.r == r) || e.kind == kWild || (e.kind == kClass && e.lim.inClass(r))
			}), r)
		}

		// Ranges.
		for j := 0; j < len(l); j += 2 {
			newClassEdge(v, b.getCb(v, func(e *edge) bool {
				return e.kind == kWild || (e.kind == kClass && e.lim.inClass(l[j]))
			}), []rune{l[j], l[j+1]})
		}

		// Wild.
		newWildEdge(v, b.getKind(v, kWild))
	}

	sorted := make([]*node, b.nextId)
	for _, v := range b.tab {
		if -1 != v.id {
			sorted[v.id] = v
		}
	}

	return &Dfa{
		Start: dfaStart,
		Nodes: sorted,
	}
}

type dfaBuilder struct {
	graphBuilder
	nfa         []*node
	allNilNodes []int
	tab         map[stKey]*node
	todo        []*node
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

func (b *dfaBuilder) getDfaEdges(v *node) ([]rune, limits, []asserts) {
	alphabet := make(map[rune]any)
	var a asserts
	var l limits
	for _, i := range v.set {
		for _, e := range b.nfa[i].e {
			switch e.kind {
			case kClass:
				for j := 0; j < len(e.lim); j += 2 {
					if e.lim[j] == e.lim[j+1] {
						alphabet[e.lim[j]] = nil
					} else {
						l = appendLimits(l, e.lim[j], e.lim[j+1])
					}
				}
			case kRune:
				alphabet[e.r] = nil
			case kAssert:
				a |= e.a
			}
		}
	}
	st := b.setToSt(v.set, nfAccepting)
	b.closure(st, func(e *edge) bool {
		return e.kind == kAssert
	})
	for _, i := range stToSet(st) {
		for _, e := range b.nfa[i].e {
			if e.kind == kAssert {
				a |= e.a
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

		nodeAcc := b.nfa[i].accept
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
func (b *dfaBuilder) closure(st flagSet, cb func(*edge) bool) {
	bfs := stToSet(st)
	visited := make([]bool, len(b.nfa))
	for len(bfs) > 0 {
		i := bfs[0]
		bfs = bfs[1:]
		if visited[i] {
			continue
		}
		visited[i] = true
		for _, e := range b.nfa[i].e {
			if visited[e.dst.id] {
				continue
			}
			if e.kind == kNil || (cb != nil && cb(e)) {
				st[e.dst.id] = nfAccepting
				bfs = append(bfs, e.dst.id)
			}
		}
	}
}

func (b *dfaBuilder) get(st flagSet) *node {
	b.nilClosure(st)
	key := b.makeStKey(st)
	nNode, found := b.tab[key]
	if !found {
		nNode = b.newNode()
		nNode.set = stToSet(st)
		nNode.accept = key.accept
		b.tab[key] = nNode
	}
	if !found {
		b.todo = append(b.todo, nNode)
	}
	return nNode
}

func (b *dfaBuilder) nextTodo() *node {
	v := b.todo[len(b.todo)-1]
	b.todo = b.todo[:len(b.todo)-1]
	return v
}

// constructEndNode Construct the node of no return.
func (b *dfaBuilder) constructEndNode() {
	b.tab[b.makeStKey(b.newEmptySt())] = &node{id: -1, accept: -1}
}

func (b *dfaBuilder) constructAllNilList() {
	for i, n := range b.nfa {
		if n.accept > -1 {
			continue
		}
		var haveNonNil bool
		for _, e := range n.e {
			if e.kind != kNil {
				haveNonNil = true
				break
			}
		}
		if !haveNonNil {
			b.allNilNodes = append(b.allNilNodes, i)
		}
	}
}

func (b *dfaBuilder) getCb(v *node, cb func(*edge) bool) *node {
	return b.get(b.makeSt(v, cb))
}

func (b *dfaBuilder) makeSt(v *node, cb func(*edge) bool) flagSet {
	st := b.newEmptySt()
	for _, i := range v.set {
		for _, e := range b.nfa[i].e {
			if st[e.dst.id] != nfAccepting && cb(e) {
				st[e.dst.id] = nfAccepting
			}
		}
	}
	return st
}

func (b *dfaBuilder) makeKindSt(v *node, kind ...int) flagSet {
	return b.makeSt(v, func(e *edge) bool {
		return slices.Contains(kind, e.kind)
	})
}

func (b *dfaBuilder) getKind(v *node, kind ...int) *node {
	return b.get(b.makeKindSt(v, kind...))
}

func (b *dfaBuilder) getAssertWithClosure(v *node, a asserts) *node {
	st := b.setToSt(v.set, nfNotAccepting)
	b.closure(st, func(e *edge) bool {
		return e.kind == kAssert && (e.a&a) != 0
	})
	return b.get(st)
}

func getAssertsSubsets(a asserts) []asserts {
	var options []asserts
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
	perm := make([]asserts, allSubsetsSize-1)
	for i := 1; i < allSubsetsSize; i++ {
		var set asserts
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
