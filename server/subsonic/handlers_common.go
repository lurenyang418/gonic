package subsonic

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/env25/mpdlrc/lrc"
	"github.com/jinzhu/gorm"
	"github.com/lurenyang418/gonic/internal/db"
	"github.com/lurenyang418/gonic/internal/scanner"
	"github.com/lurenyang418/gonic/server/subsonic/params"
	"github.com/lurenyang418/gonic/server/subsonic/spec"
	"github.com/lurenyang418/gonic/server/subsonic/specid"
)

func (c *Controller) ServeGetLicence(_ *http.Request) *spec.Response {
	sub := spec.NewResponse()
	sub.Licence = &spec.Licence{
		Valid: true,
	}
	return sub
}

func (c *Controller) ServePing(_ *http.Request) *spec.Response {
	return spec.NewResponse()
}

func (c *Controller) ServeGetOpenSubsonicExtensions(_ *http.Request) *spec.Response {
	sub := spec.NewResponse()
	sub.OpenSubsonicExtensions = &spec.OpenSubsonicExtensions{
		{Name: "transcodeOffset", Versions: []int{1}},
		{Name: "formPost", Versions: []int{1}},
		{Name: "songLyrics", Versions: []int{1}},
	}
	return sub
}

func (c *Controller) ServeScrobble(r *http.Request) *spec.Response {
	user := r.Context().Value(CtxUser).(*db.User)
	params := r.Context().Value(CtxParams).(params.Params)

	id, err := params.GetID("id")
	if err != nil {
		return spec.NewError(10, "please provide a `id` parameter")
	}

	optStamp := params.GetOrTime("time", time.Now())

	switch id.Type {
	case specid.Track:
		var track db.Track
		if err := c.dbc.Preload("Album").Preload("Album.Artists").First(&track, id.Value).Error; err != nil {
			return spec.NewError(0, "error finding track: %v", err)
		}
		if track.Album == nil {
			return spec.NewError(0, "track has no album %d", track.ID)
		}

		if err := scrobbleStatsUpdateTrack(c.dbc, &track, user.ID, optStamp); err != nil {
			return spec.NewError(0, "error updating stats: %v", err)
		}

	case specid.PodcastEpisode:
		var podcastEpisode db.PodcastEpisode
		if err := c.dbc.Preload("Podcast").First(&podcastEpisode, id.Value).Error; err != nil {
			return spec.NewError(0, "error finding podcast episode: %v", err)
		}

		if err := scrobbleStatsUpdatePodcastEpisode(c.dbc, id.Value); err != nil {
			return spec.NewError(0, "error updating stats: %v", err)
		}
	default:
		return spec.NewError(10, "can't scrobble type %s", id.Type)
	}

	return spec.NewResponse()
}

func (c *Controller) ServeGetMusicFolders(_ *http.Request) *spec.Response {
	sub := spec.NewResponse()
	sub.MusicFolders = &spec.MusicFolders{}
	for i, mp := range c.musicPaths {
		alias := mp.Alias
		if alias == "" {
			alias = filepath.Base(mp.Path)
		}
		sub.MusicFolders.List = append(sub.MusicFolders.List, &spec.MusicFolder{ID: i, Name: alias})
	}
	return sub
}

func (c *Controller) ServeStartScan(r *http.Request) *spec.Response {
	go func() {
		if _, err := c.scanner.ScanAndClean(scanner.ScanOptions{}); err != nil {
			log.Printf("error while scanning: %v\n", err)
		}
	}()
	return c.ServeGetScanStatus(r)
}

func (c *Controller) ServeGetScanStatus(_ *http.Request) *spec.Response {
	var trackCount int
	if err := c.dbc.Model(db.Track{}).Count(&trackCount).Error; err != nil {
		return spec.NewError(0, "error finding track count: %v", err)
	}

	sub := spec.NewResponse()
	sub.ScanStatus = &spec.ScanStatus{
		Scanning: c.scanner.IsScanning(),
		Count:    trackCount,
	}
	return sub
}

func (c *Controller) ServeGetUser(r *http.Request) *spec.Response {
	user := r.Context().Value(CtxUser).(*db.User)

	sub := spec.NewResponse()
	sub.User = &spec.User{
		Username:          user.Name,
		AdminRole:         user.IsAdmin,
		PodcastRole:       c.podcasts != nil,
		DownloadRole:      true,
		ScrobblingEnabled: false,
		Folder:            []int{1},
	}
	return sub
}

func (c *Controller) ServeNotFound(_ *http.Request) *spec.Response {
	return spec.NewError(70, "view not found")
}

func (c *Controller) ServeGetPlayQueue(r *http.Request) *spec.Response {
	params := r.Context().Value(CtxParams).(params.Params)
	user := r.Context().Value(CtxUser).(*db.User)
	var queue db.PlayQueue
	err := c.dbc.
		Where("user_id=?", user.ID).
		Find(&queue).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return spec.NewResponse()
	}
	sub := spec.NewResponse()
	sub.PlayQueue = &spec.PlayQueue{}
	sub.PlayQueue.Username = user.Name
	sub.PlayQueue.Position = queue.Position
	sub.PlayQueue.Current = queue.CurrentSID()
	sub.PlayQueue.Changed = queue.UpdatedAt
	sub.PlayQueue.ChangedBy = queue.ChangedBy

	trackIDs := queue.GetItems()
	sub.PlayQueue.List = make([]*spec.TrackChild, 0, len(trackIDs))

	transcodeMeta := streamGetTranscodeMeta(c.dbc, user.ID, params.GetOr("c", ""))

	for _, id := range trackIDs {
		switch id.Type {
		case specid.Track:
			var track db.Track
			err := c.dbc.
				Where("id=?", id.Value).
				Preload("Album").
				Preload("Artists").
				Preload("TrackStar", "user_id=?", user.ID).
				Preload("TrackRating", "user_id=?", user.ID).
				Find(&track).
				Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return spec.NewError(0, "error finding track")
			}
			if track.ID != 0 {
				tc := spec.NewTCTrackByFolder(&track, track.Album)
				tc.TranscodeMeta = transcodeMeta
				sub.PlayQueue.List = append(sub.PlayQueue.List, tc)
			}
		case specid.PodcastEpisode:
			var pe db.PodcastEpisode
			err := c.dbc.
				Where("id=?", id.Value).
				Find(&pe).
				Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return spec.NewError(0, "error finding podcast episode")
			}
			if pe.ID != 0 {
				tc := spec.NewTCPodcastEpisode(&pe)
				tc.TranscodeMeta = transcodeMeta
				sub.PlayQueue.List = append(sub.PlayQueue.List, tc)
			}
		}
	}
	return sub
}

func (c *Controller) ServeSavePlayQueue(r *http.Request) *spec.Response {
	params := r.Context().Value(CtxParams).(params.Params)
	tracks, err := params.GetIDList("id")
	if err != nil {
		return spec.NewError(10, "please provide some `id` parameters")
	}
	trackIDs := make([]specid.ID, 0, len(tracks))
	for _, id := range tracks {
		if (id.Type == specid.Track) || (id.Type == specid.PodcastEpisode) {
			trackIDs = append(trackIDs, id)
		}
	}
	if len(trackIDs) == 0 {
		return spec.NewError(10, "no track ids provided")
	}
	user := r.Context().Value(CtxUser).(*db.User)
	var queue db.PlayQueue
	c.dbc.Where("user_id=?", user.ID).First(&queue)
	queue.UserID = user.ID
	queue.Current = params.GetOrID("current", specid.ID{}).String()
	queue.Position = params.GetOrInt("position", 0)
	queue.ChangedBy = params.GetOr("c", "") // must exist, middleware checks
	queue.SetItems(trackIDs)
	c.dbc.Save(&queue)
	return spec.NewResponse()
}

func (c *Controller) ServeGetSong(r *http.Request) *spec.Response {
	params := r.Context().Value(CtxParams).(params.Params)
	user := r.Context().Value(CtxUser).(*db.User)
	id, err := params.GetID("id")
	if err != nil {
		return spec.NewError(10, "provide an `id` parameter")
	}
	var track db.Track
	err = c.dbc.
		Where("id=?", id.Value).
		Preload("Album").
		Preload("Album.Artists").
		Preload("Artists").
		Preload("TrackStar", "user_id=?", user.ID).
		Preload("TrackRating", "user_id=?", user.ID).
		First(&track).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return spec.NewError(70, "couldn't find a track with that id")
	}

	transcodeMeta := streamGetTranscodeMeta(c.dbc, user.ID, params.GetOr("c", ""))

	sub := spec.NewResponse()
	sub.Track = spec.NewTrackByTags(&track, track.Album)

	sub.Track.TranscodeMeta = transcodeMeta

	return sub
}

func (c *Controller) ServeGetRandomSongs(r *http.Request) *spec.Response {
	params := r.Context().Value(CtxParams).(params.Params)
	user := r.Context().Value(CtxUser).(*db.User)
	var tracks []*db.Track
	q := c.dbc.DB.
		Limit(params.GetOrInt("size", 10)).
		Preload("Album").
		Preload("Album.Artists").
		Preload("Artists").
		Preload("TrackStar", "user_id=?", user.ID).
		Preload("TrackRating", "user_id=?", user.ID).
		Joins("JOIN albums ON tracks.album_id=albums.id").
		Order(gorm.Expr("random()"))
	if year, err := params.GetInt("fromYear"); err == nil {
		q = q.Where("albums.tag_year >= ?", year)
	}
	if year, err := params.GetInt("toYear"); err == nil {
		q = q.Where("albums.tag_year <= ?", year)
	}
	if genre, err := params.Get("genre"); err == nil {
		q = q.Joins("JOIN track_genres ON track_genres.track_id=tracks.id")
		q = q.Joins("JOIN genres ON genres.id=track_genres.genre_id AND genres.name=?", genre)
	}
	if m := getMusicFolder(c.musicPaths, params); m != "" {
		q = q.Where("albums.root_dir=?", m)
	}
	if err := q.Find(&tracks).Error; err != nil {
		return spec.NewError(10, "get random songs: %v", err)
	}
	sub := spec.NewResponse()
	sub.RandomTracks = &spec.RandomTracks{}
	sub.RandomTracks.List = make([]*spec.TrackChild, len(tracks))

	transcodeMeta := streamGetTranscodeMeta(c.dbc, user.ID, params.GetOr("c", ""))

	for i, track := range tracks {
		sub.RandomTracks.List[i] = spec.NewTrackByTags(track, track.Album)
		sub.RandomTracks.List[i].TranscodeMeta = transcodeMeta
	}
	return sub
}

func (c *Controller) ServeGetLyrics(r *http.Request) *spec.Response {
	params := r.Context().Value(CtxParams).(params.Params)
	artist, _ := params.Get("artist")
	title, _ := params.Get("title")

	var track db.Track
	err := c.dbc.
		Preload("Album").
		Joins("JOIN track_artists ON track_artists.track_id = tracks.id").
		Joins("JOIN artists ON artists.id = track_artists.artist_id").
		Where("tracks.tag_title LIKE ? AND artists.name LIKE ?", title, artist).
		First(&track).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return spec.NewError(70, "couldn't find a track with that id")
	}
	if err != nil {
		return spec.NewError(0, "lyrics: %v", err)
	}

	sub := spec.NewResponse()

	sub.Lyrics = &spec.Lyrics{
		Artist: track.TagTrackArtist,
		Title:  track.TagTitle,
		Value:  "",
	}

	lyrics, err := lyricsAll(track)
	if err != nil {
		return spec.NewError(0, "get lyrics: %v", err)
	}
	if len(lyrics) == 0 {
		return sub
	}

	// prefer unsynced for this endpoint
	slices.SortFunc(lyrics, func(a, b *spec.StructuredLyrics) int {
		if a.Synced {
			return +1
		}
		return -1
	})

	lyric := lyrics[0]

	var lines []string
	for _, l := range lyric.Lines {
		lines = append(lines, l.Value)
	}

	sub.Lyrics.Value = strings.Join(lines, "\n")

	return sub
}

func (c *Controller) ServeGetLyricsBySongID(r *http.Request) *spec.Response {
	params := r.Context().Value(CtxParams).(params.Params)
	id, err := params.GetID("id")
	if err != nil {
		return spec.NewError(10, "provide an `id` parameter")
	}

	var track db.Track
	q := c.dbc.
		Preload("Album").
		Preload("Album.Artists").
		Preload("Artists").
		Where("id=?", id.Value).
		First(&track)
	if err := q.Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return spec.NewError(70, "couldn't find a track with that id")
		} else {
			return spec.NewError(0, "lyrics: %v", err)
		}
	}

	sub := spec.NewResponse()
	sub.LyricsList = &spec.LyricsList{
		StructuredLyrics: []*spec.StructuredLyrics{},
	}

	structuredLyrics, err := lyricsAll(track)
	if err != nil {
		return spec.NewError(0, "get lyrics: %v", err)
	}

	sub.LyricsList.StructuredLyrics = structuredLyrics

	return sub
}

func lyricsAll(track db.Track) ([]*spec.StructuredLyrics, error) {
	var r []*spec.StructuredLyrics

	fromTags, err := lyricsFromTags(track)
	if err != nil {
		return nil, fmt.Errorf("from tags: %w", err)
	}
	if fromTags != nil {
		r = append(r, fromTags)
	}

	fromFile, err := lyricsFromFile(track)
	if err != nil {
		return nil, fmt.Errorf("from file: %w", err)
	}
	if fromFile != nil {
		r = append(r, fromFile)
	}

	fromFileUnsynced, err := lyricsFromFileUnsynced(track)
	if err != nil {
		return nil, fmt.Errorf("from file unsynced: %w", err)
	}
	if fromFileUnsynced != nil {
		r = append(r, fromFileUnsynced)
	}

	return r, nil
}

func lyricsFromTags(track db.Track) (*spec.StructuredLyrics, error) {
	if track.TagLyrics == "" {
		return nil, nil
	}

	r := spec.StructuredLyrics{
		Lang:          "xxx",
		DisplayArtist: track.TagTrackArtist,
		DisplayTitle:  track.TagTitle,
	}

	times, lrc, err := lrc.ParseString(track.TagLyrics)
	if err != nil {
		return nil, err
	}

	if len(times) > 0 {
		lines := make([]spec.Lyric, len(times))
		for i, time := range times {
			start := time.Milliseconds()
			lines[i] = spec.Lyric{
				Start: &start,
				Value: strings.TrimSpace(lrc[i]),
			}
		}

		r.Synced = true
		r.Lines = lines

		return &r, nil
	}

	rawLines := strings.Split(track.TagLyrics, "\n")

	lines := make([]spec.Lyric, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, spec.Lyric{
			Value: strings.TrimSpace(line),
		})
	}

	r.Lines = lines

	return &r, nil
}

func lyricsFromFile(track db.Track) (*spec.StructuredLyrics, error) {
	filePath := track.AbsPath()
	fileDir := filepath.Dir(filePath)
	fileName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	lrcContent, err := os.ReadFile(filepath.Join(fileDir, fileName+".lrc"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	times, lrc, err := lrc.Parse(lrcContent)
	if err != nil {
		return nil, err
	}

	lines := make([]spec.Lyric, len(times))
	for i, time := range times {
		start := time.Milliseconds()
		lines[i] = spec.Lyric{
			Start: &start,
			Value: strings.TrimSpace(lrc[i]),
		}
	}

	r := spec.StructuredLyrics{
		Lang:          "xxx",
		Synced:        true,
		Lines:         lines,
		DisplayArtist: track.TagTrackArtist,
		DisplayTitle:  track.TagTitle,
	}

	return &r, nil
}

func lyricsFromFileUnsynced(track db.Track) (*spec.StructuredLyrics, error) {
	filePath := track.AbsPath()
	fileDir := filepath.Dir(filePath)
	fileName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	text, err := os.ReadFile(filepath.Join(fileDir, fileName+".txt"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	r := spec.StructuredLyrics{
		Lang:          "xxx",
		DisplayArtist: track.TagTrackArtist,
		DisplayTitle:  track.TagTitle,
	}

	rawLines := strings.Split(string(text), "\n")

	lines := make([]spec.Lyric, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, spec.Lyric{
			Value: strings.TrimSpace(line),
		})
	}

	r.Lines = lines

	return &r, nil
}

func scrobbleStatsUpdateTrack(dbc *db.DB, track *db.Track, userID int, playTime time.Time) error {
	var play db.Play
	if err := dbc.Where("album_id=? AND user_id=?", track.AlbumID, userID).First(&play).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("find stat: %w", err)
	}

	play.AlbumID = track.AlbumID
	play.UserID = userID
	play.Count++ // for getAlbumList?type=frequent
	play.Length += track.Length
	if playTime.After(play.Time) {
		play.Time = playTime // for getAlbumList?type=recent
	}

	if err := dbc.Save(&play).Error; err != nil {
		return fmt.Errorf("save stat: %w", err)
	}
	return nil
}

func scrobbleStatsUpdatePodcastEpisode(dbc *db.DB, peID int) error {
	var pe db.PodcastEpisode
	if err := dbc.Where("id=?", peID).First(&pe).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("find podcast episode: %w", err)
	}

	pe.ModifiedAt = time.Now()

	if err := dbc.Save(&pe).Error; err != nil {
		return fmt.Errorf("save podcast episode: %w", err)
	}
	return nil
}

func getMusicFolder(musicPaths []MusicPath, p params.Params) string {
	idx, err := p.GetInt("musicFolderId")
	if err != nil {
		return ""
	}
	if idx < 0 || idx >= len(musicPaths) {
		return os.DevNull
	}
	return musicPaths[idx].Path
}

func lowerUDecOrHash(in string) string {
	inRunes := []rune(in)
	if len(inRunes) == 0 {
		return ""
	}
	lower := unicode.ToLower(inRunes[0])
	if !unicode.IsLetter(lower) {
		return "#"
	}
	return string(lower)
}
