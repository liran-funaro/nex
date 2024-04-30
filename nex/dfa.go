package nex

// Dfa - DFA: Deterministic Finite Automaton
type Dfa struct {
	Nfa   *Nfa
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

	states := make([]bool, len(b.nfa.Nodes))
	// The DFA start state is the state representing the nil-closure of the start
	// node in the NFA. Recall it has index 0.
	states[0] = true
	dfaStart := b.get(states)
	for len(b.todo) > 0 {
		v := b.todo[len(b.todo)-1]
		b.todo = b.todo[0 : len(b.todo)-1]
		// Singles.
		for _, r := range nfa.Singles {
			newRuneEdge(v, b.getCb(v, func(e *edge) bool {
				return e.kind == kRune && e.r == r ||
					e.kind == kWild ||
					e.kind == kClass && e.negate != e.lim.inClass(r)
			}), r)
		}
		// Character ranges.
		for j := 0; j < len(nfa.Lim); j += 2 {
			e := newClassEdge(v, b.getCb(v, func(e *edge) bool {
				return e.kind == kWild ||
					e.kind == kClass && e.negate != e.lim.inClass(nfa.Lim[j])
			}))

			e.lim = append(e.lim, nfa.Lim[j], nfa.Lim[j+1])
		}
		// Wild.
		newWildEdge(v, b.getCb(v, func(e *edge) bool {
			return e.kind == kWild || (e.kind == kClass && e.negate)
		}))
		// ^ and $.
		newStartEdge(v, b.getCb(v, func(e *edge) bool { return e.kind == kStart }))
		newEndEdge(v, b.getCb(v, func(e *edge) bool { return e.kind == kEnd }))
	}

	sorted := make([]*node, b.n)
	for _, v := range b.tab {
		if -1 != v.n {
			sorted[v.n] = v
		}
	}

	return &Dfa{
		Nfa:   nfa,
		Nodes: sorted,
		Start: dfaStart,
	}
}

type dfaBuilder struct {
	nfa  *Nfa
	n    int
	tab  map[string]*node
	todo []*node
}

func (b *dfaBuilder) nilClose(st []bool) {
	visited := make([]bool, len(b.nfa.Nodes))
	var do func(int)
	do = func(i int) {
		visited[i] = true
		v := b.nfa.Nodes[i]
		for _, e := range v.e {
			if e.kind == kNil && !visited[e.dst.n] {
				st[e.dst.n] = true
				do(e.dst.n)
			}
		}
	}
	for i := 0; i < len(b.nfa.Nodes); i++ {
		if st[i] && !visited[i] {
			do(i)
		}
	}
}

func (b *dfaBuilder) newDFANode(st []bool) (res *node, found bool) {
	var buf []byte
	accept := false
	for i, v := range st {
		if v {
			buf = append(buf, '1')
			accept = accept || b.nfa.Nodes[i].accept
		} else {
			buf = append(buf, '0')
		}
	}
	res, found = b.tab[string(buf)]
	if !found {
		res = new(node)
		res.n = b.n
		res.accept = accept
		b.n++
		for i, v := range st {
			if v {
				res.set = append(res.set, i)
			}
		}
		b.tab[string(buf)] = res
	}
	return res, found
}

func (b *dfaBuilder) get(states []bool) *node {
	b.nilClose(states)
	newNode, isOld := b.newDFANode(states)
	if !isOld {
		b.todo = append(b.todo, newNode)
	}
	return newNode
}

// constructEndNode Construct the node of no return.
func (b *dfaBuilder) constructEndNode() {
	var buf []byte
	for i := 0; i < len(b.nfa.Nodes); i++ {
		buf = append(buf, '0')
	}
	tmp := new(node)
	tmp.n = -1
	b.tab[string(buf)] = tmp
}

func (b *dfaBuilder) getCb(v *node, cb func(*edge) bool) *node {
	states := make([]bool, len(b.nfa.Nodes))
	for _, i := range v.set {
		for _, e := range b.nfa.Nodes[i].e {
			if cb(e) {
				states[e.dst.n] = true
			}
		}
	}
	return b.get(states)
}
