package dgproto_test

import (
	"go/ast"
	"go/build"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const architectureModule = "github.com/datagram-messenger/dgproto-go"

// Inspect build-source files even under inactive platform/tag constraints so an
// optional build cannot introduce a reverse edge. Ignore Go-ignored artifacts;
// the maintained standalone interop program is an explicit fixture exception.
func TestArchitectureBoundaries(t *testing.T) {
	leaves := map[string]bool{"wire": true, "tlv": true, "replay": true, "acceptpolicy": true, "keylog": true}
	standard := make(map[string]bool)
	isStandard := func(path string) bool {
		if ok, found := standard[path]; found {
			return ok
		}
		pkg, err := build.Default.Import(path, "", build.FindOnly)
		ok := err == nil && pkg.Goroot
		standard[path] = ok
		return ok
	}
	check := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(path)
		if entry.IsDir() {
			if architectureIgnoredPath(rel) && rel != "integrationtests/testdata" && rel != "integrationtests/testdata/interop" {
				return filepath.SkipDir
			}
			return nil
		}
		if !architectureSourcePath(rel) {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		expr, err := architectureBuildExpr(file)
		if err != nil {
			t.Fatalf("%s: build constraint: %v", path, err)
		}
		if architectureRequiresTag(expr, "ignore") {
			return nil
		}
		area := "root"
		if strings.HasPrefix(rel, "internal/") {
			area = "leaf"
			parts := strings.Split(rel, "/")
			if len(parts) != 3 || !leaves[parts[1]] {
				t.Errorf("%s: unreviewed internal owner; update the documented DAG deliberately", rel)
			}
		} else if strings.HasPrefix(rel, "integrationtests/") {
			area = "consumer"
			want := "integrationtests"
			if strings.HasPrefix(rel, "integrationtests/api/") {
				want = "dgproto_test"
			}
			// testdata contains standalone public-consumer interop programs, not
			// test packages; still enforce their public-import boundary below.
			if !strings.Contains(rel, "/testdata/") && (file.Name.Name != want || !strings.HasSuffix(rel, "_test.go")) {
				t.Errorf("%s: consumer suites must be external-only tests in package %s", rel, want)
			}
		}
		for _, imp := range file.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Errorf("%s: invalid import: %v", rel, err)
				continue
			}
			if problem := architectureImportProblem(area, rel, name, isStandard(name), leaves); problem != "" {
				t.Errorf("%s: import %q: %s", rel, name, problem)
			}
		}
		return nil
	}
	for _, dir := range []string{"internal", "integrationtests"} {
		if err := filepath.WalkDir(dir, check); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range entries {
		if !architectureSourcePath(path) || strings.HasSuffix(path, "_test.go") {
			continue // White-box root tests are intentionally implementation-adjacent.
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		expr, err := architectureBuildExpr(file)
		if err != nil {
			t.Fatalf("%s: build constraint: %v", path, err)
		}
		if architectureRequiresTag(expr, "ignore") {
			continue
		}
		for _, imp := range file.Imports {
			name, _ := strconv.Unquote(imp.Path.Value)
			if problem := architectureImportProblem("root", path, name, isStandard(name), leaves); problem != "" {
				t.Errorf("%s: import %q: %s", path, name, problem)
			}
			if name == architectureModule+"/internal/keylog" && !architectureRequiresTag(expr, "dgproto_keylog") {
				t.Errorf("%s: optional keylog import must require dgproto_keylog", path)
			}
		}
	}
}

// Go ignores dot/underscore paths and testdata during package discovery.
func architectureIgnoredPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") || part == "testdata" || part == "vendor" {
			return true
		}
	}
	return false
}

func architectureSourcePath(path string) bool {
	path = filepath.ToSlash(path)
	if path == "integrationtests/testdata/interop/main.go" {
		return true
	}
	return strings.HasSuffix(path, ".go") && !architectureIgnoredPath(path)
}

// Read real Go build expressions, not substrings or arbitrary facade filenames.
// Modern constraints take precedence over their legacy +build equivalents.
func architectureBuildExpr(file *ast.File) (constraint.Expr, error) {
	var legacy constraint.Expr
	for _, group := range file.Comments {
		if group.Pos() >= file.Package {
			break
		}
		for _, comment := range group.List {
			line := comment.Text
			if constraint.IsGoBuild(line) {
				return constraint.Parse(line)
			}
			if constraint.IsPlusBuild(line) {
				expr, err := constraint.Parse(line)
				if err != nil {
					return nil, err
				}
				if legacy == nil {
					legacy = expr
				} else {
					legacy = &constraint.AndExpr{X: legacy, Y: expr}
				}
			}
		}
	}
	return legacy, nil
}

// Prove the constraint cannot be satisfied with the required tag disabled.
// Other tags vary independently; this also catches negation and OR bypasses.
func architectureRequiresTag(expr constraint.Expr, required string) bool {
	if expr == nil {
		return false
	}
	var tags []string
	seen := make(map[string]bool)
	var collect func(constraint.Expr)
	collect = func(expr constraint.Expr) {
		switch expr := expr.(type) {
		case *constraint.TagExpr:
			if expr.Tag != required && !seen[expr.Tag] {
				seen[expr.Tag] = true
				tags = append(tags, expr.Tag)
			}
		case *constraint.NotExpr:
			collect(expr.X)
		case *constraint.AndExpr:
			collect(expr.X)
			collect(expr.Y)
		case *constraint.OrExpr:
			collect(expr.X)
			collect(expr.Y)
		}
	}
	collect(expr)
	values := make(map[string]bool)
	var satisfiable func(int) bool
	satisfiable = func(i int) bool {
		if i == len(tags) {
			return expr.Eval(func(tag string) bool { return tag != required && values[tag] })
		}
		values[tags[i]] = false
		if satisfiable(i + 1) {
			return true
		}
		values[tags[i]] = true
		return satisfiable(i + 1)
	}
	return !satisfiable(0)
}

func architectureImportProblem(area, file, path string, standard bool, leaves map[string]bool) string {
	if area == "leaf" && !standard {
		// External tests may import their own leaf, but never root or siblings.
		owner := architectureModule + "/" + filepath.ToSlash(filepath.Dir(file))
		if !strings.HasSuffix(file, "_test.go") || path != owner {
			return "internal leaves may import only stdlib (external tests may import their own leaf)"
		}
	}
	if area == "consumer" && strings.HasPrefix(path, architectureModule+"/") {
		return "consumer tests may import the public root, not repository implementation packages"
	}
	if area == "root" && strings.HasPrefix(path, architectureModule+"/") {
		leaf := strings.TrimPrefix(path, architectureModule+"/internal/")
		if !leaves[leaf] {
			return "root may depend only on reviewed internal leaves"
		}
	}
	return ""
}

func TestArchitectureBuildAndSourceRegressions(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   bool
	}{
		{"", false},
		{"//go:build dgproto_keylog\n\n", true},
		{"//go:build dgproto_keylog && windows\n\n", true},
		{"//go:build dgproto_keylog || windows\n\n", false},
		{"//go:build !dgproto_keylog\n\n", false},
		{"//go:build windows && (dgproto_keylog || linux)\n\n", false},
		{"// +build dgproto_keylog\n\n", true},
		{"// +build dgproto_keylog windows\n\n", false},
		{"//go:build windows\n// +build dgproto_keylog\n\n", false},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "renamed_facade.go", tc.source+"package dgproto\n", parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		expr, err := architectureBuildExpr(file)
		if err != nil {
			t.Fatal(err)
		}
		if got := architectureRequiresTag(expr, "dgproto_keylog"); got != tc.want {
			t.Errorf("%q: requires keylog=%v, want %v", tc.source, got, tc.want)
		}
	}
	for path, want := range map[string]bool{
		"internal/wire/header_linux.go":                true,
		"internal/wire/header_test.go":                 true,
		"internal/wire/_scratch.go":                    false,
		"internal/wire/.scratch.go":                    false,
		"internal/wire/testdata/broken.go":             false,
		"integrationtests/testdata/interop/main.go":    true,
		"integrationtests/testdata/interop/ignored.go": false,
		"integrationtests/vendor/example/broken.go":    false,
		"integrationtests/README.md":                   false,
	} {
		if got := architectureSourcePath(path); got != want {
			t.Errorf("%s: source=%v, want %v", path, got, want)
		}
	}
}

// Synthetic AST/import regressions test the policy without editing the repository
// or relying on file counts, line counts, compiler errors or Go's internal rule.
func TestArchitectureBoundaryRegressions(t *testing.T) {
	leaves := map[string]bool{"wire": true, "keylog": true}
	for _, tc := range []struct {
		name, area, file, source string
		standard, rejected       bool
	}{
		{"stdlib leaf", "leaf", "internal/wire/header.go", `package wire; import "encoding/binary"`, true, false},
		{"reverse edge", "leaf", "internal/wire/header.go", `package wire; import "github.com/datagram-messenger/dgproto-go"`, false, true},
		{"sibling edge", "leaf", "internal/wire/header.go", `package wire; import "github.com/datagram-messenger/dgproto-go/internal/tlv"`, false, true},
		{"own external test", "leaf", "internal/wire/header_test.go", `package wire_test; import "github.com/datagram-messenger/dgproto-go/internal/wire"`, false, false},
		{"sibling test edge", "leaf", "internal/wire/header_test.go", `package wire_test; import "github.com/datagram-messenger/dgproto-go/internal/tlv"`, false, true},
		{"third party leaf", "leaf", "internal/wire/header.go", `package wire; import "example.org/codec"`, false, true},
		{"public consumer", "consumer", "integrationtests/api/header_test.go", `package dgproto_test; import "github.com/datagram-messenger/dgproto-go"`, false, false},
		{"private consumer", "consumer", "integrationtests/api/header_test.go", `package dgproto_test; import "github.com/datagram-messenger/dgproto-go/internal/wire"`, false, true},
		{"known root edge", "root", "header.go", `package dgproto; import "github.com/datagram-messenger/dgproto-go/internal/wire"`, false, false},
		{"new root edge", "root", "header.go", `package dgproto; import "github.com/datagram-messenger/dgproto-go/internal/newowner"`, false, true},
		{"optional facade", "root", "debug_keys.go", `package dgproto; import "github.com/datagram-messenger/dgproto-go/internal/keylog"`, false, false},
		{"third party consumer", "consumer", "integrationtests/api/benchmark_test.go", `package dgproto_test; import "golang.org/x/crypto/chacha20poly1305"`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), tc.file, tc.source, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if imp, ok := n.(*ast.ImportSpec); ok {
					path, _ := strconv.Unquote(imp.Path.Value)
					problem := architectureImportProblem(tc.area, tc.file, path, tc.standard, leaves)
					if (problem != "") != tc.rejected {
						t.Fatalf("rejected=%v, want %v: %s", problem != "", tc.rejected, problem)
					}
				}
				return true
			})
		})
	}
}
