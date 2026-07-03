package panel

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestAPIRouteManifestUnchanged(t *testing.T) {
	root := panelRepositoryRoot(t)
	files := []string{filepath.Join(root, "internal", "bootstrap", "panel", "app.go")}
	err := filepath.WalkDir(filepath.Join(root, "internal", "modules"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == "routes.go" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	patterns := routePatterns(t, files)
	const wantCount = 134
	const wantHash = "78e43270c6bbd678f285ef6dd51096584c9c04725b34173e040ec6cb25a5556f"
	manifest := strings.Join(patterns, "\n") + "\n"
	gotHash := fmt.Sprintf("%x", sha256.Sum256([]byte(manifest)))
	if len(patterns) != wantCount || gotHash != wantHash {
		t.Fatalf("API route manifest changed: count=%d hash=%s\n%s", len(patterns), gotHash, manifest)
	}
}

func routePatterns(t *testing.T, files []string) []string {
	t.Helper()
	found := map[string]struct{}{}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			pattern, err := strconv.Unquote(literal.Value)
			if err != nil || !strings.HasPrefix(pattern, "GET /api/") &&
				!strings.HasPrefix(pattern, "POST /api/") &&
				!strings.HasPrefix(pattern, "PUT /api/") &&
				!strings.HasPrefix(pattern, "DELETE /api/") &&
				!strings.HasPrefix(pattern, "PATCH /api/") {
				return true
			}
			found[pattern] = struct{}{}
			return true
		})
	}
	patterns := make([]string, 0, len(found))
	for pattern := range found {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns
}

func panelRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
