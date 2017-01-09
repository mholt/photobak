package googlephotos

import (
	"encoding/gob"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/mholt/photobak"
)

const (
	name  = "googlephotos"
	title = "Google Photos"
)

func init() {
	var accounts photobak.StringFlagList
	flag.Var(&accounts, name, "Add a "+title+" account to the repository")

	photobak.RegisterProvider(photobak.Provider{
		Name:        name,
		Title:       title,
		Accounts:    func() []string { return accounts },
		Credentials: getToken,
		NewClient:   newClient,
	})

	gob.Register(Entry{})
}

// Client acts as a client to the Picasa Web Albums
// API (which has since been monkey-patched to work
// with Google Photos but it's all we've got for
// now). It requires an OAuth2-authenticated
// http.Client in order to function properly.
type Client struct {
	HTTPClient *http.Client
}

// Name returns "googlephotos".
func (c *Client) Name() string {
	return "googlephotos"
}

func (c *Client) ListCollections() ([]photobak.Collection, error) {
	// the picasa web album API docs say the default "kind" parameter
	// value is "album" which is what we want, so we don't bother to
	// specify it here.
	url := "https://picasaweb.google.com/data/feed/api/user/default"
	max := 0 // TODO: Temporary
	if max > 0 {
		url += fmt.Sprintf("?max-results=%d", max)
	}
	data, err := c.getFeed(url)
	if err != nil {
		return nil, err
	}
	// fmt.Println("ALBUM:", string(data))

	var results Atom
	err = xml.Unmarshal(data, &results)
	if err != nil {
		return nil, err
	}

	albums := make([]photobak.Collection, len(results.Entries))
	for i := range results.Entries {
		results.Entries[i].Title = sanitizeFilename(results.Entries[i].Title)
		albums[i] = results.Entries[i]
	}

	return albums, nil
}

func (c *Client) ListCollectionItems(col photobak.Collection, itemChan chan photobak.Item) error {
	defer close(itemChan)
	url := "https://picasaweb.google.com/data/feed/api/user/default/albumid/" + col.CollectionID()
	return c.listAllPhotos(url, itemChan)
}

func (c *Client) listAllPhotos(baseURL string, itemChan chan photobak.Item) error {
	var page Atom
	var err error

	start := 1
	count := 0
	max := 0 // TODO: temporary

	// we can't rely on NumPhotos in an album to be correct,
	// and the number of photos can change while download is
	// happening; so just keep downloading until no results.
	// (the i == 0 condition ensures we run at least once.)
	for i := 0; i == 0 || len(page.Entries) > 0; i++ {
		if max > 0 && count >= max {
			break
		}

		page, err = c.listPhotosPage(baseURL, start, max-count)
		if err != nil {
			return err
		}

		for _, entry := range page.Entries {
			itemChan <- entry
		}

		start += len(page.Entries)
		count += len(page.Entries)
	}

	return nil
}

func (c *Client) DownloadItemInto(item photobak.Item, w io.Writer) error {
	gpItem, ok := item.(Entry)
	if !ok {
		return fmt.Errorf("item is not a Google Photos entry")
	}

	// if a video, and video is still processing, we can't download it yet
	if gpItem.VideoStatus != "" &&
		gpItem.VideoStatus != "ready" &&
		gpItem.VideoStatus != "final" {
		return fmt.Errorf("item is a video and is still being processed (status: %v), try again later", gpItem.VideoStatus)
	}

	url, err := getBestDownloadURL(gpItem)
	if err != nil {
		return fmt.Errorf("identifying the best download URL: %v", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(w, resp.Body)

	return err
}

// getBestDownloadURL gets the URL to the highest-resolution
// non-Flash video, if possible. If the entry is for a photo,
// there won't be a video of it, in which case we just download
// whatever there is at the highest resolution.
func getBestDownloadURL(e Entry) (string, error) {
	var highestRes int
	var bestURL string

	if e.Media != nil {
		// prefer videos that aren't flash
		for _, media := range e.Media.Content {
			res := media.Width * media.Height
			if res > highestRes &&
				media.Medium == "video" &&
				!strings.Contains(media.Type, "flash") {
				highestRes = res
				bestURL = media.URL
			}
		}
		if bestURL == "" {
			// otherwise, prefer the largest of anything we can find
			highestRes = 0
			for _, media := range e.Media.Content {
				res := media.Width * media.Height
				if res > highestRes {
					highestRes = res
					bestURL = media.URL
				}
			}
		}
	}

	if bestURL == "" && e.Content != nil {
		// okaaaaay, well, this value has worked well in the past
		// for photos... sooooo... give it a shot, I guess.
		bestURL = e.Content.URL
	}

	if bestURL == "" {
		// i give up.
		return "", fmt.Errorf("no satisfactory media content found")
	}

	return bestURL, nil
}

// func (c *Client) ListRecentPhotos(max int) ([]Entry, error) {
// 	// Maximum start-index: 1000.
// 	// For some reason, this API endpoint ONLY works if
// 	// max-results is specified, and since we cannot get
// 	// more than 1000 entries, that is what we default to.
// 	// This endpoint does not expose EXIF data and the URLs
// 	// provided do not include GPS data in the embedded EXIF
// 	// data. Seems this endpoint is intended for use by
//	// public consumption as a summary...
// 	if max <= 0 || max > 1000 {
// 		max = 1000
// 	}
// 	return c.listAllPhotos("https://picasaweb.google.com/data/feed/api/user/default?kind=photo", max)
// }

// ListAlbums returns a list of the authenticated user's albums
// func (c *Client) ListAlbums(max int) ([]Entry, error) {
// 	url := "https://picasaweb.google.com/data/feed/api/user/default"
// 	if max > 0 {
// 		url += fmt.Sprintf("?max-results=%d", max)
// 	}
// 	// default "kind" is "album", according to the docs...
// 	data, err := c.getFeed(url)
// 	if err != nil {
// 		return nil, err
// 	}

// 	var results Atom
// 	err = xml.Unmarshal(data, &results)

// 	// sanitize album names
// 	for i := 0; i < len(results.Entries); i++ {
// 		fmt.Println("ALBUM TITLE BEFORE:", results.Entries[i].Title)
// 		results.Entries[i].Title = sanitizeFilename(results.Entries[i].Title)
// 		fmt.Println("ALBUM TITLE AFTER: ", results.Entries[i].Title)
// 	}

// 	return results.Entries, err
// }

// (Note: This call returns more info about each photo, including
// some exif data, and the content URLs to download the photos
// // include all exif data including GPS coordinates.)
// func (c *Client) ListAlbumPhotos(album Entry, max int) ([]Entry, error) {
// 	// Maximum start-index: ~10000.
// 	// https://code.google.com/p/gdata-issues/issues/detail?id=7004
// 	// https://github.com/camlistore/camlistore/issues/874
// 	return c.listAllPhotos("https://picasaweb.google.com/data/feed/api/user/default/albumid/"+album.ID, max)
// }

func (c *Client) listPhotosPage(baseURL string, start, max int) (Atom, error) {
	url, err := url.Parse(baseURL)
	if err != nil {
		return Atom{}, err
	}
	qs := url.Query()
	qs.Set("imgmax", "d") // "d" for original, high-res files
	qs.Set("start-index", strconv.Itoa(start))
	if max > 0 {
		qs.Set("max-results", strconv.Itoa(max))
	}
	url.RawQuery = qs.Encode()

	data, err := c.getFeed(url.String())
	if err != nil {
		return Atom{}, err
	}
	//fmt.Println("ITEMS:", string(data))

	var results Atom
	err = xml.Unmarshal(data, &results)

	// sanitize titles (file names)
	for i := 0; i < len(results.Entries); i++ {
		results.Entries[i].Title = path.Base(results.Entries[i].Title) // https://github.com/tgulacsi/picago/pull/6
		results.Entries[i].Title = sanitizeFilename(results.Entries[i].Title)
	}
	// TODO: Should we give a default file extension of .jpg
	// for images (or .mp4 for videos) that don't have one?
	// or should we be smart about detecting the file type, or none
	// of the above...

	return results, err
}

func (c *Client) getFeed(endpoint string) ([]byte, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("GData-Version", "2")

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	return ioutil.ReadAll(res.Body)
}

// sanitizeFilename replaces common special characters in filename.
// Only the file name should be passed in, NOT the whole path.
// It does map more than one character to empty string, meaning
// that it could introduce collisions, for example: "$5.jpg" and
// "5.jpg" will have the same output value. It is unfortunate.
func sanitizeFilename(filename string) string {
	r := strings.NewReplacer(
		"/", "",
		"\\", "",
		":", "",
		"@", "_at_",
		"+", "_",
		"*", "",
		"<", "",
		">", "",
		"{", "",
		"}", "",
		"^", "",
		"#", "",
		"!", "",
		"~", "",
		"$", "",
		"[", "",
		"]", "",
		"=", "",
		"|", "",
		"?", "",
		"`", "",
		"●", "-", // common with Google Hangouts albums
	)
	return r.Replace(filename)
}
