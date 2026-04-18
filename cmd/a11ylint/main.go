// Command a11ylint runs the a11ylint Analyzer as a standalone binary.
// Invocation mirrors go/vet: "a11ylint ./..." from the module root.
// A non-zero exit indicates one or more overlay literals lacked an
// accessibility Label.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/mike-ward/go-map/a11ylint"
)

func main() { singlechecker.Main(a11ylint.Analyzer) }
