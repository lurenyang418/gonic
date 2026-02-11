//nolint:lll,gocyclo,forbidigo,nilerr,errcheck
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	// avatar encode/decode
	_ "image/gif"
	_ "image/png"

	"github.com/gorilla/securecookie"
	"github.com/sentriz/gormstore"
	"golang.org/x/sync/errgroup"

	"go.senan.xyz/flagconf"

	"github.com/lurenyang418/gonic/internal/db"
	"github.com/lurenyang418/gonic/internal/deps"
	"github.com/lurenyang418/gonic/internal/middleware"
	"github.com/lurenyang418/gonic/internal/playlist"
	"github.com/lurenyang418/gonic/internal/podcast"
	"github.com/lurenyang418/gonic/internal/scanner"
	"github.com/lurenyang418/gonic/pkg/version"
	"github.com/lurenyang418/gonic/server/admin"
	"github.com/lurenyang418/gonic/server/subsonic"
)

func main() {
	confListenAddr := flag.String("listen-addr", "0.0.0.0:4747", "listen address (optional)")

	confPodcastPurgeAgeDays := flag.Uint("podcast-purge-age", 0, "age (in days) to purge podcast episodes if not accessed (optional)")
	confPodcastPath := flag.String("podcast-path", "", "path to podcasts")

	var confMusicPaths pathAliases
	flag.Var(&confMusicPaths, "music-path", "path to music")

	confPlaylistsPath := flag.String("playlists-path", "", "path to your list of new or existing m3u playlists that gonic can manage")

	confDBPath := flag.String("db-path", "gonic.db", "path to database (optional)")

	confScanIntervalMins := flag.Uint("scan-interval", 0, "interval (in minutes) to automatically scan music (optional)")
	confScanAtStart := flag.Bool("scan-at-start-enabled", false, "whether to perform an initial scan at startup (optional)")
	confScanWatcher := flag.Bool("scan-watcher-enabled", false, "whether to watch file system for new music and rescan (optional)")
	confScanEmbeddedCover := flag.Bool("scan-embedded-cover-enabled", true, "whether to scan for embedded covers in audio files (optional)")

	confProxyPrefix := flag.String("proxy-prefix", "", "url path prefix to use if behind proxy. eg '/gonic' (optional)")
	confHTTPLog := flag.Bool("http-log", true, "http request logging (optional)")

	confShowVersion := flag.Bool("version", false, "show gonic version")
	confConfigPath := flag.String("config-path", "", "path to config (optional)")

	confExcludePattern := flag.String("exclude-pattern", "", "regex pattern to exclude files from scan (optional)")

	var confMultiValueGenre, confMultiValueArtist, confMultiValueAlbumArtist multiValueSetting
	flag.Var(&confMultiValueGenre, "multi-value-genre", "setting for multi-valued genre scanning (optional)")
	flag.Var(&confMultiValueArtist, "multi-value-artist", "setting for multi-valued track artist scanning (optional)")
	flag.Var(&confMultiValueAlbumArtist, "multi-value-album-artist", "setting for multi-valued album artist scanning (optional)")

	confLogDB := flag.Bool("log-db", false, "enable database query logging (optional)")

	deprecatedConfGenreSplit := flag.String("genre-split", "", "(deprecated, see multi-value settings)")

	flag.Parse()
	flagconf.ParseEnv()
	flagconf.ParseConfig(*confConfigPath)

	if *confShowVersion {
		fmt.Printf("%s\n", version.Version)
		os.Exit(0)
	}

	if _, err := regexp.Compile(*confExcludePattern); err != nil {
		log.Fatalf("invalid exclude pattern: %v\n", err)
	}

	if len(confMusicPaths) == 0 {
		log.Fatalf("please provide a music directory")
	}

	var err error
	for i, confMusicPath := range confMusicPaths {
		if confMusicPaths[i].path, err = validatePath(confMusicPath.path); err != nil {
			log.Fatalf("checking music dir %q: %v", confMusicPath.path, err)
		}
	}

	if *confPodcastPath, err = validatePath(*confPodcastPath); err != nil {
		log.Fatalf("checking podcast directory: %v", err)
	}
	if *confPlaylistsPath, err = validatePath(*confPlaylistsPath); err != nil {
		log.Fatalf("checking playlist directory: %v", err)
	}

	dbc, err := db.New(*confDBPath, deps.DBDriverOptions(), *confLogDB)
	if err != nil {
		log.Fatalf("error opening database: %v\n", err)
	}
	defer dbc.Close()

	err = dbc.Migrate(db.MigrationContext{
		Production:        true,
		DBPath:            *confDBPath,
		OriginalMusicPath: confMusicPaths[0].path,
		PlaylistsPath:     *confPlaylistsPath,
		PodcastsPath:      *confPodcastPath,
	})
	if err != nil {
		log.Panicf("error migrating database: %v\n", err)
	}

	var musicPaths []subsonic.MusicPath
	for _, pa := range confMusicPaths {
		musicPaths = append(musicPaths, subsonic.MusicPath{Alias: pa.alias, Path: pa.path})
	}

	proxyPrefixExpr := regexp.MustCompile(`^\/*(.*?)\/*$`)
	*confProxyPrefix = proxyPrefixExpr.ReplaceAllString(*confProxyPrefix, `/$1`)

	if *deprecatedConfGenreSplit != "" && *deprecatedConfGenreSplit != "\n" {
		confMultiValueGenre = multiValueSetting{Mode: scanner.Delim, Delim: *deprecatedConfGenreSplit}
		*deprecatedConfGenreSplit = "<deprecated>"
	}
	if confMultiValueArtist.Mode == scanner.None && confMultiValueAlbumArtist.Mode > scanner.None {
		confMultiValueArtist.Mode = confMultiValueAlbumArtist.Mode
		confMultiValueArtist.Delim = confMultiValueAlbumArtist.Delim
	}
	if confMultiValueArtist.Mode != confMultiValueAlbumArtist.Mode {
		log.Panic("differing multi artist and album artist modes have been tested yet. please set them to be the same")
	}

	log.Printf("starting gonic %s\n", version.Version)
	log.Printf("provided config\n")
	flag.VisitAll(func(f *flag.Flag) {
		value := strings.ReplaceAll(f.Value.String(), "\n", "")
		log.Printf("    %-30s %s\n", f.Name, value)
	})

	tagReader := deps.TagReader

	scannr := scanner.New(
		subsonic.MusicPaths(musicPaths),
		dbc,
		map[scanner.Tag]scanner.MultiValueSetting{
			scanner.Genre:       scanner.MultiValueSetting(confMultiValueGenre),
			scanner.Artist:      scanner.MultiValueSetting(confMultiValueArtist),
			scanner.AlbumArtist: scanner.MultiValueSetting(confMultiValueAlbumArtist),
		},
		tagReader,
		*confExcludePattern,
		*confScanEmbeddedCover,
	)
	podcast := podcast.New(dbc, *confPodcastPath, tagReader)

	playlistStore, err := playlist.NewStore(*confPlaylistsPath)
	if err != nil {
		log.Panicf("error creating playlists store: %v", err)
	}

	sessKey, err := dbc.GetSetting("session_key")
	if err != nil {
		log.Panicf("error getting session key: %v\n", err)
	}
	if sessKey == "" {
		sessKey = string(securecookie.GenerateRandomKey(32))
		if err := dbc.SetSetting("session_key", sessKey); err != nil {
			log.Panicf("error setting session key: %v\n", err)
		}
	}
	sessDB := gormstore.New(dbc.DB, []byte(sessKey))
	sessDB.SessionOpts.HttpOnly = true
	sessDB.SessionOpts.SameSite = http.SameSiteLaxMode

	resolveProxyPath := func(in string) string {
		url, _ := url.Parse(in)
		url.Path = path.Join(*confProxyPrefix, url.Path)
		return url.String()
	}

	ctrlAdmin, err := admin.New(dbc, sessDB, scannr, podcast, resolveProxyPath)
	if err != nil {
		log.Panicf("error creating admin controller: %v\n", err)
	}
	ctrlSubsonic, err := subsonic.New(dbc, scannr, musicPaths, *confPodcastPath, playlistStore, podcast, tagReader, resolveProxyPath)
	if err != nil {
		log.Panicf("error creating subsonic controller: %v\n", err)
	}

	chain := middleware.Chain()
	if *confHTTPLog {
		chain = middleware.Chain(middleware.Log)
	}
	chain = middleware.Chain(
		chain,
		middleware.BasicCORS,
	)
	trim := middleware.TrimPathSuffix(".view") // /x.view and /x should match the same

	mux := http.NewServeMux()
	mux.Handle("/admin/", http.StripPrefix("/admin", chain(ctrlAdmin)))
	mux.Handle("/rest/", http.StripPrefix("/rest", chain(trim(ctrlSubsonic))))
	mux.Handle("/ping", chain(middleware.Message("ok")))
	mux.Handle("/", chain(http.RedirectHandler(resolveProxyPath("/admin/home"), http.StatusSeeOther)))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	errgrp, ctx := errgroup.WithContext(ctx)

	errgrp.Go(func() error {
		defer logJob("http")()

		server := &http.Server{
			Addr:         *confListenAddr,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  5 * time.Second,
			Handler:      mux,
		}
		errgrp.Go(func() error {
			<-ctx.Done()
			return server.Shutdown(context.Background())
		})
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})

	errgrp.Go(func() error {
		if !*confScanWatcher {
			return nil
		}

		defer logJob("scan watcher")()

		return scannr.ExecuteWatch(ctx)
	})

	errgrp.Go(func() error {
		defer logJob("session clean")()

		ctxTick(ctx, 10*time.Minute, func() {
			sessDB.Cleanup()
		})
		return nil
	})

	errgrp.Go(func() error {
		defer logJob("podcast refresh")()

		ctxTick(ctx, 1*time.Hour, func() {
			if err := podcast.RefreshPodcasts(); err != nil {
				log.Printf("failed to refresh some feeds: %s", err)
			}
		})
		return nil
	})

	errgrp.Go(func() error {
		defer logJob("podcast download")()

		ctxTick(ctx, 5*time.Second, func() {
			if err := podcast.DownloadTick(); err != nil {
				log.Printf("failed to download podcast: %s", err)
			}
		})
		return nil
	})

	errgrp.Go(func() error {
		if *confPodcastPurgeAgeDays == 0 {
			return nil
		}

		defer logJob("podcast purge")()

		ctxTick(ctx, 24*time.Hour, func() {
			if err := podcast.PurgeOldPodcasts(time.Duration(*confPodcastPurgeAgeDays) * 24 * time.Hour); err != nil {
				log.Printf("error purging old podcasts: %v", err)
			}
		})
		return nil
	})

	errgrp.Go(func() error {
		if *confScanIntervalMins == 0 {
			return nil
		}

		defer logJob("scan timer")()

		ctxTick(ctx, time.Duration(*confScanIntervalMins)*time.Minute, func() {
			if _, err := scannr.ScanAndClean(scanner.ScanOptions{}); err != nil {
				log.Printf("error scanning: %v", err)
			}
		})
		return nil
	})

	errgrp.Go(func() error {
		if !*confScanAtStart {
			return nil
		}

		defer logJob("scan at start")()

		if _, err := scannr.ScanAndClean(scanner.ScanOptions{}); err != nil {
			log.Printf("error scanning on start: %v", err)
		}
		return nil
	})

	if err := errgrp.Wait(); err != nil {
		log.Panic(err)
	}

	fmt.Println("shutdown complete")
}

const pathAliasSep = "->"

type (
	pathAliases []pathAlias
	pathAlias   struct{ alias, path string }
)

func (pa pathAliases) String() string {
	var strs []string
	for _, p := range pa {
		if p.alias != "" {
			strs = append(strs, fmt.Sprintf("%s %s %s", p.alias, pathAliasSep, p.path))
			continue
		}
		strs = append(strs, p.path)
	}
	return strings.Join(strs, ", ")
}

func (pa *pathAliases) Set(value string) error {
	if name, path, ok := strings.Cut(value, pathAliasSep); ok {
		*pa = append(*pa, pathAlias{alias: name, path: path})
		return nil
	}
	*pa = append(*pa, pathAlias{path: value})
	return nil
}

func validatePath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path can't be empty")
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return "", errors.New("path does not exist, please provide one")
	}
	p, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("make absolute: %w", err)
	}
	return p, nil
}

type multiValueSetting scanner.MultiValueSetting

func (mvs multiValueSetting) String() string {
	switch mvs.Mode {
	case scanner.Delim:
		return fmt.Sprintf("delim(%s)", mvs.Delim)
	case scanner.Multi:
		return "multi"
	default:
		return "none"
	}
}

func (mvs *multiValueSetting) Set(value string) error {
	mode, delim, _ := strings.Cut(value, " ")
	switch mode {
	case "delim":
		if delim == "" {
			return fmt.Errorf("no delimiter provided for delimiter mode")
		}
		mvs.Mode = scanner.Delim
		mvs.Delim = delim
	case "multi":
		mvs.Mode = scanner.Multi
	case "none":
	default:
		return fmt.Errorf(`unknown multi value mode %q. should be "none" | "multi" | "delim <delim>"`, mode)
	}
	return nil
}

func logJob(jobName string) func() {
	log.Printf("starting job %q", jobName)
	return func() { log.Printf("stopped job %q", jobName) }
}

func ctxTick(ctx context.Context, interval time.Duration, f func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f()
		}
	}
}
