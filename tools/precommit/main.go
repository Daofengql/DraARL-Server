// Precommit validates index formatting without rewriting partially staged files.
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"strings"
)

func main() {
	output, err := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z").Output()
	if err != nil {
		fail(err)
	}
	frontend, backend := false, false
	for _, name := range bytes.Split(output, []byte{0}) {
		path := string(name)
		if path == "" {
			continue
		}
		if strings.HasSuffix(path, ".go") {
			backend = true
			source, err := exec.Command("git", "show", ":"+path).Output() // #nosec G204 -- Only fixed local tool names are selected by code; arguments are passed without a shell.
			if err != nil {
				fail(err)
			}
			formatted, err := format.Source(source)
			if err != nil {
				fail(fmt.Errorf("%s: %w", path, err))
			}
			if !bytes.Equal(source, formatted) {
				fail(fmt.Errorf("run gofmt on %s and stage it again", path))
			}
		}
		frontend = frontend || strings.HasPrefix(path, "www/")
	}
	if backend {
		run("", "go", "vet", "./cmd/...", "./internal/...", "./pkg/...", "./test/...", "./tools/...")
	}
	if frontend {
		run("www", "npm", "run", "lint")
		run("www", "npm", "run", "typecheck")
		run("www", "npm", "run", "test:unit")
	}
}

func run(dir, name string, args ...string) {
	if name == "npm" && os.PathSeparator == '\\' {
		args = append([]string{"/d", "/c", "npm.cmd"}, args...)
		name = "cmd.exe"
	}
	cmd := exec.Command(name, args...) // #nosec G204 -- Only fixed local tool names are selected by code; arguments are passed without a shell.
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
