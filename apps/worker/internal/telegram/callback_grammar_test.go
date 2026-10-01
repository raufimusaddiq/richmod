package telegram

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// Every callback this package can emit or parse must be admitted by the API
// webhook's grammar (reviewdomain.ValidTelegramCallback); otherwise the API
// drops the button tap before the worker ever sees it. The scan reads every
// "review:" and "pending:" string literal in non-test source, so adding a new
// callback without teaching the shared grammar fails here.
func TestEveryCallbackLiteralIsAdmittedByIngress(t *testing.T) {
	samples := reviewdomain.TelegramCallbackSamples()
	hasPrefix := func(prefix string) bool {
		for _, sample := range samples {
			if strings.HasPrefix(sample, prefix) {
				return true
			}
		}
		return false
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil || !(strings.HasPrefix(value, "review:") || strings.HasPrefix(value, "pending:")) {
				return true
			}
			found++
			value = strings.ReplaceAll(value, "%d", "1")
			where := fset.Position(literal.Pos()).String()
			switch {
			case reviewdomain.ValidTelegramCallback(value):
			case value == "review:" || value == "pending:":
				// Generic namespace test in a dispatcher.
			case strings.HasSuffix(value, ":") && hasPrefix(value):
				// A prefix that a valid callback is built from or parsed with.
			default:
				t.Errorf("%s: callback %q is not admitted by the API ingress grammar", where, value)
			}
			return true
		})
	}
	if found < 30 {
		t.Fatalf("scanned only %d callback literals; the scan is not finding the worker's callbacks", found)
	}
}
