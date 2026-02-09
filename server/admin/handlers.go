//nolint:goerr113
package admin

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/nfnt/resize"

	"github.com/lurenyang418/gonic/internal/db"
	"github.com/lurenyang418/gonic/internal/middleware"
	"github.com/lurenyang418/gonic/internal/scanner"
)

func (c *Controller) ServeNotFound(_ *http.Request) *Response {
	return &Response{template: "not_found.tmpl", code: 404}
}

func (c *Controller) ServeLogin(_ *http.Request) *Response {
	return &Response{template: "login.tmpl"}
}

func (c *Controller) ServeHome(r *http.Request) *Response {
	user := r.Context().Value(CtxUser).(*db.User)

	data := &templateData{}
	// stats box
	data.Stats, _ = c.dbc.Stats()
	// lastfm box
	data.RequestRoot = middleware.BaseURL(r)

	// users box
	allUsersQ := c.dbc.DB
	if !user.IsAdmin {
		allUsersQ = allUsersQ.Where("name=?", user.Name)
	}
	allUsersQ.Find(&data.AllUsers)

	// recent folders box
	c.dbc.
		Order("created_at DESC").
		Limit(10).
		Find(&data.RecentFolders)

	data.IsScanning = c.scanner.IsScanning()
	if tStr, _ := c.dbc.GetSetting(db.LastScanTime); tStr != "" {
		i, _ := strconv.ParseInt(tStr, 10, 64)
		data.LastScanTime = time.Unix(i, 0)
	}

	// podcasts box
	c.dbc.Find(&data.Podcasts)

	return &Response{
		template: "home.tmpl",
		data:     data,
	}
}

func (c *Controller) ServeChangeUsername(r *http.Request) *Response {
	user, err := selectedUserIfAdmin(c, r)
	if err != nil {
		return &Response{code: 400, err: err.Error()}
	}
	data := &templateData{}
	data.SelectedUser = user
	return &Response{
		template: "change_username.tmpl",
		data:     data,
	}
}

func (c *Controller) ServeChangeUsernameDo(r *http.Request) *Response {
	user, err := selectedUserIfAdmin(c, r)
	if err != nil {
		return &Response{code: 400, err: err.Error()}
	}
	usernameNew := r.FormValue("username")
	if err := validateUsername(usernameNew); err != nil {
		return &Response{
			redirect: r.Referer(),
			flashW:   []string{err.Error()},
		}
	}
	user.Name = usernameNew
	if err := c.dbc.Save(user).Error; err != nil {
		return &Response{redirect: r.Referer(), flashW: []string{fmt.Sprintf("save username: %v", err)}}
	}
	return &Response{redirect: "/admin/home"}
}

func (c *Controller) ServeChangePassword(r *http.Request) *Response {
	user, err := selectedUserIfAdmin(c, r)
	if err != nil {
		return &Response{code: 400, err: err.Error()}
	}
	data := &templateData{}
	data.SelectedUser = user
	return &Response{
		template: "change_password.tmpl",
		data:     data,
	}
}

func (c *Controller) ServeChangePasswordDo(r *http.Request) *Response {
	user, err := selectedUserIfAdmin(c, r)
	if err != nil {
		return &Response{code: 400, err: err.Error()}
	}
	passwordOne := r.FormValue("password_one")
	passwordTwo := r.FormValue("password_two")
	if err := validatePasswords(passwordOne, passwordTwo); err != nil {
		return &Response{
			redirect: r.Referer(),
			flashW:   []string{err.Error()},
		}
	}
	user.Password = passwordOne
	if err := c.dbc.Save(user).Error; err != nil {
		return &Response{redirect: r.Referer(), flashW: []string{fmt.Sprintf("save user: %v", err)}}
	}
	return &Response{redirect: "/admin/home"}
}

func (c *Controller) ServeChangeAvatar(r *http.Request) *Response {
	user, err := selectedUserIfAdmin(c, r)
	if err != nil {
		return &Response{code: 400, err: err.Error()}
	}
	data := &templateData{}
	data.SelectedUser = user
	return &Response{
		template: "change_avatar.tmpl",
		data:     data,
	}
}

func (c *Controller) ServeChangeAvatarDo(r *http.Request) *Response {
	user, err := selectedUserIfAdmin(c, r)
	if err != nil {
		return &Response{code: 400, err: err.Error()}
	}
	avatar, err := getAvatarFile(r)
	if err != nil {
		return &Response{
			redirect: r.Referer(),
			flashW:   []string{err.Error()},
		}
	}
	user.Avatar = avatar
	if err := c.dbc.Save(user).Error; err != nil {
		return &Response{redirect: r.Referer(), flashW: []string{fmt.Sprintf("save user: %v", err)}}
	}
	return &Response{
		redirect: r.Referer(),
		flashN:   []string{"avatar saved successfully"},
	}
}

func (c *Controller) ServeDeleteAvatarDo(r *http.Request) *Response {
	user, err := selectedUserIfAdmin(c, r)
	if err != nil {
		return &Response{code: 400, err: err.Error()}
	}
	user.Avatar = nil
	if err := c.dbc.Save(user).Error; err != nil {
		return &Response{redirect: r.Referer(), flashW: []string{fmt.Sprintf("save user: %v", err)}}
	}
	return &Response{
		redirect: r.Referer(),
		flashN:   []string{"avatar deleted successfully"},
	}
}

func (c *Controller) ServeDeleteUser(r *http.Request) *Response {
	user, err := selectedUserIfAdmin(c, r)
	if err != nil {
		return &Response{code: 400, err: err.Error()}
	}
	data := &templateData{}
	data.SelectedUser = user
	return &Response{
		template: "delete_user.tmpl",
		data:     data,
	}
}

func (c *Controller) ServeDeleteUserDo(r *http.Request) *Response {
	user, err := selectedUserIfAdmin(c, r)
	if err != nil {
		return &Response{code: 400, err: err.Error()}
	}
	if user.IsAdmin {
		return &Response{
			redirect: "/admin/home",
			flashW:   []string{"can't delete the admin user"},
		}
	}
	if err := c.dbc.Delete(user).Error; err != nil {
		return &Response{redirect: r.Referer(), flashW: []string{fmt.Sprintf("delete user: %v", err)}}
	}
	return &Response{redirect: "/admin/home"}
}

func (c *Controller) ServeCreateUser(_ *http.Request) *Response {
	return &Response{template: "create_user.tmpl"}
}

func (c *Controller) ServeCreateUserDo(r *http.Request) *Response {
	username := r.FormValue("username")
	if err := validateUsername(username); err != nil {
		return &Response{
			redirect: r.Referer(),
			flashW:   []string{err.Error()},
		}
	}
	passwordOne := r.FormValue("password_one")
	passwordTwo := r.FormValue("password_two")
	if err := validatePasswords(passwordOne, passwordTwo); err != nil {
		return &Response{
			redirect: r.Referer(),
			flashW:   []string{err.Error()},
		}
	}
	user := db.User{
		Name:     username,
		Password: passwordOne,
	}
	if err := c.dbc.Create(&user).Error; err != nil {
		return &Response{
			redirect: r.Referer(),
			flashW:   []string{fmt.Sprintf("could not create user %q: %v", username, err)},
		}
	}
	return &Response{redirect: "/admin/home"}
}

func (c *Controller) ServeStartScanIncDo(_ *http.Request) *Response {
	defer doScan(c.scanner, scanner.ScanOptions{})
	return &Response{
		redirect: "/admin/home",
		flashN:   []string{"incremental scan started. refresh for results"},
	}
}

func (c *Controller) ServeStartScanFullDo(_ *http.Request) *Response {
	defer doScan(c.scanner, scanner.ScanOptions{IsFull: true})
	return &Response{
		redirect: "/admin/home",
		flashN:   []string{"full scan started. refresh for results"},
	}
}

func (c *Controller) ServePodcastAddDo(r *http.Request) *Response {
	rssURL := r.FormValue("feed")
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(rssURL)
	if err != nil {
		return &Response{
			redirect: "/admin/home",
			flashW:   []string{fmt.Sprintf("could not create feed: %v", err)},
		}
	}
	if _, err := c.podcasts.AddNewPodcast(rssURL, feed); err != nil {
		return &Response{
			redirect: "/admin/home",
			flashW:   []string{fmt.Sprintf("could not create feed: %v", err)},
		}
	}
	return &Response{
		redirect: "/admin/home",
	}
}

func (c *Controller) ServePodcastDownloadDo(r *http.Request) *Response {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		return &Response{code: 400, err: "please provide a valid podcast id"}
	}
	if err := c.podcasts.DownloadPodcastAll(id); err != nil {
		return &Response{redirect: r.Referer(), flashW: []string{fmt.Sprintf("error downloading: %v", err)}}
	}
	return &Response{
		redirect: "/admin/home",
		flashN:   []string{"started downloading podcast episodes"},
	}
}

func (c *Controller) ServePodcastUpdateDo(r *http.Request) *Response {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		return &Response{code: 400, err: "please provide a valid podcast id"}
	}
	setting := db.PodcastAutoDownload(r.FormValue("setting"))
	var message string
	switch setting {
	case db.PodcastAutoDownloadLatest:
		message = "future podcast episodes will be automatically downloaded"
	case db.PodcastAutoDownloadNone:
		message = "future podcast episodes will not be downloaded"
	default:
		return &Response{code: 400, err: "please provide a valid podcast download type"}
	}
	if err := c.podcasts.SetAutoDownload(id, setting); err != nil {
		return &Response{
			flashW: []string{fmt.Sprintf("could not update auto download setting: %v", err)},
			code:   400,
		}
	}
	return &Response{
		redirect: "/admin/home",
		flashN:   []string{message},
	}
}

func (c *Controller) ServePodcastDeleteDo(r *http.Request) *Response {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		return &Response{code: 400, err: "please provide a valid podcast id"}
	}
	if err := c.podcasts.DeletePodcast(id); err != nil {
		return &Response{redirect: r.Referer(), flashW: []string{fmt.Sprintf("error deleting: %v", err)}}
	}
	return &Response{
		redirect: "/admin/home",
	}
}

func getAvatarFile(r *http.Request) ([]byte, error) {
	err := r.ParseMultipartForm(10 << 20) // keep up to 10MB in memory
	if err != nil {
		return nil, err
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		return nil, fmt.Errorf("read form file: %w", err)
	}
	i, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	resized := resize.Resize(64, 64, i, resize.Lanczos3)
	var buff bytes.Buffer
	if err := jpeg.Encode(&buff, resized, nil); err != nil {
		return nil, err
	}
	return buff.Bytes(), nil
}

func selectedUserIfAdmin(c *Controller, r *http.Request) (*db.User, error) {
	selectedUsername := r.URL.Query().Get("user")
	if selectedUsername == "" {
		return nil, fmt.Errorf("please provide a username")
	}
	user := r.Context().Value(CtxUser).(*db.User)
	if !user.IsAdmin && user.Name != selectedUsername {
		return nil, fmt.Errorf("must be admin to perform actions for other users")
	}
	selectedUser := c.dbc.GetUserByName(selectedUsername)
	return selectedUser, nil
}

func doScan(scanner *scanner.Scanner, opts scanner.ScanOptions) {
	go func() {
		if _, err := scanner.ScanAndClean(opts); err != nil {
			log.Printf("error while scanning: %v\n", err)
		}
	}()
}
