package googlephotos

import "time"

// Structures in this file shamelessly borrowed
// from Tamás Gulácsi's excellent package:
// https://github.com/tgulacsi/picago
// then enhanced/changed a bit for this use case.

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

type Tags struct {
	ImageUniqueID string `xml:"imageUniqueID"`
}

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

func (e Entry) CollectionID() string   { return e.ID }
func (e Entry) CollectionName() string { return e.Title }

// TODO: Yikes, the ID may be too unique. Same photo has different IDs :(
// But <gphoto:timestamp> seems unique, as does exif>uniquePhotoID
// -- we may need to try various combinations until we find non-empty
// values that we are confident will correctly ID a photo... :(
// fall back to e.ID if all else fails.
// For videos, tags>imageUniqueID should do. I think.
func (e Entry) ItemID() string {
	if e.Exif != nil && e.Exif.UID != "" {
		return e.Exif.UID
	}
	// TODO: Fall back to timestamp?
	// if e.Timestamp != "" {
	// 	return e.Timestamp
	// }
	return e.ID // NOTE! Same photo may have different IDs... :(
}
func (e Entry) ItemName() string    { return e.Title }
func (e Entry) ItemETag() string    { return e.ETag }
func (e Entry) ItemCaption() string { return e.Summary }

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

type Link struct {
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
	URL  string `xml:"href,attr"`
}

type EntryMedia struct {
	Title       string         `xml:"http://search.yahoo.com/mrss title"`
	Description string         `xml:"description"`
	Keywords    string         `xml:"keywords"`
	Credit      string         `xml:"credit"`
	Content     []MediaContent `xml:"content"`
	Thumbnail   []MediaContent `xml:"thumbnail"`
}

type MediaContent struct {
	URL    string `xml:"url,attr"`
	Type   string `xml:"type,attr"`
	Width  int    `xml:"width,attr"`
	Height int    `xml:"height,attr"`
	Medium string `xml:"medium,attr"` // "image" or "video"
}

type EntryContent struct {
	URL  string `xml:"src,attr"`
	Type string `xml:"type,attr"`
}

type Author struct {
	Name string `xml:"name"`
	URI  string `xml:"uri"`
}
