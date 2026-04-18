package a11ylint_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/mike-ward/go-map/a11ylint"
)

// TestAnalyzer runs the analyzer against testdata/src/a, which
// imports a testdata-stub mapview. analysistest loads testdata in
// GOPATH mode so the stub resolves without touching the real module.
func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), a11ylint.Analyzer, "a")
}
