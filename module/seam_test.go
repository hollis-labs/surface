package module_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModulesImportOnlyTheContract keeps the seam: a module under modules/ may import, of
// Surface, only the module package (and its test helper). Go's internal rule already keeps
// core/internal out of reach; this also keeps out the core's public assembly and the app
// runner, so a module never comes to depend on how an app is put together.
func TestModulesImportOnlyTheContract(t *testing.T) {
	const self = "github.com/hollis-labs/surface/"
	allowed := map[string]bool{self + "module": true, self + "module/moduletest": true}
	root := filepath.Join("..", "modules")
	if _, err := os.Stat(root); err != nil {
		t.Skip("no modules directory")
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == "web" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		// A module's own packages (modules/<id>/...) are its own business.
		rel, _ := filepath.Rel(root, path)
		own := self + "modules/" + strings.Split(filepath.ToSlash(rel), "/")[0]
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(p, self) || allowed[p] || p == own || strings.HasPrefix(p, own+"/") {
				continue
			}
			t.Errorf("%s imports %s: a module may import only %smodule", path, p, self)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
