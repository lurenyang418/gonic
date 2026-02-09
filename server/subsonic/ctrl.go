package subsonic

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/lurenyang418/gonic/internal/db"
	"github.com/lurenyang418/gonic/internal/middleware"
	"github.com/lurenyang418/gonic/internal/playlist"
	"github.com/lurenyang418/gonic/internal/podcast"
	"github.com/lurenyang418/gonic/internal/scanner"
	"github.com/lurenyang418/gonic/pkg/tags"
	"github.com/lurenyang418/gonic/server/subsonic/params"
	"github.com/lurenyang418/gonic/server/subsonic/spec"
)

type CtxKey int

const (
	CtxUser CtxKey = iota
	CtxSession
	CtxParams
)

type MusicPath struct {
	Alias, Path string
}

func MusicPaths(paths []MusicPath) []string {
	var r []string
	for _, p := range paths {
		r = append(r, p.Path)
	}
	return r
}

type ProxyPathResolver func(in string) string

type Controller struct {
	*http.ServeMux

	dbc           *db.DB
	scanner       *scanner.Scanner
	musicPaths    []MusicPath
	podcastsPath  string
	playlistStore *playlist.Store
	podcasts      *podcast.Podcasts
	tagReader     tags.Reader

	resolveProxyPath ProxyPathResolver
}

func New(
	dbc *db.DB,
	scannr *scanner.Scanner,
	musicPaths []MusicPath,
	podcastsPath string,
	playlistStore *playlist.Store,
	podcasts *podcast.Podcasts,
	tagReader tags.Reader,
	resolveProxyPath ProxyPathResolver) (*Controller, error) {
	c := Controller{
		ServeMux: http.NewServeMux(),

		dbc:           dbc,
		scanner:       scannr,
		musicPaths:    musicPaths,
		podcastsPath:  podcastsPath,
		playlistStore: playlistStore,
		podcasts:      podcasts,
		tagReader:     tagReader,

		resolveProxyPath: resolveProxyPath,
	}

	chain := middleware.Chain(
		withParams,
		withRequiredParams,
		withUser(dbc),
	)
	chainRaw := middleware.Chain(
		chain,
		slow,
	)

	c.Handle("/getLicense", chain(resp(c.ServeGetLicence)))
	c.Handle("/ping", chain(resp(c.ServePing)))
	c.Handle("/getOpenSubsonicExtensions", chain(resp(c.ServeGetOpenSubsonicExtensions)))

	c.Handle("/getMusicFolders", chain(resp(c.ServeGetMusicFolders)))
	c.Handle("/getScanStatus", chain(resp(c.ServeGetScanStatus)))
	c.Handle("/scrobble", chain(resp(c.ServeScrobble)))
	c.Handle("/startScan", chain(resp(c.ServeStartScan)))
	c.Handle("/getUser", chain(resp(c.ServeGetUser)))
	c.Handle("/getPlaylists", chain(resp(c.ServeGetPlaylists)))
	c.Handle("/getPlaylist", chain(resp(c.ServeGetPlaylist)))
	c.Handle("/createPlaylist", chain(resp(c.ServeCreateOrUpdatePlaylist)))
	c.Handle("/updatePlaylist", chain(resp(c.ServeUpdatePlaylist)))
	c.Handle("/deletePlaylist", chain(resp(c.ServeDeletePlaylist)))
	c.Handle("/savePlayQueue", chain(resp(c.ServeSavePlayQueue)))
	c.Handle("/getPlayQueue", chain(resp(c.ServeGetPlayQueue)))
	c.Handle("/getSong", chain(resp(c.ServeGetSong)))
	c.Handle("/getRandomSongs", chain(resp(c.ServeGetRandomSongs)))
	c.Handle("/getSongsByGenre", chain(resp(c.ServeGetSongsByGenre)))
	c.Handle("/getBookmarks", chain(resp(c.ServeGetBookmarks)))
	c.Handle("/createBookmark", chain(resp(c.ServeCreateBookmark)))
	c.Handle("/deleteBookmark", chain(resp(c.ServeDeleteBookmark)))
	c.Handle("/getTopSongs", chain(resp(c.ServeGetTopSongs)))
	c.Handle("/getSimilarSongs", chain(resp(c.ServeGetSimilarSongs)))
	c.Handle("/getSimilarSongs2", chain(resp(c.ServeGetSimilarSongsTwo)))
	c.Handle("/getLyrics", chain(resp(c.ServeGetLyrics)))
	c.Handle("/getLyricsBySongId", chain(resp(c.ServeGetLyricsBySongID)))

	// raw
	c.Handle("/getCoverArt", chainRaw(respRaw(c.ServeGetCoverArt)))
	c.Handle("/stream", chainRaw(respRaw(c.ServeStream)))
	c.Handle("/download", chainRaw(respRaw(c.ServeStream)))
	c.Handle("/getAvatar", chainRaw(respRaw(c.ServeGetAvatar)))

	// browse by tag
	c.Handle("/getAlbum", chain(resp(c.ServeGetAlbum)))
	c.Handle("/getAlbumList2", chain(resp(c.ServeGetAlbumListTwo)))
	c.Handle("/getArtist", chain(resp(c.ServeGetArtist)))
	c.Handle("/getArtists", chain(resp(c.ServeGetArtists)))
	c.Handle("/search3", chain(resp(c.ServeSearchThree)))
	c.Handle("/getStarred2", chain(resp(c.ServeGetStarredTwo)))
	c.Handle("/getArtistInfo2", chain(resp(c.ServeGetArtistInfoTwo)))
	c.Handle("/getAlbumInfo2", chain(resp(c.ServeGetAlbumInfoTwo)))

	// browse by folder
	c.Handle("/getIndexes", chain(resp(c.ServeGetIndexes)))
	c.Handle("/getMusicDirectory", chain(resp(c.ServeGetMusicDirectory)))
	c.Handle("/getAlbumList", chain(resp(c.ServeGetAlbumList)))
	c.Handle("/search2", chain(resp(c.ServeSearchTwo)))
	c.Handle("/getGenres", chain(resp(c.ServeGetGenres)))
	c.Handle("/getArtistInfo", chain(resp(c.ServeGetArtistInfo)))
	c.Handle("/getStarred", chain(resp(c.ServeGetStarred)))

	// star / rating
	c.Handle("/star", chain(resp(c.ServeStar)))
	c.Handle("/unstar", chain(resp(c.ServeUnstar)))
	c.Handle("/setRating", chain(resp(c.ServeSetRating)))

	// podcasts
	c.Handle("/getPodcasts", chain(resp(c.ServeGetPodcasts)))
	c.Handle("/getNewestPodcasts", chain(resp(c.ServeGetNewestPodcasts)))
	c.Handle("/downloadPodcastEpisode", chain(resp(c.ServeDownloadPodcastEpisode)))
	c.Handle("/createPodcastChannel", chain(resp(c.ServeCreatePodcastChannel)))
	c.Handle("/refreshPodcasts", chain(resp(c.ServeRefreshPodcasts)))
	c.Handle("/deletePodcastChannel", chain(resp(c.ServeDeletePodcastChannel)))
	c.Handle("/deletePodcastEpisode", chain(resp(c.ServeDeletePodcastEpisode)))

	c.Handle("/", chain(resp(c.ServeNotFound)))

	return &c, nil
}

type (
	handlerSubsonic    func(r *http.Request) *spec.Response
	handlerSubsonicRaw func(w http.ResponseWriter, r *http.Request) *spec.Response
)

func resp(h handlerSubsonic) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := writeResp(w, r, h(r)); err != nil {
			log.Printf("error writing subsonic response: %v\n", err)
		}
	})
}

func respRaw(h handlerSubsonicRaw) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := writeResp(w, r, h(w, r)); err != nil {
			log.Printf("error writing raw subsonic response: %v\n", err)
		}
	})
}

func withParams(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := params.New(r)
		withParams := context.WithValue(r.Context(), CtxParams, params)
		next.ServeHTTP(w, r.WithContext(withParams))
	})
}

func withRequiredParams(next http.Handler) http.Handler {
	requiredParameters := []string{
		"u", "c",
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := r.Context().Value(CtxParams).(params.Params)
		for _, req := range requiredParameters {
			if _, err := params.Get(req); err != nil {
				_ = writeResp(w, r, spec.NewError(10, "please provide a %q parameter", req))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func withUser(dbc *db.DB) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			params := r.Context().Value(CtxParams).(params.Params)
			// ignoring errors here, a middleware has already ensured they exist
			username, _ := params.Get("u")
			password, _ := params.Get("p")
			token, _ := params.Get("t")
			salt, _ := params.Get("s")

			passwordAuth := token == "" && salt == ""
			tokenAuth := password == ""
			if tokenAuth == passwordAuth {
				_ = writeResp(w, r, spec.NewError(10,
					"please provide `t` and `s`, or just `p`"))
				return
			}
			user := dbc.GetUserByName(username)
			if user == nil {
				_ = writeResp(w, r, spec.NewError(40,
					"invalid username %q", username))
				return
			}
			var credsOk bool
			if tokenAuth {
				credsOk = checkCredsToken(user.Password, token, salt)
			} else {
				credsOk = checkCredsBasic(user.Password, password)
			}
			if !credsOk {
				_ = writeResp(w, r, spec.NewError(40, "invalid password"))
				return
			}
			withUser := context.WithValue(r.Context(), CtxUser, user)
			next.ServeHTTP(w, r.WithContext(withUser))
		})
	}
}

func slow(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)  //nolint:bodyclose
		_ = rc.SetWriteDeadline(time.Time{}) // set no deadline, since we're probably streaming
		_ = rc.SetReadDeadline(time.Time{})  // set no deadline, since we're probably streaming
		next.ServeHTTP(w, r)
	})
}

func checkCredsToken(password, token, salt string) bool {
	toHash := fmt.Sprintf("%s%s", password, salt)
	hash := md5.Sum([]byte(toHash))
	expToken := hex.EncodeToString(hash[:])
	return token == expToken
}

func checkCredsBasic(password, given string) bool {
	if len(given) >= 4 && given[:4] == "enc:" {
		bytes, _ := hex.DecodeString(given[4:])
		given = string(bytes)
	}
	return password == given
}

func writeResp(w http.ResponseWriter, r *http.Request, resp *spec.Response) error {
	if resp == nil {
		return nil
	}
	if resp.Error != nil {
		log.Printf("subsonic error code %d: %s", resp.Error.Code, resp.Error.Message)
	}

	params := r.Context().Value(CtxParams).(params.Params)
	format, _ := params.Get("f")
	if format != "" && format != "json" {
		log.Printf("unsupported format requested: %s, only json is supported", format)
		errorResp := spec.NewError(0, "unsupported format '%s', only json format is supported", format)
		w.Header().Set("Content-Type", "application/json")
		data, err := json.Marshal(errorResp)
		if err != nil {
			return fmt.Errorf("marshal error response: %w", err)
		}
		_, err = w.Write(data)
		if err != nil {
			return fmt.Errorf("write error response: %w", err)
		}
		return nil
	}

	var res struct {
		*spec.Response `json:"subsonic-response"`
	}
	res.Response = resp

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("marshal to json: %w", err)
	}
	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("write response: %w", err)
	}

	return nil
}
