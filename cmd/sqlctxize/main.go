package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/p1ass/sqlctxize"
)

func main() {
	overwrite := flag.Bool("w", false, "write the result to the source files instead of stdout")
	dir := flag.String("dir", ".", "directory of the module to rewrite")
	exclude := flag.String("exclude", "", "comma-separated package directories (relative to -dir, glob allowed) to leave untouched")
	flag.Parse()

	var patterns []string
	for p := range strings.SplitSeq(*exclude, ",") {
		if p = strings.TrimSpace(p); p != "" {
			patterns = append(patterns, p)
		}
	}
	results, err := sqlctxize.Rewrite(sqlctxize.Options{Dir: *dir, Exclude: patterns})
	if err != nil {
		fmt.Fprintln(os.Stderr, "sqlctxize:", err)
		os.Exit(1)
	}
	for _, r := range results {
		if *overwrite {
			if err := os.WriteFile(r.Path, r.Content, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "sqlctxize:", err)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "rewrote", r.Path)
			continue
		}
		fmt.Printf("// %s\n%s\n", r.Path, r.Content)
	}
}
