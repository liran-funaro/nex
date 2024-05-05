package nex

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

	states := make([]bool, len(b.nfa.Nodes))
	// The DFA start state is the state representing the nil-closure of the start
	// node in the NFA. Recall it has index 0.
	states[0] = true
	dfaStart := b.get(states)
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
		newWildEdge(v, b.getCb(v, func(e *edge) bool {
			return e.kind == kWild
		}))
		// ^ and $.
		newStartEdge(v, b.getCb(v, func(e *edge) bool { return e.kind == kStart }))
		newEndEdge(v, b.getCb(v, func(e *edge) bool { return e.kind == kEnd }))
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

func stToSet(st []bool) []int {
	var set []int
	for i, v := range st {
		if v {
			set = append(set, i)
		}
	}
	return set
}

func makeStKey(st []bool) string {
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

// nilClose adds all nil steps to the state list
func (b *dfaBuilder) nilClose(st []bool) {
	bfs := stToSet(st)
	visited := make([]bool, len(b.nfa.Nodes))
	for len(bfs) > 0 {
		i := bfs[0]
		bfs = bfs[1:]
		visited[i] = true
		for _, e := range b.nfa.Nodes[i].e {
			if e.kind == kNil && !visited[e.dst.id] {
				st[e.dst.id] = true
				bfs = append(bfs, e.dst.id)
			}
		}
	}
}

func (b *dfaBuilder) stToAccept(st []bool) bool {
	for i, v := range st {
		if v && b.nfa.Nodes[i].accept {
			return true
		}
	}
	return false
}

func (b *dfaBuilder) newDFANode(st []bool) (res *node, found bool) {
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

func (b *dfaBuilder) get(st []bool) *node {
	b.nilClose(st)
	nNode, isOld := b.newDFANode(st)
	if !isOld {
		b.todo = append(b.todo, nNode)
	}
	return nNode
}

// constructEndNode Construct the node of no return.
func (b *dfaBuilder) constructEndNode() {
	b.tab[makeStKey(make([]bool, len(b.nfa.Nodes)))] = &node{id: -1}
}

func (b *dfaBuilder) getCb(v *node, cb func(*edge) bool) *node {
	st := make([]bool, len(b.nfa.Nodes))
	for _, i := range v.set {
		for _, e := range b.nfa.Nodes[i].e {
			if !st[e.dst.id] {
				st[e.dst.id] = cb(e)
			}
		}
	}
	return b.get(st)
}
