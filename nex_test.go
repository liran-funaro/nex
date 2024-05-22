package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liran-funaro/nex/nex"
	"github.com/liran-funaro/nex/nex/parser"
	"github.com/stretchr/testify/require"
)

//go:embed test-data/toy-input.txt
var toyInput string

//go:embed test-data/toy-output.txt
var toyOutput string

//go:embed test-data/robo-input.txt
var roboInput string

//go:embed test-data/robo-output.txt
var roboOutput string

//go:embed test-data/peter-input.txt
var peterInput string

//go:embed test-data/peter-output.txt
var peterOutput string

func TestNexPrograms(t *testing.T) {
	t.Parallel()
	for _, x := range []struct {
		prog, in, out string
	}{
		{"lc.nex", "no newline", "0 10\n"},
		{"lc.nex", "one two three\nfour five six\n", "2 28\n"},

		{"toy.nex", toyInput, toyOutput},

		{"wc.nex", "no newline", "0 0 0\n"},
		{"wc.nex", "\n", "1 0 1\n"},
		{"wc.nex", "1\na b\nA B C\n", "3 6 12\n"},
		{"wc.nex", "one two three\nfour five six\n", "2 6 28\n"},

		{"rob.nex", roboInput, roboOutput},

		{"peter.nex", peterInput, peterOutput},
		{"peter2.nex", "###\n#\n####\n", "rect 1 4 1 2\nrect 1 2 2 3\nrect 1 5 3 4\n"},
		{"u.nex", "١ + ٢ + ... + ١٨ = 一百五十三", "1 + 2 + ... + 18 = 153"},
		{"bug50.nex", "# comment 1\nhello42:\n# comment 2\n\na\nblah:42x\n", "COMMENT: # comment 1\nTEXT: hello42\nERROR: :\nCOMMENT: # comment 2\nTEXT: a\nTEXT: blah:42x\n"},
	} {
		t.Run(x.prog, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			require.NoError(t, nex.ExecWithParams(&nex.ExecParams{
				InputFilename: path.Join("test-data", x.prog),
				Standalone:    true,
				RunProgram:    true,
				Stdin:         strings.NewReader(x.in),
				Stderr:        os.Stderr,
				Stdout:        &stdout,
			}))
			require.Equal(t, x.out, string(stdout.Bytes()))
		})
	}
}

const cornerCasesMainDoc = `//
package main
import ("os")

type yySymType = string

func main() {
  lval := new(yySymType)
  l := NewLexer(os.Stdin)
  for l.Lex(lval) != 0 { }
  fmt.Print(string(*lval))
}
`

func TestCornerCases(t *testing.T) {
	t.Parallel()
	//tmpdir := t.TempDir()
	tmpdir, err := filepath.Abs("./test-data/output")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(tmpdir, os.ModePerm))

	for i, x := range []struct {
		name, prog, in, out string
	}{
		{
			"Test parentheses and $",
			`
/[a-z]*/ <   *lval += "["
  /a(($*|$$)($($)$$$))$($$$)*/
             *lval += "0"
  /(e$|f$)/  *lval += "1"
  /(qux)*/   *lval += "2"
  /.$/       *lval += "."
>            *lval += "]"
`,
			"a b c d e f g aaab aaaa eeeg fffe quxqux quxq quxe",
			"[0][.][.][.][1][1][.][.][0][.][1][2][2.][21]",
		}, {
			"Exercise ^ and rule precedence",
			`
/[a-z]*/ <  { *lval += "[" }
  /((^*|^^)(^(^)^^^))^(^^^)*bar/ { *lval += "0" }
  /(^foo)*/ { *lval += "1" }
  /^fooo$/  { *lval += "2" }
  /^f(oo)*/ { *lval += "3" }
  /^foo*/   { *lval += "4" }
  /^/       { *lval += "." }
>           { *lval += "]" }
`,
			"foo bar foooo fooo fooooo fooof baz foofoo",
			"[1][0][3][2][4][4][.][1]",
		}, {
			`Exercise \b and rule precedence`,
			`
/[a-z,]*/ <  { *lval += "[" }
  /((\b*|\b\b)(\b(\b)\b\b\b))\b(\b\b\b)*bar\b/ { *lval += "0" }
  /(\bfoo\b)*/ { *lval += "1" }
  /\bfooo\b$/  { *lval += "2" }
  /\bf(oo)*\b/ { *lval += "3" }
  /\bfoo*\b/   { *lval += "4" }
  /\b/         { *lval += "." }
>              { *lval += "]" }
`,
			"foo bar foooo fooo fooooo fooof baz foofoo foo,foo",
			"[1.][0.][3.][2.][4.][..][..][..][1.1.]",
		}, {
			"Anchored empty matches",
			`
/^/ { *lval += "BEGIN" }
/$/ { *lval += "END" }
`,
			"", "BEGIN",
		}, {
			"Anchored empty matches",
			`
/$/ { *lval += "END" }
/^/ { *lval += "BEGIN" }
`, "", "END",
		}, {
			"Anchored empty matches", `
/^$/ { *lval += "BOTH" }
/^/ { *lval += "BEGIN" }
/$/ { *lval += "END" }
`,
			"", "BOTH",
		}, {
			"Built-in Line and Column counters",
			`
/\*/    { *lval += yySymType(fmt.Sprintf("[%d,%d]", yylex.Line(), yylex.Column())) }
`,
			`..*.
**
...
...*.*
*
`,
			"[0,2][1,0][1,1][3,3][3,5][4,0]",
		},
		{
			"Patterns like awk's BEGIN and END",
			`
<          { *lval += "[" }
  /[0-9]*/ { *lval += "N" }
  /;/      { *lval += ";" }
  /./      { *lval += "." }
>          { *lval += "]\n" }
`,
			"abc 123 xyz;a1b2c3;42", "[....N....;.N.N.N;N]\n",
		}, {
			"A partial match regex has no effect on an immediately following match",
			`
/abcd/ { *lval += "ABCD" }
/\n/   { *lval += "\n" }
`,
			"abcd\nbabcd\naabcd\nabcabcd\n", "ABCD\nABCD\nABCD\nABCD\n",
		},

		// Nested regex test. The simplistic parser means we must use commented
		// braces to balance out quoted braces.
		// Sprinkle in a couple of return statements to check Lex() saves stack
		// state correctly between calls.
		{
			"Nested regex test",
			`
/a[bcd]*e/ < { *lval += "[" }
  /a/        { *lval += "A" }
  /bcd/ <    { *lval += "(" }
  /c/        { *lval += "X"; return 1 }
  >          { *lval += ")" }
  /e/        { *lval += "E" }
  /ccc/ <    {
    *lval += "{"
    // }  [balance out the quoted left brace]
  }
  /./        { *lval += "?" }
  >          {
    // {  [balance out the quoted right brace]
    *lval += "}"
    return 2
  }
>            { *lval += "]" }
/\n/ { *lval += "\n" }
/./ { *lval += "." }
`,
			"abcdeabcabcdabcdddcccbbbcde", "[A(X)E].......[A(X){???}(X)E]",
		}, {
			"Exercise hyphens in character classes",
			`
/[a-z-]*/ < { *lval += "[" }
  /[^-a-df-m]/ { *lval += "0" }
  /./       { *lval += "1" }
>           { *lval += "]" }
/\n/ { *lval += "\n" }
/./ { *lval += "." }
`,
			"-azb-ycx@d--w-e-", "[11011010].[1110101]",
		}, {
			"Overlapping character classes",
			`
/[a-e]+[d-h]+/ { *lval += "0" }
/[m-n]+[k-p]+[^k-r]+[o-p]+/ { *lval += "1" }
/./ { *(*string)(lval) += yylex.Text() }
`,
			"abcdefghijmnopabcoq", "0ij1q",
		}, {
			"Repeat",
			`
/\s*/         { *lval += yylex.Text() }
/(?i)a{2,5}/  { *lval += "a" }
/(?i)b{3}/    { *lval += "b" }
/(?i)c{3,}/   { *lval += "c" }
/(?i)d+/      { *lval += "d" }
/(?i)(efg|e)/ { *lval += "e" }
/\w+/     { *lval += "." }
`,
			`
aAaaa aa a aA aaaaaa
B bb bbB Bbbb
C cc ccC cCcC
d Dd dDDd ddddDdddd
efg e ef eg`, `
a a . a .
. . b .
. . c c
d d d d
e e . .`,
		}, {
			"Delim and escape",
			`
/a\// { *lval += "0" }
_b\__  { *lval += "1" }
"\s+" { *lval += yylex.Text() }
'.'   { *lval += "." }
`,
			"a/ a\\ aa b_ b\\ bb c", "0 .. .. 1 .. .. .",
		},
	} {
		t.Run(fmt.Sprintf("[%d] %s", i, x.name), func(t *testing.T) {
			t.Parallel()
			program, err := parser.ParseNex(strings.NewReader(x.prog + cornerCasesMainDoc))
			require.NoError(t, err)
			b := nex.LexerBuilder{}
			code, err := b.DumpFormattedLexer(program)
			require.NoError(t, err)
			progDir := path.Join(tmpdir, fmt.Sprintf("prog-%d", i))
			require.NoError(t, os.MkdirAll(progDir, os.ModePerm))
			outPath := path.Join(progDir, "main.go")
			require.NoError(t, os.WriteFile(outPath, code, os.ModePerm))
			testProgram(t, tmpdir, x.in, x.out, outPath)
		})
	}
}

//go:embed test-data/rp-input.txt
var rpInput string

//go:embed test-data/rp-output.txt
var rpOutput string

// Test the reverse-Polish notation calculator rp.{nex,y}.
func TestNexPlusYacc(t *testing.T) {
	t.Parallel()
	testWithYacc(t, "test-data", "rp.nex", "rp.y", nil, "", rpInput, rpOutput)
}

//go:embed test-data/tacky/input.txt
var testTackyInput string

//go:embed test-data/tacky/output.txt
var testTackyOutput string

func TestWax(t *testing.T) {
	t.Parallel()
	testWithYacc(t, path.Join("test-data", "tacky"), "tacky.nex", "tacky.y", []string{"tacky.go"},
		"p *Tacky", testTackyInput, testTackyOutput)
}

// ################################################################################
// # Helper functions
// ################################################################################

func replaceInFile(t *testing.T, filepath, old, new string) {
	data, err := os.ReadFile(filepath)
	require.NoError(t, err)
	data = []byte(strings.Replace(string(data), old, new, 1))
	require.NoError(t, os.WriteFile(filepath, data, 0o666))
}

func copyToDir(t *testing.T, dst, src string) {
	dst = filepath.Join(dst, filepath.Base(src))
	s, err := os.Open(src)
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()
	d, err := os.Create(dst)
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	_, err = io.Copy(d, s)
	require.NoError(t, err)
}

func runCmd(t *testing.T, cwd, bin string, s ...string) {
	cmd := exec.Command(bin, s...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "Command: %s\nOutput: %s\n", s, string(output))
}

func testProgram(t *testing.T, cwd, input, output string, goFiles ...string) {
	cmd := exec.Command("go", append([]string{"run"}, goFiles...)...)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr
	got, err := cmd.Output()
	require.NoError(t, err, "Output")
	require.Equal(t, output, string(got))
}

func testWithYacc(t *testing.T, srcDir, nexFile, yFile string, otherFiles []string, fields, input, output string) {
	cwd := t.TempDir()
	for _, f := range append(otherFiles, nexFile, yFile) {
		copyToDir(t, cwd, path.Join(srcDir, f))
	}

	nexFile = path.Join(cwd, nexFile)
	nexOutFile := nexFile + ".go"
	require.NoError(t, nex.Exec("nex", "-o", nexOutFile, nexFile))
	replaceInFile(t, nexOutFile, "// [NEX_END_OF_LEXER_STRUCT]", fields)

	yFile = path.Join(cwd, yFile)
	yOutFile := yFile + ".go"
	runCmd(t, cwd, "goyacc", "-o", yOutFile, yFile)

	goFiles := []string{nexOutFile, yOutFile}
	for _, f := range otherFiles {
		goFiles = append(goFiles, path.Join(cwd, f))
	}
	testProgram(t, cwd, input, output, goFiles...)
}
