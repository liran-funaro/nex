.PHONY: all install test

all: install test

install: nex.go $(shell find . -type f)
	go install github.com/liran-funaro/nex

test: nex_test.go $(shell find . -type f) $(shell find test-data -type f)
	go test github.com/liran-funaro/nex
