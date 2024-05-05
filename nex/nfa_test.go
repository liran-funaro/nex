package nex

import (
	"fmt"
	"regexp/syntax"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ################################################################################################################
// # Debugging
// ################################################################################################################

func makeIndent(indent int) string {
	r := make([]string, indent)
	for i := range r {
		r[i] = " "
	}
	return strings.Join(r, "")
}

func runeList(runes []rune) string {
	r := make([]string, len(runes))
	for i := range r {
		r[i] = fmt.Sprintf("%c", runes[i])
	}
	return fmt.Sprintf("[%s]", strings.Join(r, ","))
}

func showFlags(f syntax.Flags) string {
	if f == 0 {
		return "POSIX"
	}

	var flags []string

	//  FoldCase      Flags = 1 << iota // case-insensitive match
	if f&syntax.FoldCase != 0 {
		flags = append(flags, "fold-case")
	}
	//	Literal                         // treat pattern as literal string
	if f&syntax.Literal != 0 {
		flags = append(flags, "literal")
	}

	//	MatchNL = ClassNL | DotNL
	if f&syntax.MatchNL != 0 {
		flags = append(flags, "match-nl")
	} else {
		//	ClassNL                         // allow character classes like [^a-z] and [[:space:]] to match newline
		if f&syntax.ClassNL != 0 {
			flags = append(flags, "class-nl")
		}
		//	DotNL                           // allow . to match newline
		if f&syntax.DotNL != 0 {
			flags = append(flags, "dot-nl")
		}
	}

	//	Perl        = ClassNL | OneLine | PerlX | UnicodeGroups // as close to Perl as possible
	if f&syntax.Perl != 0 {
		flags = append(flags, "perl")
	} else {
		//	OneLine                         // treat ^ and $ as only matching at beginning and end of text
		if f&syntax.OneLine != 0 {
			flags = append(flags, "one-line")
		}
		//	PerlX                           // allow Perl extensions
		if f&syntax.PerlX != 0 {
			flags = append(flags, "perl-x")
		}
		//	UnicodeGroups                   // allow \p{Han}, \P{Han} for Unicode group and negation
		if f&syntax.UnicodeGroups != 0 {
			flags = append(flags, "unicode-groups")
		}
	}

	//	NonGreedy                       // make repetition operators default to non-greedy
	if f&syntax.NonGreedy != 0 {
		flags = append(flags, "not-greedy")
	}
	//	WasDollar                       // regexp OpEndText was $, not \z
	if f&syntax.WasDollar != 0 {
		flags = append(flags, "was-dollar")
	}
	//	Simple                          // regexp contains no counted repetition
	if f&syntax.WasDollar != 0 {
		flags = append(flags, "simple")
	}
	return fmt.Sprintf("[%s]", strings.Join(flags, ","))
}

func showRe(r *syntax.Regexp, indent int) {
	i0 := makeIndent(indent)
	fmt.Printf("%s%s {", i0, r.Op.String())
	if r.Name != "" {
		fmt.Printf("(%s)", r.Name)
	}
	fmt.Printf("\n")

	indent += 2
	i := makeIndent(indent)

	fmt.Printf("%sflags: %s\n", i, showFlags(r.Flags))

	if len(r.Rune) > 0 {
		fmt.Printf("%srunes: %s\n", i, runeList(r.Rune))
	}
	if len(r.Sub) > 0 {
		fmt.Printf("%ssub:\n", i)
		for _, s := range r.Sub {
			showRe(s, indent+2)
		}
	}
	if r.Min > 0 || r.Max > 0 {
		fmt.Printf("%smin-max: {%d, %d}\n", i, r.Min, r.Max)
	}

	fmt.Printf("%s}\n", i0)
}

func parseAndShowNfa(exp string) error {
	fmt.Println(exp)
	r, err := syntax.Parse(exp, syntax.Perl)
	if err != nil {
		return err
	}
	showRe(r, 0)
	return nil
}

func TestExpressions(t *testing.T) {
	require.NoError(t, parseAndShowNfa("(?i) [0-9a-z]+ x{2,5} (abc|c) (abc|a) (a|d) (a|b) y{3} y{3,} [^abc]"))
}
