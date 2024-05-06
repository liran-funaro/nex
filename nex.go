package main

import (
	"os"

	"github.com/liran-funaro/nex/nex"
)

func main() {
	nex.Exec(os.Args[0], os.Args[1:]...)
}
