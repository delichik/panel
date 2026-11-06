package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestPackageDependencyBoundaries(t *testing.T) {
	root := repositoryRoot(t)

	checkImports(t, filepath.Join(root, "internal", "platform"), func(file, imported string) {
		if strings.HasPrefix(imported, "panel/internal/modules/") {
			t.Errorf("%s: platform package must not import business module %q", relative(root, file), imported)
		}
	})

	checkImports(t, filepath.Join(root, "internal", "modules"), func(file, imported string) {
		if !strings.Contains(imported, "/internal/modules/") || !strings.Contains(imported, "/store/") {
			return
		}
		currentModule := moduleName(relative(root, file))
		importedModule := moduleName(strings.TrimPrefix(imported, "panel/"))
		if currentModule != "" && importedModule != "" && currentModule != importedModule {
			t.Errorf("%s: module %q must not import store implementation from module %q", relative(root, file), currentModule, importedModule)
		}
	})
}

func checkImports(t *testing.T, root string, check func(file, imported string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			check(path, imported)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func moduleName(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "internal" && parts[i+1] == "modules" {
			return parts[i+2]
		}
	}
	return ""
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func relative(root, path string) string {
	value, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(value)
}
