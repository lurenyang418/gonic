package tags

import (
	"fmt"
	"path/filepath"
	"strings"

	"go.senan.xyz/taglib"
)

var _ Reader = TaglibReader{}

type TaglibReader struct{}

func (TaglibReader) CanRead(absPath string) bool {
	switch ext := strings.ToLower(filepath.Ext(absPath)); ext {
	case ".mp3", ".flac", ".aac", ".m4a", ".m4b", ".ogg", ".opus", ".wma", ".wav", ".wv", ".ape":
		return true
	}
	return false
}

func (TaglibReader) Read(absPath string) (Properties, Tags, error) {
	tp, err := taglib.ReadProperties(absPath)
	if err != nil {
		return Properties{}, nil, fmt.Errorf("read properties: %w", err)
	}

	tag, err := taglib.ReadTags(absPath)
	if err != nil {
		return Properties{}, nil, fmt.Errorf("read tags: %w", err)
	}

	return Properties{Length: tp.Length, Bitrate: tp.Bitrate, HasCover: len(tp.Images) > 0}, tag, nil
}

func (TaglibReader) ReadCover(absPath string) ([]byte, error) {
	return taglib.ReadImage(absPath)
}
