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

	funMacro = "NN_FUN"
)

type edge struct {
	kind   int           // Rune/Class/Wild/Nil.
	r      rune          // Rune for rune edges.
	lim    charTypeEdges // Pairs of limits for character class edges.
	negate bool          // True if the character class is negated.
	dst    *node         // Destination node.
}

func edgeCompare(a, b *edge) int {
	return int(a.r - b.r)
}

type node struct {
	e      []*edge // Out-edges.
	n      int     // Index number. Scoped to a family.
	accept bool    // True if this is an accepting state.
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
	res := newKindEdge(u, v, kClass)
	res.lim = make([]rune, 0, 2)
	return res
}

func newRuneEdge(u, v *node, r rune) *edge {
	res := newKindEdge(u, v, kRune)
	res.r = r
	return res
}

func compactGraphInner(n *node, mark map[int]bool) []*node {
	var nodes []*node

	mark[n.n] = true
	nodes = append(nodes, n)
	for _, e := range n.e {
		if !mark[e.dst.n] {
			nodes = append(nodes, compactGraphInner(e.dst, mark)...)
		}
	}

	return nodes
}

func compactGraph(n *node) []*node {
	nodes := compactGraphInner(n, make(map[int]bool))
	for i, v := range nodes {
		v.n = i
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
	if u.accept {
		_, _ = fmt.Fprintf(b.out, "  %v[style=filled,color=green];\n", u.n)
	}
	b.done[u] = true
	for _, e := range u.e {
		// We use -1 to denote the dead end node in DFAs.
		if e.dst.n == -1 {
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
			if e.negate {
				label += "^"
			}
			for i := 0; i < len(e.lim); i += 2 {
				label += runeToDot(e.lim[i])
				if e.lim[i] != e.lim[i+1] {
					label += "-" + runeToDot(e.lim[i+1])
				}
			}
			label += "]\"]"
		}
		_, _ = fmt.Fprintf(b.out, "  %v -> %v%v;\n", u.n, e.dst.n, label)
	}
	for _, e := range u.e {
		if !b.done[e.dst] {
			b.show(e.dst)
		}
	}
}
