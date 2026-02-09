package deps

import (
	"net/url"

	// Cgo-free database
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	// Cgo-free tagger
	"github.com/lurenyang418/gonic/pkg/tags"
)

//nolint:gochecknoglobals
var TagReader = tags.TaglibReader{}

// DBDriverOptions returns SQLite DSN options for the ncruces driver
func DBDriverOptions() url.Values {
	return url.Values{
		"_pragma": {
			"busy_timeout(30000)",
			"journal_mode(WAL)",
			"foreign_keys(ON)",
		},
	}
}
