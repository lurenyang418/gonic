package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFirst(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	name := filepath.Join(base, "test")
	_, err := os.Create(name)
	require.NoError(t, err)

	p := func(name string) string {
		return filepath.Join(base, name)
	}

	r, err := First(p("one"), p("two"), p("test"), p("four"))
	require.NoError(t, err)
	require.Equal(t, p("test"), r)
}
