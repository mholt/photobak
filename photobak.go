package photobak

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// Client is a type that can interfact with a media
// storage service.
type Client interface {
	// Name should return the lower-cased, one-word
	// name of the service the client connects to.
	// It should be unique as it is used on the file
	// system and as an identifier in the database.
	Name() string

	// ListCollections should return the list of all the
	// collections of media (i.e. albums) from which
	// photos (and videos, etc.) will be downloaded.
	ListCollections() ([]Collection, error)

	// ListCollectionItems gets all the media in the
	// collection and sends each one down the channel.
	ListCollectionItems(Collection, chan Item) error

	// DownloadItemInto gets the item from the service
	// and writes it to the writer.
	DownloadItemInto(Item, io.Writer) error
}

// Collection is a collection of media, like a
// photo album or stream or bucket or whatever.
type Collection interface {
	// CollectionID returns the unique ID of this
	// collection; it will be used as a key in the
	// local index.
	CollectionID() string

	// CollectionName returns the human-readable
	// name (or a filename) for this collection.
	// No sanitization is performed on this
	// name, so implementations must ensure the
	// return value is safe to use as a directory
	// name on the file system.
	CollectionName() string
}

// Item is a media item: typically a photo or video.
type Item interface {
	// ItemID returns the unique ID of the item, used
	// locally as a key in the index. De-duplication will
	// be performed based on this ID. If an item appears
	// in multiple collections, for example, it should
	// have the same ID in both collections. If the IDs
	// are different, the photo will be downloaded one time
	// for each album that it's in, which is undesirable.
	ItemID() string

	// ItemName returns the file name (with extension)
	// of the item. No sanitization is performed on
	// the name, so implementations must ensure that
	// the name is safe as a filename.
	ItemName() string

	// ItemETag returns the ETag of this item. If the
	// cloud provider does not support ETags, this
	// can return an empty string and ETags will not
	// be used. An ETag is used to download an item
	// only when it has changed. It is effectively
	// just a hash value.
	ItemETag() string

	// ItemCaption is the caption or description
	// attached to this item.
	ItemCaption() string
}

type collection struct {
	Collection
	dirName string
	dirPath string
}

type item struct {
	Item
	fileName string
	filePath string
	isNew    bool
}

// DBCollection represents a collection (album,
// bucket, or stream) of photos/videos stored in
// the database.
type DBCollection struct {
	ID      string    // unique ID
	Name    string    // name of collection
	DirName string    // the name of the directory representing this collection
	DirPath string    // the repo-relative path to collection directory on disk
	Saved   time.Time // when this collection was put into the DB (or updated)
	Meta    CollectionMeta
}

type CollectionMeta struct {
	API Collection // everything given by remote/API; only stored if requested
}

// DBItem represents an item stored in the database.
type DBItem struct {
	ID           string    // unique ID for this item (should be same across all collections)
	Name         string    // name as given by the API, usually the file name
	FileName     string    // same as Name, unless there is another file with the same name in its folder
	FilePath     string    // repo-relative path to the file on disk
	Hash         []byte    // sha256 of the contents
	Saved        time.Time // when this item was put into the DB (or updated)
	CollectionID string    // the key to index the album of this photo
	Caption      string    // the summary/caption/description of the item, if not stored in Meta.
	Meta         ItemMeta  // extra info
}

// ItemMeta holds extra information about an item.
// Fields on this struct may not be set.
type ItemMeta struct {
	API     Item     // everything given by remote/API; only stored if requested
	Setting *Setting // obtained directly from embedded EXIF
}

// Setting is a place and time. This information
// may be extracted from EXIF data contained in the
// actual file if it is not available in the API
// response.
type Setting struct {
	// Coordinates where the media originated.
	Latitude    float64
	Longitude   float64
	Altitude    float64
	AltitudeRef string

	// The timestamp when the media originated.
	OriginTime time.Time
}

var providers = make(map[string]Provider)

func RegisterProvider(p Provider) {
	p.Name = strings.ToLower(p.Name)
	providers[p.Name] = p
}

type providerAccount struct {
	provider Provider
	username string // or email address
}

func (a providerAccount) key() []byte {
	return []byte(fmt.Sprintf("%s:%s", a.provider.Name, a.username))
}

func (pa providerAccount) accountPath() string {
	username := strings.ToLower(pa.username)
	username = strings.Replace(username, "@", "_at_", -1)
	username = strings.Replace(username, "+", "_", -1)
	return filepath.Join(pa.provider.Name, username)
}

func (a providerAccount) String() string {
	return string(a.key())
}

func getAccounts() []providerAccount {
	var accounts []providerAccount
	for _, p := range providers {
		for _, a := range p.Accounts() {
			accounts = append(accounts, providerAccount{
				provider: p,
				username: strings.ToLower(a),
			})
		}
	}
	return accounts
}

// Provider represents a cloud storage provider.
type Provider struct {
	Name        string
	Title       string
	Accounts    func() []string
	Credentials func(string) ([]byte, error)
	NewClient   func([]byte) (Client, error)
}

// StringFlagList is used to store flags of repeating
// occurrences, for example: "-opt a -opt b -opt c"
// will contain ["a", "b", "c"] in the slice.
type StringFlagList []string

// String returns the string representation of l.
func (l *StringFlagList) String() string {
	return strings.Join(*l, ", ")
}

// Set satisfies the flag.Value interface.
func (l *StringFlagList) Set(value string) error {
	*l = append(*l, value)
	return nil
}
