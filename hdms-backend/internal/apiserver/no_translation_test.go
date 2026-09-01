package apiserver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The contract rule is that no endpoint varies its response text by the
// caller's language. Reading Accept-Language is the first step of breaking it,
// so it is the thing to catch.
func TestNoHandlerReadsAcceptLanguage(t *testing.T) {
	t.Parallel()

	var offenders []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(source), "Accept-Language") {
			offenders = append(offenders, path)
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders, "an API response must not vary by the caller's language")
}
