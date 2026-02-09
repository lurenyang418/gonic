// TODO: this package shouldn't really exist. we can usually just attempt our normal filesystem operations
// and handle errors atomically. eg.
// - Safe could instead be try create file, handle error
// - Unique could be try create file, on err create file (1), etc
package utils

import (
	"os"
	"path/filepath"
	"strings"
)

func First(path ...string) (string, error) {
	var err error
	for _, p := range path {
		_, err = os.Stat(p)
		if err == nil {
			return p, nil
		}
	}
	return "", err
}

// HasPrefix checks a path has a prefix, making sure to respect path boundaries. So that /aa & /a does not match, but /a/a & /a does.
func HasPrefix(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, filepath.Clean(prefix)+string(filepath.Separator))
}
