package main

import (
	"log"
	"os"

	"github.com/liran-funaro/nex/nex"
)

func main() {
	if err := nex.Exec(os.Args[0], os.Args[1:]...); err != nil {
		log.Fatal(err)
	}
}
