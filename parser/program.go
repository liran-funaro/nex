package parser

import (
	"fmt"
	"io"

	"github.com/liran-funaro/nex/graph"
)

type NexProgram struct {
	Id        int
	Regex     string
	StartCode string
	EndCode   string
	UserCode  string
	Children  []*NexProgram
	NFA       []*graph.Node
	DFA       []*graph.Node
}

func (r *NexProgram) GetRegex() string {
	return r.Regex
}

func (r *NexProgram) GetId() int {
	return r.Id
}

func (r *NexProgram) WriteNFADotGraph(writer io.Writer) error {
	if len(r.NFA) == 0 {
		return nil
	}
	graph.WriteDotGraph(writer, r.NFA[0], fmt.Sprintf("NFA_%d", r.Id))
	for _, c := range r.Children {
		if err := c.WriteNFADotGraph(writer); err != nil {
			return err
		}
	}
	return nil
}

func (r *NexProgram) WriteDFADotGraph(writer io.Writer) error {
	if len(r.DFA) == 0 {
		return nil
	}
	graph.WriteDotGraph(writer, r.DFA[0], fmt.Sprintf("DFA_%d", r.Id))
	for _, c := range r.Children {
		if err := c.WriteDFADotGraph(writer); err != nil {
			return err
		}
	}
	return nil
}
