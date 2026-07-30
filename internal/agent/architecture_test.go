package agent_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelVisibleToolsUseOnlyOpenAPIBusinessBoundary(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("tools", "*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, path := range files {
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		require.NoError(t, parseErr)
		for _, spec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			require.NoError(t, unquoteErr)
			for _, forbidden := range []string{
				"/internal/store",
				"/internal/application",
				"/internal/api/v1",
			} {
				if forbidden == "/internal/api/v1" &&
					strings.Contains(importPath, "/internal/api/v1generated") {
					continue
				}
				require.NotContains(t, importPath, forbidden,
					"model-visible tool %s bypasses the generated OpenAPI boundary", path)
			}
		}
	}
}
