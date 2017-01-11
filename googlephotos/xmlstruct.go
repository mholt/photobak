package googlephotos

import (
	"path/filepath"
	"time"
)

// Structures in this file shamelessly borrowed
// from Tamás Gulácsi's excellent package:
// https://github.com/tgulacsi/picago
// then enhanced/changed a bit for this use case.

// Atom is an entire feed result from the API.
type Atom struct {
	ID           string    `xml:"id"`
	Name         string    `xml:"name"`
	Updated      time.Time `xml:"updated"`
	Title        string    `xml:"title"`
	Subtitle     string    `xml:"subtitle"`
	Icon         string    `xml:"icon"`
	Thumbnail    string    `xml:"http://schemas.google.com/photos/2007 thumbnail"`
	Author       Author    `xml:"author"`
	NumPhotos    int       `xml:"numphotos"`
	StartIndex   int       `xml:"startIndex"`
	TotalResults int       `xml:"totalResults"`
	ItemsPerPage int       `xml:"itemsPerPage"`
	Entries      []Entry   `xml:"entry"`
}

// Tags is item tags, like unique ID.
type Tags struct {
	ImageUniqueID string `xml:"imageUniqueID"`
}

// Entry is an entry in the feed results. Can be
// used for both albums and photos.
type Entry struct {
	ETag          string         `xml:"etag,attr"`
	EntryID       string         `xml:"http://www.w3.org/2005/Atom id"`
	ID            string         `xml:"http://schemas.google.com/photos/2007 id"`
	Timestamp     string         `xml:"http://schemas.google.com/photos/2007 timestamp"`
	Published     time.Time      `xml:"published"`
	Updated       time.Time      `xml:"updated"`
	Edited        time.Time      `xml:"edited"`
	AlbumID       string         `xml:"albumid"`
	ImageVersion  string         `xml:"imageVersion"`
	OriginalVideo *OriginalVideo `xml:"originalvideo"`
	VideoStatus   string         `xml:"videostatus"`
	Name          string         `xml:"http://schemas.google.com/photos/2007 name"`
	Title         string         `xml:"title"`
	Summary       string         `xml:"summary"`
	Links         []Link         `xml:"link"`
	Author        *Author        `xml:"author"`
	Location      string         `xml:"http://schemas.google.com/photos/2007 location"`
	NumPhotos     int            `xml:"numphotos"`
	Content       *EntryContent  `xml:"content"`
	Media         *EntryMedia    `xml:"group"`
	Exif          *EntryExif     `xml:"tags"`
	Point         string         `xml:"where>Point>pos"`
}

// CollectionID returns the collection ID.
func (e Entry) CollectionID() string { return e.ID }

// CollectionName returns the collection name.
func (e Entry) CollectionName() string { return e.Title }

// ItemID returns the item ID. Unfortunately, Google's "id" field
// is sometimes too unique: the same photo can have different IDs
// if in different albums. There is usually an ID in the exif
// tags of the XML which is more accurate. However, in some cases,
// I've seen that ID be the same for different versions (edits) of
// the same photo. In other words, it is not unique enough.  If we
// did use the exif ID, it would mean we potentially lose the edited
// version of the photo, but even if we did, it wouldn't be much
// loss since we still would have one version of the photo on disk.
//
// (In the case of an item with the same ID but different
// name in a single album, the first file would be left in place
// and the second one would not be written, but a media list file
// would be created in that directory to the first file in that same
// directory. Kinda weird but technically doing its job. This is
// what I witnessed when using the EXIF ID and that's how I
// detected the overlap.)
//
// Photobak will de-duplicate at the content level by inspecting
// downloaded files byte-for-byte. So even if this item is
// actually a duplicate, the image data won't be stored duplicated
// as long as Google Photos gives the same content for this item.
// However, I have seen cases where the same photo in Google
// Photos has different IDs and different checksums. Indeed, the
// image files had the same bytes until line 88443 of a hexdump,
// after which they varied until the end (one was even slightly
// shorter than the other, but they looked the same visually
// and had the same dimensions). This was confirmed independently
// of Photobak by using a browser, to ensure it's not a bug in
// the downloader.
//
// Long story short, I'm fairly confident using the Google
// Photos ID is sufficient to be unique without overwriting.
// But there will be some duplication of the item in the DB
// and maybe on disk too. If we ever want to go back to using
// EXIF IDs, we can try it... but people will have to
// re-download their whole collections unless we write some
// sort of subcommand to convert their DB.
func (e Entry) ItemID() string {
	// if e.Exif != nil && e.Exif.UID != "" {
	// 	return e.Exif.UID
	// }
	return e.ID
}

// ItemName returns the item's name (file name).
// It appends a file extension based on MIME type
// if there isn't one already, because sometimes
// Google Photos items don't come with file extensions. -_-
func (e Entry) ItemName() string {
	name := e.Title
	ext := filepath.Ext(name)
	if ext == "" {
		switch e.Content.Type {
		case "image/jpeg":
			name += ".jpg"
		case "image/png":
			name += ".png"
		case "video/mpeg4":
			name += ".mp4"
		case "image/gif":
			name += ".gif"
		}
	}
	return name
}

// ItemETag returns the item's ETag.
//
// NOTE: We could use the ETag field, but it seems
// Timestamp, Updated, Edited, or ImageVersion also
// work. All these were somewhat tested with
// changes and I chose to use Updated as the ETag.
func (e Entry) ItemETag() string { return e.Updated.String() }

// ItemCaption returns the item's summary/description.
func (e Entry) ItemCaption() string { return e.Summary }

// OriginalVideo is info about the originally-uploaded video.
type OriginalVideo struct {
	AudioCodec   string `xml:" audioCodec,attr"`
	Channels     string `xml:" channels,attr"`
	Duration     string `xml:" duration,attr"`
	FPS          string `xml:" fps,attr"`
	Height       string `xml:" height,attr"`
	SamplingRate string `xml:" samplingrate,attr"`
	VideoType    string `xml:" type,attr"`
	VideoCodec   string `xml:" videoCodec,attr"`
	Width        string `xml:" width,attr"`
}

// EntryExif is exif data given in the entry.
type EntryExif struct {
	FStop       float32 `xml:"fstop"`
	Make        string  `xml:"make"`
	Model       string  `xml:"model"`
	Exposure    float32 `xml:"exposure"`
	Flash       bool    `xml:"flash"`
	FocalLength float32 `xml:"focallength"`
	ISO         int32   `xml:"iso"`
	Timestamp   int64   `xml:"time"`
	UID         string  `xml:"imageUniqueID"`
}

// Link information.
type Link struct {
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
	URL  string `xml:"href,attr"`
}

// EntryMedia stores the list of media for the entry.
type EntryMedia struct {
	Title       string         `xml:"http://search.yahoo.com/mrss title"`
	Description string         `xml:"description"`
	Keywords    string         `xml:"keywords"`
	Credit      string         `xml:"credit"`
	Content     []MediaContent `xml:"content"`
	Thumbnail   []MediaContent `xml:"thumbnail"`
}

// MediaContent is a media content item.
type MediaContent struct {
	URL    string `xml:"url,attr"`
	Type   string `xml:"type,attr"`
	Width  int    `xml:"width,attr"`
	Height int    `xml:"height,attr"`
	Medium string `xml:"medium,attr"` // "image" or "video"
}

// EntryContent is a content item.
type EntryContent struct {
	URL  string `xml:"src,attr"`
	Type string `xml:"type,attr"`
}

// Author information.
type Author struct {
	Name string `xml:"name"`
	URI  string `xml:"uri"`
}
