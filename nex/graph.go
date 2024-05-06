package nex

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strconv"
)

const (
	kNil = iota
	kRune
	kClass
	kWild
	kStart
	kEnd
)

type edge struct {
	kind int           // Rune/Class/Wild/Nil.
	r    rune          // Rune for rune edges.
	lim  charTypeEdges // Pairs of limits for character class edges.
	dst  *node         // Destination node.
}

func edgeCompare(a, b *edge) int {
	return int(a.r - b.r)
}

type graphBuilder struct {
	nextId int
}

type node struct {
	e      []*edge // Out-edges.
	id     int     // Index number. Scoped to a family.
	accept int     // True if this is an accepting state.
	set    []int   // The NFA nodes represented by a DFA node.
}

type charTypeEdges []rune

func (l charTypeEdges) inClass(r rune) bool {
	for i := 0; i < len(l); i += 2 {
		if l[i] <= r && r <= l[i+1] {
			return true
		}
	}
	return false
}

func (n *node) getEdgeKind(kind int) []*edge {
	var res []*edge
	for _, e := range n.e {
		if e.kind == kind {
			res = append(res, e)
		}
	}
	return res
}

func (g *graphBuilder) newNode() *node {
	n := &node{id: g.nextId, accept: -1}
	g.nextId++
	return n
}

func newEdge(u, v *node) *edge {
	res := &edge{dst: v}
	u.e = append(u.e, res)
	slices.SortFunc(u.e, edgeCompare)
	return res
}

func newKindEdge(u, v *node, kind int) *edge {
	res := newEdge(u, v)
	res.kind = kind
	return res
}

func newStartEdge(u, v *node) *edge {
	return newKindEdge(u, v, kStart)
}

func newEndEdge(u, v *node) *edge {
	return newKindEdge(u, v, kEnd)
}

func newWildEdge(u, v *node) *edge {
	return newKindEdge(u, v, kWild)
}

func newNilEdge(u, v *node) *edge {
	return newKindEdge(u, v, kNil)
}
func newClassEdge(u, v *node) *edge {
	return newKindEdge(u, v, kClass)
}

func newRuneEdge(u, v *node, r rune) *edge {
	res := newKindEdge(u, v, kRune)
	res.r = r
	return res
}

func compactGraph(start *node) []*node {
	visited := map[int]bool{}
	nodes := []*node{start}
	for pos := 0; pos < len(nodes); pos++ {
		n := nodes[pos]
		for _, e := range n.e {
			if !visited[e.dst.id] {
				visited[e.dst.id] = true
				nodes = append(nodes, e.dst)
			}
		}
	}

	for i, v := range nodes {
		v.id = i
	}

	return nodes
}

func dumpDotGraph(start *node, id string) []byte {
	var buf bytes.Buffer
	writeDotGraph(&buf, start, id)
	return buf.Bytes()
}

// writeDotGraph Print a graph in DOT format given the start node.
//
//	$ dot -Tps input.dot -o output.ps
func writeDotGraph(out io.Writer, start *node, id string) {
	b := dotGraphBuilder{
		out:  out,
		done: make(map[*node]bool),
	}
	_, _ = fmt.Fprintf(out, "digraph %v {\n  0[shape=box];\n", id)
	b.show(start)
	_, _ = fmt.Fprintln(out, "}")
}

type dotGraphBuilder struct {
	out  io.Writer
	done map[*node]bool
}

func (b *dotGraphBuilder) show(u *node) {
	if u.accept >= 0 {
		_, _ = fmt.Fprintf(b.out, "  %v[style=filled,color=green];\n", u.id)
	}
	b.done[u] = true
	for _, e := range u.e {
		// We use -1 to denote the dead end node in DFAs.
		if e.dst.id == -1 {
			continue
		}
		label := ""
		runeToDot := func(r rune) string {
			if strconv.IsPrint(r) {
				return fmt.Sprintf("%v", string(r))
			}
			return fmt.Sprintf("U+%X", int(r))
		}
		switch e.kind {
		case kRune:
			label = fmt.Sprintf("[label=%q]", runeToDot(e.r))
		case kWild:
			label = "[color=blue]"
		case kClass:
			label = "[label=\"["
			for i := 0; i < len(e.lim); i += 2 {
				label += runeToDot(e.lim[i])
				if e.lim[i] != e.lim[i+1] {
					label += "-" + runeToDot(e.lim[i+1])
				}
			}
			label += "]\"]"
		}
		_, _ = fmt.Fprintf(b.out, "  %v -> %v%v;\n", u.id, e.dst.id, label)
	}
	for _, e := range u.e {
		if !b.done[e.dst] {
			b.show(e.dst)
		}
	}
}
