package photobak

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"
	"time"
)

// Info is a log to write informational and
// notice messages to.
var Info = log.New(ioutil.Discard, "", 0)

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
	// The implementation MUST close the Item channel
	// when there are no more items to list!
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
	// local index. It must be unique across all
	// collections in the account.
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
	// The ID must be unique across all items in the
	// account, but a single item must not have more than
	// one ID, even if it appears in multiple albums!
	ItemID() string

	// ItemName returns the file name of the item (with
	// extension). No sanitization is performed on the
	// name, so implementations must ensure that the
	// name is safe to use as a filename.
	ItemName() string

	// ItemETag returns the ETag of this item. If the
	// cloud provider does not support ETags, this
	// can return an empty string. An ETag is used to
	// download an item only when it has changed. It is
	// effectively just a hash value or "Last Updated"
	// timestamp in a consistent format.
	ItemETag() string

	// ItemCaption is the caption or description
	// attached to this item.
	ItemCaption() string
}

// collection wraps a Collection with
// vital name+path information used
// for creating/updating one.
type collection struct {
	Collection
	dirName string
	dirPath string
}

// item wraps an Item with
// vital name+path information used
// for creating/updating one. isNew
// is set to true if the item
// is new and should not overwrite
// an existing file on disk.
type item struct {
	Item
	fileName    string
	filePath    string
	isNew       bool
	collections map[string]struct{}
}

type itemContext struct {
	item           Item
	coll           collection
	ac             accountClient
	saveEverything bool
	checkIntegrity bool
}

// dbCollection represents a collection (album,
// bucket, or stream) of photos/videos stored in
// the database.
type dbCollection struct {
	ID      string    // unique ID
	Name    string    // name of collection
	DirName string    // the name of the directory representing this collection
	DirPath string    // the repo-relative path to collection directory on disk
	Saved   time.Time // when this collection was put into the DB (or updated)
	Meta    collectionMeta
	Items   map[string]struct{} // the IDs of items that are in this collection
}

// collectionMeta is extra information
// about a collection.
type collectionMeta struct {
	API Collection // everything given by remote/API; only stored if requested
}

// dbItem represents an item stored in the database.
type dbItem struct {
	ID          string              // unique ID for this item (should be same across all collections)
	Name        string              // name as given by the API, usually the file name
	FileName    string              // same as Name, unless there is another file with the same name in its folder
	FilePath    string              // repo-relative path to the file on disk
	Checksum    []byte              // sha256 of the contents that we make while downloading it
	ETag        string              // ETag, like a hash but given by the API so we can know if it changed remotely
	Saved       time.Time           // when this item was put into the DB (or updated)
	Collections map[string]struct{} // the IDs of the collections this photo appears in
	Meta        itemMeta            // extra info that we don't rely on to function correctly
}

// itemMeta holds extra information about an item.
// Fields on this struct might not be set.
type itemMeta struct {
	API     Item     // everything given by remote/API; only stored if requested
	Setting *setting // obtained directly from embedded EXIF
	Caption string   // the caption/summary/description of the item
}

// setting is a place and time. This information
// might be extracted from EXIF data contained in the
// actual file if it is not available in the API
// response.
type setting struct {
	// Coordinates where the media originated.
	Latitude    float64
	Longitude   float64
	Altitude    float64
	AltitudeRef string

	// The timestamp when the media originated.
	OriginTime time.Time
}

var providers = make(map[string]Provider)

// RegisterProvider adds p to the list of providers.
func RegisterProvider(p Provider) {
	p.Name = strings.ToLower(p.Name)
	providers[p.Name] = p
}

type providerAccount struct {
	provider Provider
	username string // or email address
}

func (pa providerAccount) key() []byte {
	return []byte(fmt.Sprintf("%s:%s", pa.provider.Name, pa.username))
}

func (pa providerAccount) accountPath() string {
	username := strings.ToLower(pa.username)
	username = strings.Replace(username, "@", "_at_", -1)
	username = strings.Replace(username, "+", "_", -1)
	return filepath.Join(pa.provider.Name, username)
}

func (pa providerAccount) String() string {
	return string(pa.key())
}

// getAccounts gets a list of all the accounts
//
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
	// The lower-case, one word name of the provider.
	// Used as the flag to configure accounts.
	Name string

	// The human-readable, proper-cased name of
	// the provider.
	Title string

	// A function that gets a list of accounts
	// configured for this provider. Return a list
	// of usernames or account IDs or whatever.
	Accounts func() []string

	// A function to get credentials for the given
	// username. Return the credentials as bytes so
	// that your NewClient function can use them to
	// create an authorized client.
	Credentials func(username string) ([]byte, error)

	// A function that returns an authorized client
	// that can access the provider's API. The credentials
	// to be used in the client are passed in.
	NewClient func(credentials []byte) (Client, error)
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
