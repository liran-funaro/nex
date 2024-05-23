#!/bin/bash
${NEXBIN:=nex} tacky.nex
goyacc tacky.y
go build tacky.go tacky.nn.go y.go
