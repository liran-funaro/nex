package main

import (
	_ "embed"
	"fmt"
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
	if nexBin, err = filepath.Abs(os.Getenv("GOPATH") + "/bin/nex"); err != nil {
		panic(err)
	}
	if _, err := os.Stat(nexBin); err != nil {
		if nexBin, err = filepath.Abs("../nex"); err != nil {
			panic(err)
		}
		if _, err := os.Stat(nexBin); err != nil {
			panic("cannot find nex binary")
		}
	}
}

//go:embed input.txt
var testInput string

//go:embed output.txt
var testOutput string

func TestWax(t *testing.T) {
	tmpdir := t.TempDir()
	run := func(s string) {
		v := strings.Split(s, " ")
		output, err := exec.Command(v[0], v[1:]...).CombinedOutput()
		require.NoError(t, err, fmt.Sprintf("cmd: %s\noutput: %s\n", s, output))
	}
	run("cp tacky.nex tacky.y tacky.go build.sh " + tmpdir)
	wd, err := os.Getwd()
	require.NoError(t, err, "Getwd")
	require.NoError(t, os.Chdir(tmpdir), "Chdir")
	defer func() {
		require.NoError(t, os.Chdir(wd), "Chdir")
	}()
	require.NoError(t, os.Setenv("NEXBIN", nexBin), "Setenv")
	run("./build.sh")
	cmd := exec.Command("./tacky")
	cmd.Stdin = strings.NewReader(testInput)
	got, err := cmd.CombinedOutput()
	require.NoError(t, err, "CombinedOutput")
	if testOutput != string(got) {
		t.Fatalf("want %q, got %q", testOutput, string(got))
	}
}
