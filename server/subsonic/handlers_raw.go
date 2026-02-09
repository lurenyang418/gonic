package subsonic

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"go.senan.xyz/wrtag/coverparse"

	"github.com/lurenyang418/gonic/internal/db"
	"github.com/lurenyang418/gonic/internal/playlist"
	"github.com/lurenyang418/gonic/pkg/tags"
	"github.com/lurenyang418/gonic/server/subsonic/params"
	"github.com/lurenyang418/gonic/server/subsonic/spec"
	"github.com/lurenyang418/gonic/server/subsonic/specid"
	"github.com/lurenyang418/gonic/server/subsonic/specidpaths"
)

// "raw" handlers are ones that don't always return a spec response.
// it could be a file, stream, etc. so you must either
//   a) write to response writer
//   b) return a non-nil spec.Response
//  _but not both_

const (
	coverDefaultSize = 600
)

func formatToImagingFormat(format string) imaging.Format {
	switch format {
	case "jpeg":
		return imaging.JPEG
	case "png":
		return imaging.PNG
	case "gif":
		return imaging.GIF
	case "bmp":
		return imaging.BMP
	case "tiff":
		return imaging.TIFF
	default:
		return imaging.JPEG
	}
}

func (c *Controller) ServeGetCoverArt(w http.ResponseWriter, r *http.Request) *spec.Response {
	params := r.Context().Value(CtxParams).(params.Params)
	id, err := params.GetID("id")
	if err != nil {
		return spec.NewError(10, "please provide an `id` parameter")
	}

	size := params.GetOrInt("size", coverDefaultSize)
	if size <= 0 {
		return spec.NewError(0, "invalid size")
	}

	reader, err := coverFor(c.dbc, c.playlistStore, c.tagReader, id)
	if err != nil {
		return spec.NewError(10, "couldn't find cover %q: %v", id, err)
	}
	defer reader.Close()

	img, format, err := image.Decode(reader)
	if err != nil {
		return spec.NewError(0, "decode for cover %q: %v", id, err)
	}

	// don't upscale
	minSize := min(size, max(img.Bounds().Dx(), img.Bounds().Dy()))

	if minSize != size {
		img = imaging.Fit(img, minSize, minSize, imaging.Lanczos)
	}

	w.Header().Set("Cache-Control", "public, max-age=1209600")
	w.Header().Set("Content-Type", "image/"+format)

	if err := imaging.Encode(w, img, formatToImagingFormat(format)); err != nil {
		return spec.NewError(0, "encode cover %q: %v", id, err)
	}

	return nil
}

var (
	errCoverNotFound = errors.New("could not find a cover with that id")
	errCoverEmpty    = errors.New("no cover found")
)

// TODO: can we use specidpaths.Locate here?
func coverFor(dbc *db.DB, playlistStore *playlist.Store, tagReader tags.Reader, id specid.ID) (io.ReadCloser, error) {
	switch id.Type {
	case specid.Album:
		return coverForAlbum(dbc, id.Value)
	case specid.Artist:
		return nil, errCoverNotFound
	case specid.Podcast:
		return coverForPodcast(dbc, id.Value)
	case specid.PodcastEpisode:
		return coverForPodcastEpisode(dbc, id.Value)
	case specid.Track:
		return coverForTrack(dbc, tagReader, id.Value)
	case specid.Playlist:
		return coverForPlaylist(playlistStore, id)
	default:
		return nil, errCoverNotFound
	}
}

func coverForAlbum(dbc *db.DB, id int) (*os.File, error) {
	var folder db.Album
	err := dbc.DB.
		Select("id, root_dir, left_path, right_path, cover").
		First(&folder, id).
		Error
	if err != nil {
		return nil, fmt.Errorf("select album: %w", err)
	}
	if folder.Cover == "" {
		return nil, errCoverEmpty
	}
	return os.Open(filepath.Join(folder.RootDir, folder.LeftPath, folder.RightPath, folder.Cover))
}

func coverForPodcast(dbc *db.DB, id int) (*os.File, error) {
	var podcast db.Podcast
	err := dbc.
		First(&podcast, id).
		Error
	if err != nil {
		return nil, fmt.Errorf("select podcast: %w", err)
	}
	if podcast.Image == "" {
		return nil, errCoverEmpty
	}
	return os.Open(filepath.Join(podcast.RootDir, podcast.Image))
}

func coverForPodcastEpisode(dbc *db.DB, id int) (*os.File, error) {
	var pe db.PodcastEpisode
	err := dbc.
		Preload("Podcast").
		First(&pe, id).
		Error
	if err != nil {
		return nil, fmt.Errorf("select episode: %w", err)
	}
	if pe.Podcast == nil || pe.Podcast.Image == "" {
		return nil, errCoverEmpty
	}
	return os.Open(filepath.Join(pe.Podcast.RootDir, pe.Podcast.Image))
}

func coverForTrack(dbc *db.DB, tagReader tags.Reader, id int) (io.ReadCloser, error) {
	var tr db.Track
	err := dbc.
		Preload("Album").
		First(&tr, id).
		Error
	if err != nil {
		return nil, fmt.Errorf("select track: %w", err)
	}

	absPath := tr.AbsPath()

	cover, err := tagReader.ReadCover(absPath)
	if err != nil {
		return nil, fmt.Errorf("read cover: %w", err)
	}
	if len(cover) == 0 {
		return nil, errCoverEmpty
	}

	return io.NopCloser(bytes.NewReader(cover)), nil
}

func coverForPlaylist(playlistStore *playlist.Store, id specid.ID) (*os.File, error) {
	playlistPath := playlistIDDecode(id)
	playlistDir := filepath.Join(playlistStore.BasePath(), filepath.Dir(playlistPath))
	playlistBase := strings.TrimSuffix(filepath.Base(playlistPath), filepath.Ext(playlistPath))

	entries, err := os.ReadDir(playlistDir)
	if err != nil {
		return nil, fmt.Errorf("read playlist dir: %w", err)
	}

	var cover string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		nameBase := strings.TrimSuffix(name, filepath.Ext(name))
		if nameBase == playlistBase && coverparse.IsCover(name) {
			cover = coverparse.BestBetween(cover, name)
		}
	}

	if cover == "" {
		return nil, errCoverEmpty
	}

	return os.Open(filepath.Join(playlistDir, cover))
}

func (c *Controller) ServeStream(w http.ResponseWriter, r *http.Request) *spec.Response {
	params := r.Context().Value(CtxParams).(params.Params)
	id, err := params.GetID("id")
	if err != nil {
		return spec.NewError(10, "please provide an `id` parameter")
	}

	maxBitRate, _ := params.GetInt("maxBitRate")
	format, _ := params.Get("format")
	timeOffset, _ := params.GetInt("timeOffset")

	if maxBitRate > 0 || format != "" || timeOffset > 0 {
		log.Printf("warning: client requested transcoding parameters (maxBitRate=%d, format=%q, timeOffset=%d) but transcoding is disabled. serving raw file", maxBitRate, format, timeOffset)
	}

	file, err := specidpaths.Locate(c.dbc, id)
	if err != nil {
		return spec.NewError(0, "error looking up id %s: %v", id, err)
	}

	_, ok := file.(db.AudioFile)
	if !ok {
		return spec.NewError(0, "type of id does not contain audio")
	}

	http.ServeFile(w, r, file.AbsPath())
	return nil
}

func (c *Controller) ServeGetAvatar(w http.ResponseWriter, r *http.Request) *spec.Response {
	params := r.Context().Value(CtxParams).(params.Params)
	user := r.Context().Value(CtxUser).(*db.User)
	username, err := params.Get("username")
	if err != nil {
		return spec.NewError(10, "please provide an `username` parameter")
	}
	reqUser := c.dbc.GetUserByName(username)
	if (user.ID != reqUser.ID) && !user.IsAdmin {
		return spec.NewError(50, "user not admin")
	}
	http.ServeContent(w, r, "", time.Now(), bytes.NewReader(reqUser.Avatar))
	return nil
}

func streamGetTranscodeMeta(dbc *db.DB, userID int, client string) spec.TranscodeMeta {
	return spec.TranscodeMeta{}
}
