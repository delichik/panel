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

func TestAPIRouteManifest(t *testing.T) {
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
	const wantCount = 175
	const wantHash = "a879ba80170d1beb5ac0e3fd19c20a789cc92425db3c9553989c2bbad6129421"
	manifest := strings.Join(patterns, "\n") + "\n"
	gotHash := fmt.Sprintf("%x", sha256.Sum256([]byte(manifest)))
	if len(patterns) != wantCount || gotHash != wantHash {
		t.Fatalf("API route manifest changed: count=%d hash=%s\n%s", len(patterns), gotHash, manifest)
	}
}

// TestPublicArtifactRouteManifest guards the unauthenticated surface the Panel
// exposes outside /api. The API manifest cannot see those routes, so they get
// their own: adding, renaming or removing one must be reviewed here and
// reflected in the acceptance contracts that describe the agent bundle
// download.
func TestPublicArtifactRouteManifest(t *testing.T) {
	root := panelRepositoryRoot(t)
	files := []string{filepath.Join(root, "internal", "modules", "servers", "agent_download.go")}

	patterns := publicRoutePatterns(t, files)
	const wantCount = 2
	const wantHash = "e6010026b226bfdc767151c5e7f78637dd6e99592b2e72caf76fd34f66758cc5"
	manifest := strings.Join(patterns, "\n") + "\n"
	gotHash := fmt.Sprintf("%x", sha256.Sum256([]byte(manifest)))
	if len(patterns) != wantCount || gotHash != wantHash {
		t.Fatalf("public route manifest changed: count=%d hash=%s\n%s", len(patterns), gotHash, manifest)
	}

	// The artifact name must stay a literal in the pattern rather than a path
	// parameter: that is what keeps request data out of the filesystem, and the
	// /agent prefix is what keeps the route out of /api so CDN rules that bypass
	// caching for API traffic do not apply to the bundle.
	const wantArtifactPattern = "GET /agent/{version}/{platform}/panel-agent.gz"
	found := false
	for _, pattern := range patterns {
		if strings.Contains(pattern, "/api/") {
			t.Fatalf("public route %q must stay outside /api", pattern)
		}
		if pattern == wantArtifactPattern {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the fixed-literal artifact pattern %q, got %v", wantArtifactPattern, patterns)
	}
}

// publicRoutePatterns collects the literal route patterns that are not part of
// the /api surface.
func publicRoutePatterns(t *testing.T, files []string) []string {
	t.Helper()
	found := map[string]struct{}{}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			pattern, ok := literalRoutePattern(node)
			if !ok || strings.Contains(pattern, "/api/") {
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

// literalRoutePattern extracts the pattern argument of one mux.Handle or
// mux.HandleFunc call whose first argument is a string literal.
func literalRoutePattern(node ast.Node) (string, bool) {
	call, ok := node.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") {
		return "", false
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	pattern, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return pattern, true
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
