# Nex

Nex is a lexer similar to Lex/Flex that:

- generates Go code instead of C code
- integrates with Go's yacc instead of YACC/Bison
- supports UTF-8
- supports nested _structural regular expressions_.
  See  [Structural Regular Expressions][sre] by Rob Pike.

Go has a less general [scanner package][scanner-package],
but it is especially suited for tokenizing Go code.

## Installation

```shell
go install github.com/liran-funaro/nex
```

## Example

One simple example in the [Flex manual][flex-manual]
is a scanner that counts characters and lines. The program is
similar in Nex:

```
/\n/{ nLines++; nChars++ }
/./{ nChars++ }
//
package main
import ("fmt";"os")
func main() {
  var nLines, nChars int
  NN_FUN(NewLexer(os.Stdin))
  fmt.Printf("%d %d\n", nLines, nChars)
}
```

The syntax resembles Awk more than Flex: each regex must be delimited. An empty
regex terminates the rules section and signifies the presence of user code,
which is printed on standard output with `NN_FUN` replaced by the generated
scanner.

Name the above example `lc.nex`. Then compile and run it by typing:

```shell
$ nex -r -s lc.nex
```

The program runs on standard input and output. For example:

```shell
$ nex -r -s lc.nex < /usr/share/dict/words
99171 938587
```

To generate Go code for a scanner without compiling and running it, type:

```shell
$ nex -s < lc.nex  # Prints code on standard output.
```

or:

```shell
$ nex -s lc.nex  # Writes code to lc.nn.go
```

Purists unable to tolerate text substitution using the `NN_FUN` will need more code:

```
/\n/{ lval.l++; lval.c++ }
/./{ lval.c++ }
//
package main
import ("fmt";"os")
type yySymType struct { l, c int }
func main() {
  v := new(yySymType)
  NewLexer(os.Stdin).Lex(v)
  fmt.Printf("%d %d\n", v.l, v.c)
}
```

and must run `nex` without the `-s` option:

```shell
$ nex lc.nex
```

We could avoid defining a struct by using globals instead, but even then we
need a throwaway definition of `yySymType`.

The `yy` prefix can be modified by adding `-y` option. When using yacc, it must use the same prefix:

```shell
$ nex -p YY lc.nex && go tool yacc -p YY && go run lc.nn.go y.go
```

## Toy Pascal

The Flex manual also exhibits a [scanner for a toy Pascal-like language][flex-manual],
though last I checked, its comment regex was a little buggy. Here is a
modified Nex version, without string-to-number conversions:

```
/[0-9]+/          { println("An integer:", txt()) }
/[0-9]+\.[0-9]*/  { println("A float:", txt()) }
/if|then|begin|end|procedure|function/
                  { println( "A keyword:", txt()) }
/[a-z][a-z0-9]*/  { println("An identifier:", txt()) }
/\+|-|\*|\//      { println("An operator:", txt()) }
/[ \t\n]+/        { /* eat up whitespace */ }
/./               { println("Unrecognized character:", txt()) }
/{[^\{\}\n]*}/    { /* eat up one-line comments */ }
//
package main
import "os"
func main() {
  lex := NewLexer(os.Stdin)
  txt := func() string { return lex.Text() }
  NN_FUN(lex)
}
```

Enough simple examples! Let us see what nesting can do.

## Peter into silicon

In [Structural Regular Expressions][sre], Pike imagines a newline-agnostic Awk
that operates on matched text, rather than on the whole line containing a
match, and writes code converting an input array of characters into
descriptions of rectangles. For example, given an input such as:

```
    #######
   #########
  ####  #####
 ####    ####   #
 ####      #####
####        ###
########   #####
#### #########
#### #  # ####
## #  ###   ##
###    #  ###
###    ##
 ##   #
  #   ####
  # #
##   #   ##
```

we wish to produce something like:

```
rect 5 12 1 2
rect 4 13 2 3
rect 3 7 3 4
rect 9 14 3 4
...
rect 10 12 16 17
```

With Nex, we don't have to imagine: such programs are real. Below are practical
Nex programs that strongly resemble their theoretical counterparts.
The one-character-at-a-time variant:

```
/ /{ x++ }
/#/{ println("rect", x, x+1, y, y+1); x++ }
/\n/{ x=1; y++ }
//
package main
import "os"
func main() {
  x, y := 1, 1
  NN_FUN(NewLexer(os.Stdin))
}
```

The one-run-at-a-time variant:

```
/ +/{ x+=len(txt()) }
/#+/{ println("rect", x, x+len(txt()), y, y+1); x+=len(txt()) }
/\n/{ x=1; y++ }
//
package main
import "os"
func main() {
  x, y := 1, 1
  lex := NewLexer(os.Stdin)
  txt := func() string { return lex.Text() }
  NN_FUN(lex)
}
```

The programs are more verbose than Awk because Go is the backend.

## Rob but not robot

Pike demonstrates how nesting structural expressions leads to a few simple text
editor commands to print all lines containing "rob" but not "robot". Though Nex
fails to separate looping from matching, a corresponding program is bearable:

```
/[^\n]*\n/ < { isrobot = false; isrob = false }
  /robot/    { isrobot = true }
  /rob/      { isrob = true }
>            { if isrob && !isrobot { fmt.print(lex.Text()) } }
//
package main
import ("fmt";"os")
func main() {
  var isrobot, isrob bool
  lex := NewLexer(os.Stdin)
  NN_FUN(lex)
}
```

The `<` and `>` delimit nested expressions, and work as follows.
On reading a line, we find it matches the first regex, so we execute the code
immediately following the opening `<`.

Then it's as if we run Nex again, except we focus only on the patterns and
actions up to the closing `>`, with the matched line as the entire input. Thus,
we look for occurrences of "rob" and "robot" in just the matched line and set
flags accordingly.

After the line ends, we execute the code following the closing `>` and return
to our original state, scanning for more lines.

## Word count

We can simultaneously count lines, words, and characters with Nex thanks to
nesting:

```
/[^\n]*\n/ < {}
  /[^ \t\r\n]*/ < {}
    /./  { nChars++ }
  >      { nWords++ }
  /./    { nChars++ }
>        { nLines++ }
//
package main
import ("fmt";"os")
func main() {
  var nLines, nWords, nChars int
  NN_FUN(NewLexer(os.Stdin))
  fmt.Printf("%d %d %d\n", nLines, nWords, nChars)
}
```

The first regex matches entire lines: each line is passed to the first level
of nested regexes. Within this level, the first regex matches words in the
line: each word is passed to the second level of nested regexes. Within
the second level, a regex causes every character of the word to be counted.

Lastly, we also count whitespace characters, a task performed by the second
regex of the first level of nested regexes. We could remove this statement
to count only non-whitespace characters.

## UTF-8

The following Nex program converts Eastern Arabic numerals to the digits used
in the Western world, and also Chinese phrases for numbers (the analog of
something like "one-hundred and fifty-three") into digits.

```
/[零一二三四五六七八九十百千]+/ { fmt.Print(zhToInt(txt())) }
/[٠-٩]/ {
  // The above character class might show up right-to-left in a browser.
  // The equivalent of 0 should be on the left, and the equivalent of 9 should
  // be on the right.
  //
  // The Eastern Arabic numerals are ٠١٢٣٤٥٦٧٨٩.
  fmt.Print([]rune(txt())[0] - rune('٠'))
}
/./ { fmt.Print(txt()) }
//
package main
import ("fmt";"os")
func zhToInt(s string) int {
  n := 0
  prev := 0
  f := func(m int) {
    if 0 == prev { prev = 1 }
    n += m * prev
    prev = 0
  }
  for _, c := range s {
    for m, v := range []rune("一二三四五六七八九") {
      if v == c {
	prev = m+1
	goto continue2
      }
    }
    switch c {
    case '零':
    case '十': f(10)
    case '百': f(100)
    case '千': f(1000)
    }
continue2:
  }
  n += prev
  return n
}
func main() {
  lex := NewLexer(os.Stdin)
  txt := func() string { return lex.Text() }
  NN_FUN(lex)
}
```

## nex and Go's yacc

The parser generated by `goyacc` exports so little that it's easiest to
keep the lexer and the parser in the same package.

Here's a yacc file based on the reverse-Polish-notation calculator example from the
[Bison manual][bison-manual]:

```goyacc
%{
package main
import "fmt"
%}

%union {
  n int
}

%token NUM
%%
input:    /* empty */
       | input line
;

line:     '\n'
       | exp '\n'      { fmt.Println($1.n); }
;

exp:     NUM           { $$.n = $1.n;        }
       | exp exp '+'   { $$.n = $1.n + $2.n; }
       | exp exp '-'   { $$.n = $1.n - $2.n; }
       | exp exp '*'   { $$.n = $1.n * $2.n; }
       | exp exp '/'   { $$.n = $1.n / $2.n; }
	/* Unary minus    */
       | exp 'n'       { $$.n = -$1.n;       }
;
%%
```

We must import `fmt` even if we don't use it, since code generated by yacc
needs it. Also, the `%union` is mandatory; it generates `yySymType`.

Call the above `rp.y`. Then a suitable lexer, say `rp.nex`, might be:

```
/[ \t]/  { /* Skip blanks and tabs. */ }
/[0-9]*/ { lval.n,_ = strconv.Atoi(yylex.Text()); return NUM }
/./ { return int(yylex.Text()[0]) }
//
package main
import ("os";"strconv")
func main() {
  yyParse(NewLexer(os.Stdin))
}
```

Compile the two with:

```shell
$ nex rp.nex && go tool yacc rp.y && go build y.go rp.nn.go
```

For brevity, we work in the `main` package. In a larger project we might want
to write a package that exports a function wrapped around `yyParse()`. This is
fine, provided the parser and the lexer are both in the same package.

Alternatively, we could use yacc's `-p` option to change the prefix from `yy`
to one that begins with an uppercase letter.

## Matching the beginning and end of input

We can simulate awk's BEGIN and END blocks with a regex that matches the entire
input:

```
/.*/ < { println("BEGIN") }
  /a/  { println("a") }
>      { println("END") }
//
package main
import "os"
func main() {
  NN_FUN(NewLexer(os.Stdin))
}
```

However, this causes Nex to read the entire input into memory. To solve
this problem, Nex supports the following syntax:

```
<      { println("BEGIN") }
  /a/  { println("a") }
>      { println("END") }
//
package main
import "os"
func main() {
  NN_FUN(NewLexer(os.Stdin))
}
```

In other words, if a bare `<` appears as the first pattern, then its action is
executed before reading the input. The last pattern must be a bare '>', and its
action is executed on end of input.

Additionally, no empty regex is needed to mark the beginning of the Go program.
Fortunately, an empty regex is also a Go comment, so there's no harm done if
present.

## Matching Nuances

Among rules in the same scope, the longest matching pattern takes precedence.
In event of a tie, the first pattern wins.

Un-anchored patterns never match the empty string. For example, `/(foo)*/ {}`
matches "foo" and "foofoo", but not "".

Anchored patterns can match the empty string at most once; after the match, the
start or end null strings are "used up" so will not match again.

Internally, this is implemented by omitting the very first check to see if the
current state is accepted when running the DFA corresponding to the regex. An
alternative would be to simply ignore matches of length 0, but I chose to allow
anchored empty matches just in case there turn out to be applications for them.
I'm open to changing this behaviour.

## Contributing and Testing

Check out this repo (or a clone) into a directory:

```shell
git clone https://github.com/liran-funaro/nex.git
```

## Reference

```go
func NewLexer(in io.Reader) *Lexer

// NewLexerWithInit creates a new Lexer object, runs the given callback on it,
// then returns it.
func NewLexerWithInit(in io.Reader, initFun func(*Lexer)) *Lexer

// Lex runs the lexer. Always returns 0.
// When the -s option is given, this function is not generated;
// instead, the NN_FUN macro runs the lexer.
func (yylex *Lexer) Lex(lval *yySymType) int

// Text returns the matched text.
func (yylex *Lexer) Text() string

// Line returns the current line number.
// The first line is 0.
func (yylex *Lexer) Line() int

// Column returns the current column number.
// The first column is 0.
func (yylex *Lexer) Column() int
```

# Note from the Original Author

I wrote this code to get acquainted with Go and also to explore some of the ideas in the paper.
Also, I've always been meaning to implement algorithms I learned from a compilers course I took many years ago.
Back then, we never coded them; merely understanding the theory was enough to pass the exam.

[sre]: http://doc.cat-v.org/bell_labs/structural_regexps/se.pdf
[scanner-package]: http://golang.org/pkg/scanner/
[flex-manual]: http://flex.sourceforge.net/manual/Simple-Examples.html
[bison-manual]: http://dinosaur.compilertools.net/bison/bison_5.html
