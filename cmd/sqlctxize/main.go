package main

import (
	"github.com/p1ass/sqlctxize"
	"golang.org/x/tools/go/analysis/unitchecker"
)

func main() { unitchecker.Main(sqlctxize.Analyzer) }
