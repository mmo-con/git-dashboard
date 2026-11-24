package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mmo-con/headquarters/internal/config"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/initconfig"
)

// runInit implements "headquarters init": it scans directories and prints or
// writes a starting configuration.
func runInit(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	flags := flag.NewFlagSet("headquarters init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	maxDepth := flags.Int("max-depth", config.DefaultMaxDepth, "number of directory levels below each directory that are scanned")
	output := flags.String("o", "", "write the configuration to `file` instead of standard output")
	force := flags.Bool("force", false, "replace an existing output file")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: headquarters init [options] directory...")
		fmt.Fprintln(stderr, "\nScans the directories and prints a starting configuration in which each")
		fmt.Fprintln(stderr, "first-level directory with repositories is a family. To scan a directory")
		fmt.Fprintln(stderr, "named init without generating a configuration, use headquarters ./init.")
		fmt.Fprintln(stderr, "\noptions:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	usageError := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "headquarters init: "+format+"\n", a...)
		return 2
	}
	if flags.NArg() == 0 {
		fmt.Fprintln(stderr, "headquarters init: expected at least one directory")
		flags.Usage()
		return 2
	}
	if *maxDepth < 1 {
		return usageError("--max-depth must be at least 1")
	}
	var roots []string
	for _, arg := range flags.Args() {
		if strings.HasPrefix(arg, "-") {
			return usageError("options must precede the directories: %q", arg)
		}
		root, err := directoryArg(arg)
		if err != nil {
			return usageError("%v", err)
		}
		roots = append(roots, root)
	}

	ctx := context.Background()
	client, _, err := git.Open(ctx, "git")
	if err != nil {
		fmt.Fprintln(stderr, "headquarters init:", err)
		return 1
	}
	res, err := initconfig.Generate(ctx, initconfig.Options{
		Roots:    roots,
		MaxDepth: *maxDepth,
		Home:     getenv("HOME"),
		Command:  strings.Join(append([]string{"headquarters", "init"}, args...), " "),
	}, client)
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, "headquarters init: warning:", w)
	}
	if err != nil {
		fmt.Fprintln(stderr, "headquarters init:", err)
		return 1
	}
	if *output == "" {
		if _, err := stdout.Write(res.Config); err != nil {
			return 1
		}
		return 0
	}
	if err := initconfig.WriteFile(*output, res.Config, *force); err != nil {
		fmt.Fprintln(stderr, "headquarters init:", err)
		return 1
	}
	return 0
}
