package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var nexBin string

func init() {
	var err error
	try := []string{
		filepath.Join(os.Getenv("GOPATH"), "bin", "nex"),
		filepath.Join("..", "nex"),
		"nex", // look in all of PATH
	}
	for _, path := range try {
		if path, err = exec.LookPath(path); err != nil {
			continue
		}
		if path, err = filepath.Abs(path); err != nil {
			panic(fmt.Sprintf("cannot get absolute path to nex binary: %s", err))
		}
		nexBin = path
		return
	}
	panic("cannot find nex binary")
}

//go:embed rp-input.txt
var rpInput string

//go:embed rp-output.txt
var rpOutput string

// Test the reverse-Polish notation calculator rp.{nex,y}.
func TestNexPlusYacc(t *testing.T) {
	tmpdir := t.TempDir()
	run := func(s string) {
		v := strings.Split(s, " ")
		err := exec.Command(v[0], v[1:]...).Run()
		require.NoError(t, err, s)
	}
	copyToDir(t, tmpdir, "rp.nex")
	copyToDir(t, tmpdir, "rp.y")
	wd, err := os.Getwd()
	require.NoError(t, err, "Getwd")
	require.NoError(t, os.Chdir(tmpdir), "Chdir")
	defer func() {
		require.NoError(t, os.Chdir(wd), "Chdir")
	}()
	run(nexBin + " rp.nex")
	run("goyacc rp.y")
	run("go build y.go rp.nn.go")
	cmd := exec.Command("./y")
	cmd.Stdin = strings.NewReader(rpInput)
	got, err := cmd.CombinedOutput()
	require.NoError(t, err, "CombinedOutput")
	if rpOutput != string(got) {
		t.Fatalf("want %q, got %q", rpOutput, string(got))
	}
}

//go:embed toy-input.txt
var toyInput string

//go:embed toy-output.txt
var toyOutput string

//go:embed robo-input.txt
var roboInput string

//go:embed robo-output.txt
var roboOutput string

//go:embed peter-input.txt
var peterInput string

//go:embed peter-output.txt
var peterOutput string

func TestNexPrograms(t *testing.T) {
	for i, x := range []struct {
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
			cmd := exec.Command(nexBin, "-r", "-s", x.prog)
			cmd.Stdin = strings.NewReader(x.in)
			got, err := cmd.CombinedOutput()
			require.NoError(t, err, fmt.Sprintf("program (%d): %s\ngot: %s\n", i, x.prog, string(got)))
			if string(got) != x.out {
				t.Fatalf("program: %s\nwant %q, got %q", x.prog, x.out, string(got))
			}
		})
	}
}

// To save time, we combine several test cases into a single nex program.
func TestGiantProgram(t *testing.T) {
	tmpdir := t.TempDir()
	wd, err := os.Getwd()
	require.NoError(t, err, "Getwd")
	require.NoError(t, os.Chdir(tmpdir), "Chdir")
	defer func() {
		require.NoError(t, os.Chdir(wd), "Chdir")
	}()
	s := "package main\n"
	body := ""
	for i, x := range []struct {
		prog, in, out string
	}{
		// Test parentheses and $.
		{`
/[a-z]*/ <  { *lval += "[" }
  /a(($*|$$)($($)$$$))$($$$)*/ { *lval += "0" }
  /(e$|f$)/ { *lval += "1" }
  /(qux)*/  { *lval += "2" }
  /$/       { *lval += "." }
>           { *lval += "]" }
`, "a b c d e f g aaab aaaa eeeg fffe quxqux quxq quxe",
			"[0][.][.][.][1][1][.][.][0][.][1][2][2][21]"},
		// Exercise ^ and rule precedence.
		{`
/[a-z]*/ <  { *lval += "[" }
  /((^*|^^)(^(^)^^^))^(^^^)*bar/ { *lval += "0" }
  /(^foo)*/ { *lval += "1" }
  /^fooo$/  { *lval += "2" }
  /^f(oo)*/ { *lval += "3" }
  /^foo*/   { *lval += "4" }
  /^/       { *lval += "." }
>           { *lval += "]" }
`, "foo bar foooo fooo fooooo fooof baz foofoo",
			"[1][0][3][2][4][4][.][1]"},
		// Anchored empty matches.
		{`
/^/ { *lval += "BEGIN" }
/$/ { *lval += "END" }
`, "", "BEGIN"},

		{`
/$/ { *lval += "END" }
/^/ { *lval += "BEGIN" }
`, "", "END"},

		{`
/^$/ { *lval += "BOTH" }
/^/ { *lval += "BEGIN" }
/$/ { *lval += "END" }
`, "", "BOTH"},
		// Built-in Line and Column counters.
		// Ugly hack to import fmt.
		{`"fmt"
/\*/    { *lval += yySymType(fmt.Sprintf("[%d,%d]", yylex.Line(), yylex.Column())) }
`,
			`..*.
**
...
...*.*
*
`, "[0,2][1,0][1,1][3,3][3,5][4,0]"},
		// Patterns like awk's BEGIN and END.
		{`
<          { *lval += "[" }
  /[0-9]*/ { *lval += "N" }
  /;/      { *lval += ";" }
  /./      { *lval += "." }
>          { *lval += "]\n" }
`, "abc 123 xyz;a1b2c3;42", "[....N....;.N.N.N;N]\n"},
		// A partial match regex has no effect on an immediately following match.
		{`
/abcd/ { *lval += "ABCD" }
/\n/   { *lval += "\n" }
`, "abcd\nbabcd\naabcd\nabcabcd\n", "ABCD\nABCD\nABCD\nABCD\n"},

		// Nested regex test. The simplistic parser means we must use commented
		// braces to balance out quoted braces.
		// Sprinkle in a couple of return statements to check Lex() saves stack
		// state correctly between calls.
		{`
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
`, "abcdeabcabcdabcdddcccbbbcde", "[A(X)E].......[A(X){???}(X)E]"},

		// Exercise hyphens in character classes.
		{`
/[a-z-]*/ < { *lval += "[" }
  /[^-a-df-m]/ { *lval += "0" }
  /./       { *lval += "1" }
>           { *lval += "]" }
/\n/ { *lval += "\n" }
/./ { *lval += "." }
`, "-azb-ycx@d--w-e-", "[11011010].[1110101]"},

		// Overlapping character classes.
		{`
/[a-e]+[d-h]+/ { *lval += "0" }
/[m-n]+[k-p]+[^k-r]+[o-p]+/ { *lval += "1" }
/./ { *(*string)(lval) += yylex.Text() }
`, "abcdefghijmnopabcoq", "0ij1q"},
	} {
		id := fmt.Sprintf("%v", i)
		s += `import "main/nex_test` + id + "\"\n"
		require.NoError(t, os.Mkdir("nex_test"+id, 0777), "Mkdir")
		// Ugly hack to import packages.
		prog := x.prog
		importLine := ""
		if prog[0] != '\n' {
			v := strings.SplitN(prog, "\n", 2)
			prog = v[1]
			importLine = "import " + v[0]
		}
		require.NoError(t, os.WriteFile(id+".nex", []byte(prog+`//
package nex_test`+id+`

`+importLine+`

type yySymType string

func Go() {
  x := NewLexer(bufio.NewReader(strings.NewReader(`+"`"+x.in+"`"+`)))
  lval := new(yySymType)
  for x.Lex(lval) != 0 { }
  s := string(*lval)
  if s != `+"`"+x.out+"`"+`{
    panic(`+"`"+x.prog+": want "+x.out+", got ` + s"+`)
  }
}
`), 0777), "WriteFile")
		_, cerr := exec.Command(nexBin, "-o", filepath.Join("nex_test"+id, "tmp.go"), id+".nex").CombinedOutput()
		require.NoError(t, cerr, "nex: "+s)
		body += "nex_test" + id + ".Go()\n"
	}
	s += "func main() {\n" + body + "}\n"
	err = os.WriteFile("tmp.go", []byte(s), 0777)
	require.NoError(t, err, "WriteFile")
	output, err := exec.Command("go", "mod", "init", "main").CombinedOutput()
	require.NoError(t, err, string(output))
	output, err = exec.Command("go", "run", ".").CombinedOutput()
	require.NoError(t, err, string(output))
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
