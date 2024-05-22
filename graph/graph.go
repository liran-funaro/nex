package graph

import (
	"fmt"
	"io"
	"strconv"
)

const (
	KNil = iota
	KRune
	KClass
	KAssert
	KWild
)

// The following are exact copy of the asserts in lexer.go.
const (
	AStartText Asserts = 1 << iota
	AEndText
	AStartLine
	AEndLine
	AWordBoundary
	ANoWordBoundary
)

type Asserts = uint64

type Edge struct {
	Kind int     // Nil/Rune/Class/Assert/Wild.
	Dst  *Node   // Destination node.
	R    rune    // Rune for rune edges.
	A    Asserts // Asserts for assert edges.
	Lim  limits  // Pairs of limits for character class edges.
}

type Node struct {
	E      []*Edge // Out-edges.
	Id     int     // Index number. Scoped to a family.
	Accept int     // True if this is an accepting state.
	Set    []int   // The NFA nodes represented by a DFA node.
}

type limits []rune

func (l limits) inClass(r rune) bool {
	for i := 0; i < len(l); i += 2 {
		if l[i] <= r && r <= l[i+1] {
			return true
		}
	}
	return false
}

func insertLimits(l limits, i int, r1, r2 rune) limits {
	l = append(l, 0, 0)
	copy(l[i+2:], l[i:])
	l[i] = r1
	l[i+1] = r2
	return l
}

func (n *Node) GetEdgeKind(kind int) []*Edge {
	var res []*Edge
	for _, e := range n.E {
		if e.Kind == kind {
			res = append(res, e)
		}
	}
	return res
}

func (n *Node) getEdgeAssert(a Asserts) []*Edge {
	var res []*Edge
	for _, e := range n.E {
		if e.Kind == KAssert && (e.A&a) != 0 {
			res = append(res, e)
		}
	}
	return res
}

type graphBuilder struct {
	nextId int
}

func (g *graphBuilder) newNode() *Node {
	n := &Node{Id: g.nextId, Accept: -1}
	g.nextId++
	return n
}

func newEdge(u, v *Node) *Edge {
	e := &Edge{Dst: v}
	u.E = append(u.E, e)
	return e
}

func newKindEdge(u, v *Node, kind int) *Edge {
	e := newEdge(u, v)
	e.Kind = kind
	return e
}

func newWildEdge(u, v *Node) *Edge {
	return newKindEdge(u, v, KWild)
}

func newNilEdge(u, v *Node) *Edge {
	return newKindEdge(u, v, KNil)
}
func newClassEdge(u, v *Node, lim []rune) *Edge {
	e := newKindEdge(u, v, KClass)
	e.Lim = lim
	return e
}

func newRuneEdge(u, v *Node, r rune) *Edge {
	e := newKindEdge(u, v, KRune)
	e.R = r
	return e
}

func newAssertEdge(u, v *Node, a Asserts) *Edge {
	e := newKindEdge(u, v, KAssert)
	e.A = a
	return e
}

func compactGraph(start *Node) []*Node {
	visited := map[int]bool{}
	nodes := []*Node{start}
	for pos := 0; pos < len(nodes); pos++ {
		n := nodes[pos]
		for _, e := range n.E {
			if !visited[e.Dst.Id] {
				visited[e.Dst.Id] = true
				nodes = append(nodes, e.Dst)
			}
		}
	}

	for i, v := range nodes {
		v.Id = i
	}

	return nodes
}

// WriteDotGraph Print a graph in DOT format given the start node.
//
//	$ dot -Tps input.dot -o output.ps
func WriteDotGraph(out io.Writer, start *Node, id string) {
	b := dotGraphBuilder{
		out:  out,
		done: make(map[*Node]bool),
	}
	_, _ = fmt.Fprintf(out, "digraph %v {\n  0[shape=box];\n", id)
	b.show(start)
	_, _ = fmt.Fprintln(out, "}")
}

type dotGraphBuilder struct {
	out  io.Writer
	done map[*Node]bool
}

func (b *dotGraphBuilder) show(u *Node) {
	if u.Accept >= 0 {
		_, _ = fmt.Fprintf(b.out, "  %v[style=filled,color=green];\n", u.Id)
	}
	b.done[u] = true
	for _, e := range u.E {
		// We use -1 to denote the dead end node in DFAs.
		if e.Dst.Id == -1 {
			continue
		}
		label := ""
		runeToDot := func(r rune) string {
			if strconv.IsPrint(r) {
				return fmt.Sprintf("%v", string(r))
			}
			return fmt.Sprintf("U+%X", int(r))
		}
		switch e.Kind {
		case KRune:
			label = fmt.Sprintf("[label=%q]", runeToDot(e.R))
		case KWild:
			label = "[color=blue]"
		case KClass:
			label = "[label=\"["
			for i := 0; i < len(e.Lim); i += 2 {
				label += runeToDot(e.Lim[i])
				if e.Lim[i] != e.Lim[i+1] {
					label += "-" + runeToDot(e.Lim[i+1])
				}
			}
			label += "]\"]"
		}
		_, _ = fmt.Fprintf(b.out, "  %v -> %v%v;\n", u.Id, e.Dst.Id, label)
	}
	for _, e := range u.E {
		if !b.done[e.Dst] {
			b.show(e.Dst)
		}
	}
}
