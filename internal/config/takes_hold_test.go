package config_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/config"
)

// takesHoldPrefix names the test that shows a setting takes hold when its
// declaration says: an immediate one changes what is observable right after a
// reload, a next-use one changes what the next use does, and a startup one
// changed by a reload is still the old value in the next account (#239).
const takesHoldPrefix = "TestTakesHold_"

// takesHoldName is the test a setting's key asks for.
func takesHoldName(key string) string {
	return takesHoldPrefix + strings.ReplaceAll(key, ".", "_")
}

// Every declared setting has a test that shows it takes hold when it says it
// does. The test lives wherever the effect can be seen - the interface, the
// owner, the host - so this finds it by name across the module rather than in a
// table here: a table would be one more list to keep in step with the registry,
// and the registry is what a new setting is added to.
func TestRegistry_EverySettingHasATestThatItTakesHold(t *testing.T) {
	found := takesHoldTests(t, filepath.Join("..", ".."))

	for _, e := range config.Settings() {
		name := takesHoldName(e.Key)
		assert.Truef(t, found[name], "%s is declared %s, and no test shows it: write %s next to where its effect is seen",
			e.Key, e.Applies, name)
		delete(found, name)
	}
	assert.Empty(t, found, "a takes-hold test names no declared setting")
}

// takesHoldTests collects the takes-hold tests declared anywhere under root.
func takesHoldTests(t *testing.T, root string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, takesHoldPrefix) {
				found[fn.Name.Name] = true
			}
		}
		return nil
	})
	require.NoError(t, err)
	return found
}
