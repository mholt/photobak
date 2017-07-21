// Package googlephotos implements Google Photos access for photobak using the
// crippled Picasa Web Albums API.
package googlephotos

import (
	"encoding/gob"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mholt/photobak"
	"errors"
)

const (
	name  = "googlephotos"
	title = "Google Photos"
)

var (
	maxAlbums = -1
	maxPhotos = -1
)

func init() {
	var accounts photobak.StringFlagList
	flag.Var(&accounts, name, "Add a "+title+" account to the repository")
	flag.IntVar(&maxAlbums, "maxalbums", maxAlbums, "Maximum number of albums to process (-1 for all)")
	flag.IntVar(&maxPhotos, "maxphotos", maxPhotos, "Maximum number of photos per album to process (-1 for all)")

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
	return name
}

// ListCollections lists the albums belonging to the user.
func (c *Client) ListCollections() ([]photobak.Collection, error) {
	if maxAlbums == 0 {
		return []photobak.Collection{}, nil
	}

	// the picasa web album API docs say the default "kind" parameter
	// value is "album" which is what we want, so we don't bother to
	// specify it here.
	url := "https://picasaweb.google.com/data/feed/api/user/default"
	if maxAlbums > -1 {
		url += fmt.Sprintf("?max-results=%d", maxAlbums)
	}
	data, err := c.getFeed(url)
	if err != nil {
		return nil, err
	}

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

	sort.Stable(albumSorter(albums))

	return albums, nil
}

// ListCollectionItems lists all the items in the collection given by col and puts
// them down the itemChan. This method uses an API call to get photos for the
// default (authenticated) user and to get a list of all photos in the album. This
// provides different (and more) output than the API call which just gets a list of
// the users n-most-recent photos from their stream (which is limited to just 1000),
// and that API call doesn't include information like EXIF data. In other words,
// we're using the best available API call we have here.
//
// Note that, due to a bug in the Picasa Web Albums API, there is a limit as to how
// many photos can be retrieved on very large albums. See the README for more info.
func (c *Client) ListCollectionItems(col photobak.Collection, itemChan chan photobak.Item) (err error) {
	defer close(itemChan)
	url := "https://picasaweb.google.com/data/feed/api/user/default/albumid/" + col.CollectionID()

	// try a few times in case there's a network error
	for i := 0; i < 3; i++ {
		err = c.listAllPhotos(url, itemChan)
		if err == nil {
			break
		}
		log.Printf("[DEBUG] listing photos in album '%s' (attempt %d): %v", col.CollectionName(), i+1, err)
	}

	return
}

// listAllPhotos gets all photos in the album designated by the baseURL and pipes
// them down itemChan.
func (c *Client) listAllPhotos(baseURL string, itemChan chan photobak.Item) error {
	var page Atom
	var err error

	start := 1
	count := 0

	// we can't rely on NumPhotos in an album to be correct,
	// and the number of photos can change while download is
	// happening; so just keep downloading until no results.
	// (the i == 0 condition ensures we run at least once.)
	for i := 0; i == 0 || len(page.Entries) > 0; i++ {
		if maxPhotos > -1 && count >= maxPhotos {
			break
		}

		page, err = c.listPhotosPage(baseURL, start, maxPhotos-count)
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

// DownloadItemInto downloads item into w.
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

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP GET %s: %s", url, resp.Status)
	}

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

// listPhotosPage lists photos from a "page" which consists of a single API call.
// To get all the photos in an album, you will need to call this until there are
// no more results. If max is > 0, no more than that many results will be returned
// per page.
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

	var results Atom
	err = xml.Unmarshal(data, &results)

	// sanitize titles (file names)
	for i := 0; i < len(results.Entries); i++ {
		results.Entries[i].Title = path.Base(results.Entries[i].Title) // https://github.com/tgulacsi/picago/pull/6
		results.Entries[i].Title = sanitizeFilename(results.Entries[i].Title)
	}

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

	if res.StatusCode != http.StatusOK {
		return nil, errors.New(res.Status)
	}

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

// Sorts out all automatic albums to the end of the list, since I think generally
// users will want the physical files in the albums they've curated, rather than
// the default 'everything' album with thousands of items in it or automatically
// generated albums for the specific date or service.
type albumSorter []photobak.Collection

func (a albumSorter) Len() int {
	return len(a)
}

func (a albumSorter) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a albumSorter) Less(i, j int) bool {
	return prioritizeAlbum(a[i].CollectionName()) < prioritizeAlbum(a[j].CollectionName())
}

var automaticAlbumRe = regexp.MustCompile(`^(\d+|\d{4}-\d{2}-\d{2})$`)

func prioritizeAlbum(name string) int {
	if name == "Auto Backup" || name == "Автозагрузка" {
		return 1
	} else if strings.HasPrefix(name, "Hangout ") {
		return 2
	} else if automaticAlbumRe.MatchString(name) {
		return 3
	} else {
		return 0
	}
}
