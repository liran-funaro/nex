all: install test

install: nex/main.go nex/nex.go
	go install ./nex

test: $(shell find test -type f)
	go test ./test

clean:
	rm -f $(NEX)

.PHONY: all test clean
