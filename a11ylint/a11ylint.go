// Package a11ylint provides a go/analysis Analyzer that enforces the
// "every visible state has a label" accessibility constraint on the
// go-map overlay types whose Label field is currently consumed by the
// a11y path. It reports composite literals of mapview.Marker or
// mapview.InfoWindowAction that omit Label or set it to an empty
// string constant.
//
// Scope limitations — stated so false-confidence does not accrue:
//
//   - Polyline / Polygon / Circle carry a Label field but none of the
//     current a11y or focus paths read it. Enforcing the field today
//     would be lint theater. Expand the Analyzer when those overlays
//     grow actual screen-reader wiring.
//   - Positional composite literals are skipped. None of the target
//     structs are ergonomic positionally; the AST shape differs. If a
//     consumer writes one, the lint will not catch it.
//   - Field-by-field construction (var m Marker; m.Label = "x") is
//     invisible to this pass. Composite-literal construction is the
//     idiomatic form across the codebase and examples.
//   - _test.go files are skipped by default. Internal tests construct
//     overlays for geometry/state verification and intentionally
//     exercise no-Label fallback paths. Pass -tests to include them.
package a11ylint

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer is the exported go/analysis entrypoint. Embed in a
// golangci-lint custom plugin or run standalone via cmd/a11ylint.
var Analyzer = &analysis.Analyzer{
	Name:     "a11ylint",
	Doc:      "reports mapview overlays that lack an accessibility Label.",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

// includeTests toggles _test.go scanning. Off by default; internal
// tests construct overlays for geometry verification and deliberately
// exercise no-Label paths.
var includeTests bool

func init() {
	Analyzer.Flags.BoolVar(&includeTests, "tests", false,
		"include _test.go files in the scan")
}

// targetPkg is the import path hosting the overlay types. A string
// match keeps the Analyzer runnable against code that re-vendors
// go-map without forcing a module-graph resolution at lint time.
const targetPkg = "github.com/mike-ward/go-map/mapview"

// targets enumerates the struct type names whose composite literals
// require a non-empty Label field. Restricted to types whose Label is
// actually read by the a11y / focus path today (see package doc).
var targets = map[string]struct{}{
	"Marker":           {},
	"InfoWindowAction": {},
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	nodeFilter := []ast.Node{(*ast.CompositeLit)(nil)}
	insp.Preorder(nodeFilter, func(n ast.Node) {
		lit := n.(*ast.CompositeLit)
		if !includeTests {
			pos := pass.Fset.Position(lit.Pos())
			if strings.HasSuffix(pos.Filename, "_test.go") {
				return
			}
		}
		named := targetNamed(pass.TypesInfo.TypeOf(lit))
		if named == nil {
			return
		}
		obj := named.Obj()
		if obj == nil || obj.Pkg() == nil {
			return
		}
		if obj.Pkg().Path() != targetPkg {
			return
		}
		if _, ok := targets[obj.Name()]; !ok {
			return
		}
		checkLabel(pass, lit, obj.Name())
	})
	return nil, nil
}

// targetNamed unwraps pointer and alias layers to reach the underlying
// *types.Named for the composite literal's type. Returns nil when the
// type is not a named struct — covers nil, slice / map / array
// literals, and unnamed struct types.
func targetNamed(t types.Type) *types.Named {
	if t == nil {
		return nil
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, _ := t.(*types.Named)
	return named
}

// checkLabel reports the literal when Label is missing or set to an
// empty constant string. A non-constant Label expression is accepted
// — the analyzer cannot decide whether a runtime value is non-empty,
// and a false positive on "Label: someVar" would be worse than a
// missed empty value at runtime.
func checkLabel(pass *analysis.Pass, lit *ast.CompositeLit, typeName string) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			// Positional literal — scope limitation on the package.
			return
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Label" {
			continue
		}
		if isEmptyString(pass, kv.Value) {
			pass.ReportRangef(kv.Value,
				"%s.Label is empty — accessibility label required",
				typeName)
		}
		return
	}
	pass.ReportRangef(lit,
		"%s composite literal missing Label — accessibility label required",
		typeName)
}

// isEmptyString returns true when expr resolves to a string constant
// whose value is the empty string.
func isEmptyString(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil {
		return false
	}
	if tv.Value.Kind() != constant.String {
		return false
	}
	return constant.StringVal(tv.Value) == ""
}
