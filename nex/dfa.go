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
func BuildDfa(nfa *Nfa) *Dfa {
	b := dfaBuilder{
		nfa: nfa,
		tab: make(map[string]*node),
	}

	b.constructEndNode()

	// The DFA start state is the state representing the nil-closure of the start
	// node in the NFA. Recall it has index 0.
	dfaStart := b.get(b.setToSt([]int{0}))

	// ^.
	newStartEdge(dfaStart, b.getStartWithClosure(dfaStart))

	for len(b.todo) > 0 {
		v := b.todo[len(b.todo)-1]
		b.todo = b.todo[:len(b.todo)-1]
		// Singles.
		for _, r := range nfa.Singles {
			newRuneEdge(v, b.getCb(v, func(e *edge) bool {
				return (e.kind == kRune && e.r == r) || e.kind == kWild || (e.kind == kClass && e.lim.inClass(r))
			}), r)
		}
		// Character ranges.
		for j := 0; j < len(nfa.Lim); j += 2 {
			e := newClassEdge(v, b.getCb(v, func(e *edge) bool {
				return e.kind == kWild || (e.kind == kClass && e.lim.inClass(nfa.Lim[j]))
			}))
			e.lim = []rune{nfa.Lim[j], nfa.Lim[j+1]}
		}
		// Wild.
		newWildEdge(v, b.getKind(v, kWild))
		// $.
		newEndEdge(v, b.getEndWithClosure(v))
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
	nfa  *Nfa
	tab  map[string]*node
	todo []*node
}

type flagSet = []bool

func stToSet(st flagSet) []int {
	var set []int
	for i, v := range st {
		if v {
			set = append(set, i)
		}
	}
	return set
}

func makeStKey(st flagSet) string {
	buf := make([]rune, len(st))
	for i, v := range st {
		if v {
			buf[i] = '1'
		} else {
			buf[i] = '0'
		}
	}
	return string(buf)
}

func (b *dfaBuilder) newEmptySt() flagSet {
	return make(flagSet, len(b.nfa.Nodes))
}

func (b *dfaBuilder) setToSt(set []int) flagSet {
	st := b.newEmptySt()
	for _, i := range set {
		st[i] = true
	}
	return st
}

// closure adds all nil and other steps to the state list
func (b *dfaBuilder) nilClosure(st flagSet) {
	b.closure(st, kNil)
}

// closure adds all nil and other steps to the state list
func (b *dfaBuilder) closure(st flagSet, kind ...int) {
	bfs := stToSet(st)
	visited := b.newEmptySt()
	for len(bfs) > 0 {
		i := bfs[0]
		bfs = bfs[1:]
		if visited[i] {
			continue
		}
		visited[i] = true
		for _, e := range b.nfa.Nodes[i].e {
			if !visited[e.dst.id] && slices.Contains(kind, e.kind) {
				st[e.dst.id] = true
				bfs = append(bfs, e.dst.id)
			}
		}
	}
}

func (b *dfaBuilder) stToAccept(st flagSet) int {
	acc := -1
	for i, v := range st {
		nodeAcc := b.nfa.Nodes[i].accept
		if v && nodeAcc >= 0 && (acc < 0 || nodeAcc < acc) {
			acc = nodeAcc
		}
	}
	return acc
}

func (b *dfaBuilder) newDFANode(st flagSet) (res *node, found bool) {
	key := makeStKey(st)
	res, found = b.tab[key]
	if !found {
		res = b.newNode()
		res.set = stToSet(st)
		res.accept = b.stToAccept(st)
		b.tab[key] = res
	}
	return res, found
}

func (b *dfaBuilder) newStartDFANode(st flagSet, accept int) (res *node, found bool) {
	key := "i" + makeStKey(st)
	res, found = b.tab[key]
	if !found {
		res = b.newNode()
		res.set = stToSet(st)
		res.accept = accept
		b.tab[key] = res
	}
	return res, found
}

func (b *dfaBuilder) get(st flagSet) *node {
	b.nilClosure(st)
	nNode, isOld := b.newDFANode(st)
	if !isOld {
		b.todo = append(b.todo, nNode)
	}
	return nNode
}

// constructEndNode Construct the node of no return.
func (b *dfaBuilder) constructEndNode() {
	b.tab[makeStKey(b.newEmptySt())] = &node{id: -1, accept: -1}
}

func (b *dfaBuilder) getCb(v *node, cb func(*edge) bool) *node {
	st := make(flagSet, len(b.nfa.Nodes))
	for _, i := range v.set {
		for _, e := range b.nfa.Nodes[i].e {
			if !st[e.dst.id] {
				st[e.dst.id] = cb(e)
			}
		}
	}
	return b.get(st)
}

func (b *dfaBuilder) makeKindSt(v *node, kind ...int) flagSet {
	st := make(flagSet, len(b.nfa.Nodes))
	for _, i := range v.set {
		for _, e := range b.nfa.Nodes[i].e {
			if !st[e.dst.id] {
				st[e.dst.id] = slices.Contains(kind, e.kind)
			}
		}
	}
	return st
}

func (b *dfaBuilder) getKind(v *node, kind ...int) *node {
	return b.get(b.makeKindSt(v, kind...))
}

func (b *dfaBuilder) getEndWithClosure(v *node) *node {
	st := b.makeKindSt(v, kEnd)
	b.closure(st, kNil, kEnd)
	return b.get(st)
}

func (b *dfaBuilder) getStartWithClosure(v *node) *node {
	st := b.makeKindSt(v, kStart)
	b.closure(st, kNil, kStart)
	accept := b.stToAccept(st)
	for _, i := range v.set {
		st[i] = true
	}
	nNode, isOld := b.newStartDFANode(st, accept)
	if !isOld {
		b.todo = append(b.todo, nNode)
	}
	return nNode
}
