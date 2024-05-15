package nex

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	kNil = iota
	kRune
	kClass
	kAssert
	kWild
)

type edge struct {
	kind int     // Rune/Class/Wild/Nil.
	r    rune    // Rune for rune edges.
	a    asserts // Asserts for assert edges.
	lim  limits  // Pairs of limits for character class edges.
	dst  *node   // Destination node.
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

func (n *node) getEdgeKind(kind int) []*edge {
	var res []*edge
	for _, e := range n.e {
		if e.kind == kind {
			res = append(res, e)
		}
	}
	return res
}

func (n *node) getEdgeAssert(a asserts) []*edge {
	var res []*edge
	for _, e := range n.e {
		if e.kind == kAssert && (e.a&a) != 0 {
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
	e := &edge{dst: v}
	u.e = append(u.e, e)
	return e
}

func newKindEdge(u, v *node, kind int) *edge {
	e := newEdge(u, v)
	e.kind = kind
	return e
}

func newWildEdge(u, v *node) *edge {
	return newKindEdge(u, v, kWild)
}

func newNilEdge(u, v *node) *edge {
	return newKindEdge(u, v, kNil)
}
func newClassEdge(u, v *node, lim []rune) *edge {
	e := newKindEdge(u, v, kClass)
	e.lim = lim
	return e
}

func newRuneEdge(u, v *node, r rune) *edge {
	e := newKindEdge(u, v, kRune)
	e.r = r
	return e
}

func newAssertEdge(u, v *node, a asserts) *edge {
	e := newKindEdge(u, v, kAssert)
	e.a = a
	return e
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

var assertsString = map[asserts]string{
	aStartText:      "aStartText",
	aEndText:        "aEndText",
	aStartLine:      "aStartLine",
	aEndLine:        "aEndLine",
	aWordBoundary:   "aWordBoundary",
	aNoWordBoundary: "aNoWordBoundary",
}

func assertsToString(a asserts) string {
	if a == 0 {
		return "0"
	}

	var asList []string
	for k, v := range assertsString {
		if a&k != 0 {
			asList = append(asList, v)
		}
	}
	return strings.Join(asList, "|")
}
