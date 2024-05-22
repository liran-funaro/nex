package main

import (
	"log"
	"os"

	"github.com/liran-funaro/nex/exec"
)

func main() {
	if err := exec.Execute(os.Args[0], os.Args[1:]...); err != nil {
		log.Fatal(err)
	}
}
