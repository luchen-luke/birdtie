package postgres

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// UNIT_STATIC: inspects the real query literal; it does not run SQL/native ACLs.
func TestConnectionRequestChineseFallbackUnitStatic(t *testing.T) {
	source, err := os.ReadFile("connections.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "connections.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	var query string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "ListRequests" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || method.Sel.Name != "Query" {
				return true
			}
			pool, ok := method.X.(*ast.SelectorExpr)
			if !ok || pool.Sel.Name != "pool" {
				return true
			}
			receiver, ok := pool.X.(*ast.Ident)
			if !ok || receiver.Name != "s" {
				return true
			}
			literal, ok := call.Args[1].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				t.Fatal("actual Request query no longer a literal")
			}
			if query != "" {
				t.Fatal("ambiguous actual Request query")
			}
			query, err = strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			return false
		})
	}
	if strings.Count(query, "ELSE '账号暂不可用' END") != 1 || strings.Contains(query, "Unavailable account") {
		t.Fatal("UNIT_STATIC: actual ListRequests system fallback must be Chinese")
	}
	// Baseline query SHA, with only the one fallback substituted. All original
	// fields, user names, profile/block predicates, note hiding and LIMIT remain.
	original := strings.Replace(query, "账号暂不可用", "Unavailable account", 1)
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(original))); got != "fe248d6d8ab5f4ec502da4a96844f0688364e256706b4cb0f1dd244de7dbbf96" {
		t.Fatal("UNIT_STATIC: original Request query changed beyond system fallback")
	}
}
