# Gonic - Subsonic Server Implementation

A free-software [Subsonic](http://www.subsonic.org/) server API implementation in Go.

## Current Status

This is a modified version of gonic with the following changes:

### Removed Features

- **Audio Transcoding**: The transcoding functionality has been completely removed. The server now serves audio files in their original format only.
- **FFmpeg Integration**: All FFmpeg-based transcoding, caching, and related features have been removed.
- **Tag Processing Utilities**: Some tag processing helper functions have been removed.

### Supported Subsonic API Endpoints

#### System

- `ping` - Server health check
- `getLicense` - License information (always returns valid)
- `getOpenSubsonicExtensions` - OpenSubsonic extension support
- `getMusicFolders` - List available music folders
- `getScanStatus` - Get scan status
- `startScan` - Start library scan
- `getUser` - Get user information
- `scrobble` - Scrobble playback

#### Browsing by Folder

- `getIndexes` - Get music folder indexes
- `getMusicDirectory` - Get directory contents
- `getAlbumList` - Get album list (by folder)
- `search2` - Search (by folder)
- `getStarred` - Get starred items (by folder)
- `getArtistInfo` - Get artist information (placeholder)

#### Browsing by Tags

- `getArtists` - Get all artists
- `getArtist` - Get artist details
- `getAlbum` - Get album details
- `getAlbumList2` - Get album list (by tags)
- `search3` - Search (by tags)
- `getStarred2` - Get starred items (by tags)
- `getArtistInfo2` - Get artist information
- `getAlbumInfo2` - Get album information
- `getGenres` - Get all genres
- `getSongsByGenre` - Get songs by genre

#### Album/Song Lists

- `getSong` - Get song details
- `getRandomSongs` - Get random songs
- `getTopSongs` - Get top songs by artist
- `getSimilarSongs` - Get similar songs (v1)
- `getSimilarSongs2` - Get similar songs (v2)

#### Media Annotation

- `star` - Star items
- `unstar` - Unstar items
- `setRating` - Set rating (1-5)

#### Playlists

- `getPlaylists` - Get all playlists
- `getPlaylist` - Get playlist details
- `createPlaylist` - Create playlist
- `updatePlaylist` - Update playlist
- `deletePlaylist` - Delete playlist

#### Podcasts

- `getPodcasts` - Get podcasts
- `getNewestPodcasts` - Get newest podcast episodes
- `downloadPodcastEpisode` - Download podcast episode
- `createPodcastChannel` - Create podcast channel (admin only)
- `refreshPodcasts` - Refresh podcasts (admin only)
- `deletePodcastChannel` - Delete podcast channel (admin only)
- `deletePodcastEpisode` - Delete podcast episode (admin only)

#### Bookmarks

- `getBookmarks` - Get bookmarks
- `createBookmark` - Create bookmark
- `deleteBookmark` - Delete bookmark

#### Lyrics

- `getLyrics` - Get lyrics (by artist/title)
- `getLyricsBySongId` - Get lyrics (by song ID)

#### Media Retrieval

- `stream` - Stream audio file (no transcoding)
- `download` - Download audio file
- `getCoverArt` - Get cover art
- `getAvatar` - Get user avatar

#### Play Queue

- `getPlayQueue` - Get play queue
- `savePlayQueue` - Save play queue

### Unsupported Subsonic API Endpoints

The following Subsonic API endpoints are **NOT** implemented:

#### User Management

- `createUser` - Create user
- `updateUser` - Update user
- `deleteUser` - Delete user
- `changePassword` - Change password
- `getUsers` - Get all users

#### Media Library Management

- `getShares` - Get shares
- `createShare` - Create share
- `updateShare` - Update share
- `deleteShare` - Delete share

#### Chat

- `getChatMessages` - Get chat messages
- `addChatMessage` - Add chat message

#### Jukebox

- `jukeboxControl` - Jukebox control (removed along with transcoding)

#### Internet Radio

- `getInternetRadioStations` - Get internet radio stations
- `createInternetRadioStation` - Create internet radio station
- `updateInternetRadioStation` - Update internet radio station
- `deleteInternetRadioStation` - Delete internet radio station

#### Video

- `getVideoInfo` - Get video information (video support removed)

#### Advanced Search

- `search` - Original search endpoint (replaced by search2/search3)

#### Other

- `getLyrics` - Original lyrics endpoint (replaced by getLyricsBySongId)
- `getAvatar` - Avatar endpoint (limited implementation)

### OpenSubsonic Extensions

The following OpenSubsonic extensions are supported:

- `transcodeOffset` v1 - Transcode offset support (transcoding removed, but extension is advertised)
- `formPost` v1 - Form POST support
- `songLyrics` v1 - Structured lyrics support

### Response Format

The server supports **JSON format only**:

- `json` - JSON response (default and only format)

All API responses are returned in JSON format with `Content-Type: application/json` header.

### Authentication

The server supports both authentication methods:

- Password authentication (`p` parameter)
- Token authentication (`t` and `s` parameters)

## Installation

The default login is **admin**/**admin**. Password can be changed from the web interface.

### From Source

```bash
go build ./cmd/gonic
./gonic -music-path /path/to/music
```

### With Docker

```bash
docker run -d \
  -p 4747:4747 \
  -v /path/to/music:/music \
  -v /path/to/data:/data \
  lurenyang418/gonic
```

## Configuration Options

| Environment Variable | Command Line Arg | Description |
| ------------------- | ---------------- | ----------- |
| `GONIC_MUSIC_PATH` | `-music-path` | Path to your music collection |
| `GONIC_PODCAST_PATH` | `-podcast-path` | Path to podcasts directory |
| `GONIC_PLAYLISTS_PATH` | `-playlists-path` | Path to playlists directory |
| `GONIC_DB_PATH` | `-db-path` | Path to database file (optional) |
| `GONIC_HTTP_LOG` | `-http-log` | HTTP request logging (default: enabled) |
| `GONIC_LISTEN_ADDR` | `-listen-addr` | Host and port to listen on (default: `0.0.0.0:4747`) |
| `GONIC_PROXY_PREFIX` | `-proxy-prefix` | URL path prefix for reverse proxy (optional) |
| `GONIC_SCAN_INTERVAL` | `-scan-interval` | Scan interval in minutes (optional) |
| `GONIC_SCAN_AT_START_ENABLED` | `-scan-at-start-enabled` | Perform initial scan at startup (optional) |
| `GONIC_SCAN_WATCHER_ENABLED` | `-scan-watcher-enabled` | Watch filesystem for changes (optional) |
| `GONIC_SCAN_EMBEDDED_COVER_ENABLED` | `-scan-embedded-cover-enabled` | Scan for embedded covers (default: `true`) |
| `GONIC_PODCAST_PURGE_AGE` | `-podcast-purge-age` | Age in days to purge podcast episodes (optional) |
| `GONIC_EXCLUDE_PATTERN` | `-exclude-pattern` | Regex pattern to exclude files (optional) |
| `GONIC_MULTI_VALUE_GENRE` | `-multi-value-genre` | Multi-value genre tag mode (optional) |
| `GONIC_MULTI_VALUE_ARTIST` | `-multi-value-artist` | Multi-value artist tag mode (optional) |
| `GONIC_MULTI_VALUE_ALBUM_ARTIST` | `-multi-value-album-artist` | Multi-value album artist tag mode (optional) |

## Important Notes

### No Transcoding

This version does **NOT** support audio transcoding. All audio files are served in their original format. If you need transcoding, please use the original gonic project.

### Directory Structure

When browsing by folder, the following rules apply:

- Files from the same album must be in the same folder
- All files in a folder must be from the same album

### Multi-Value Tags

Gonic supports multi-value tags for genres, artists, and album artists. Available modes:

- `multi` - Look for multi-value fields in metadata
- `delim <delim>` - Split on delimiter (e.g., `delim ;`)
- `none` (default) - No multi-value processing

## Tested Clients

This implementation has been tested with:

- Airsonic-refix
- Symfonium
- DSub
- Jamstash
- Subsonic.el
- Sublime Music
- Soundwaves
- STMP
- Termsonic
- Tempus
- Strawberry
- Ultrasonic

## License

See LICENSE file for details.

## Original Project

This is a modified version of the original [gonic](https://github.com/sentriz/gonic) project.
