package telegram

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// User-facing Telegram copy uses the shared Indonesian vocabulary and never asks
// the household to type yes/no. Only string literals are inspected, so comments,
// identifiers, and the English model prompts are unaffected.
var bannedCopy = regexp.MustCompile(`Review Inbox|Balas ya|Balas yes|yes/ya|no/tidak|Sisa salary cycle|sisa salary cycle|Observasi Wealth|review tetap terbuka|Review ini sudah|jangan balas kartu review|untuk household ini|milik household ini|masuk ke Review `)

func TestUserFacingCopyUsesSharedVocabulary(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
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
			if err != nil {
				return true
			}
			if match := bannedCopy.FindString(value); match != "" {
				t.Errorf("%s: user-facing copy contains %q: %q", fset.Position(literal.Pos()), match, value)
			}
			return true
		})
	}
}
