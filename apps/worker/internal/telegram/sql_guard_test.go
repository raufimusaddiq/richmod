package telegram

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestNoSQLStringHoldsMoreThanOneCommand(t *testing.T) {
	statement := regexp.MustCompile(`(?is)^\s*(SELECT|INSERT|UPDATE|DELETE|WITH)\b`)
	second := regexp.MustCompile(`(?is);\s*(SELECT|INSERT|UPDATE|DELETE|WITH|CREATE|ALTER|DROP)\b`)
	quoted := regexp.MustCompile(`'(?:[^']|'')*'`)
	comment := regexp.MustCompile(`--[^\n]*`)
	scanned := 0
	for _, root := range []string{"../..", "../../../api"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil || !statement.MatchString(value) {
					return true
				}
				scanned++
				stripped := comment.ReplaceAllString(quoted.ReplaceAllString(value, "''"), "")
				if second.MatchString(stripped) {
					t.Errorf("%s: SQL string holds more than one command; split it into separate Exec calls in one transaction:\n%s", path, value)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if scanned < 100 {
		t.Fatalf("scanned only %d SQL strings; the walk is not reaching the sources", scanned)
	}
}
