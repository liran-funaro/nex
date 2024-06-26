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
