package googlephotos

import "time"

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
// if in different albums. So we get the unique ID from exif data
// of the entry first, then fall back to the Google-given
// ID+timestamp, then just the ID. It's the best we can do.
func (e Entry) ItemID() string {
	if e.Exif != nil && e.Exif.UID != "" {
		return e.Exif.UID
	}
	if e.Timestamp != "" {
		return e.ID + "-" + e.Timestamp
	}
	return e.ID // NOTE! Same photo may have different IDs... :(
}

// ItemName returns the item's name (file name).
func (e Entry) ItemName() string { return e.Title }

// ItemETag returns the item's ETag.
func (e Entry) ItemETag() string { return e.ETag }

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
