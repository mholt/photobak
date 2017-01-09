package photobak

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

// Repository is a type that can store media files. It consists
// of a directory path and a database. It has methods to
// interact with providers (Client implementations) with which
// backups can be downloaded to this repository.
//
// A repository's files are totally managed and should not be
// modified, as each one is indexed in the database.
//
// A repository should not be changed after (or at least
// while) it performs a task.
type Repository struct {
	// the path to the directory of the repo. the leaf folder
	// of the path should be empty if it exists.
	path string

	// whether to save all API-given metadata in DB
	// saveEverything bool

	// the database to operate on; should be opened.
	db *boltDB

	// a map of files that are currently being downloaded/updated.
	// key is the item ID, value is the path it is being saved to.
	downloading   map[string]string
	downloadingMu sync.Mutex

	// a map of item path to channel used for waiting; if two
	// different items have same name and path, this map will
	// be used to ensure different filenames for each one.
	itemNames   map[string]chan struct{}
	itemNamesMu sync.Mutex
}

// OpenRepo opens a repository that is ready to store backups
// in. It is initiated with a path, where a folder will be created
// if it does not already exists, and a database will be created
// inside it. The path is where all saved assets will be stored.
// An opened repository should be closed when finished with it.
func OpenRepo(path string) (*Repository, error) {
	err := os.MkdirAll(path, 0700)
	if err != nil {
		return nil, err
	}

	dbPath := filepath.Join(path, "photobak.db")
	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}

	// make sure all accounts have a home in the DB
	for _, account := range getAccounts() {
		err := db.createAccount(account)
		if err != nil {
			return nil, err
		}
	}

	return &Repository{
		path:        path,
		db:          db,
		downloading: make(map[string]string),
		itemNames:   make(map[string]chan struct{}),
	}, nil
}

// Close closes a repository cleanly.
func (r *Repository) Close() error {
	return r.db.Close()
}

// getCredentials loads credentials for the given account, or if there
// are none, it will ask for new ones and save them, returning the
// byte representation of the credentials.
func (r *Repository) getCredentials(pa providerAccount) ([]byte, error) {
	// see if credentials are in database already
	creds, err := r.db.loadCredentials(pa)
	if err != nil {
		return nil, fmt.Errorf("loading credentials for %s: %v", pa.username, err)
	}
	if creds == nil {
		// we need to get credentials to access cloud provider
		creds, err = pa.provider.Credentials(pa.username)
		if err != nil {
			return nil, fmt.Errorf("getting credentials for %s: %v", pa.username, err)
		}
		err = r.db.saveCredentials(pa, creds)
		if err != nil {
			return nil, fmt.Errorf("saving credentials for %s: %v", pa.username, err)
		}
	}
	return creds, nil
}

// TODO: Should large collections be split up into folders with max. 1000 items in each? <nod>

// StoreAll downloads all media from all registered accounts
// and stores it in the repository path. It is idempotent in
// that it can be run multiple times (assuming the same
// accounts are configured) and only the items that need to
// be downloaded will be downloaded to keep things current
// and up-to-date.
//
// If saveEverything is true, the repository will also save
// everything the API provides about each item to the index.
// This will substantially increase the size of the database
// file, but if that extra data (like, say, links to thumbnail
// images or the number of comments on album) is important to
// you, set it to true.
//
// StoreAll operates per-collection (per-album), that is, it
// iterates each collection and downloads all the items for
// each collection, and organizes them by collection name
// on disk.
//
// StoreAll does not download multiple copies of the same
// photo, assuming the provider correctly IDs each item.
// If an item appears in more than one collection, the
// filepath to the item will be written to a text file
// in the other collection.
//
// StoreAll is NOT destructive or re-organizive (is that
// a word?). Collections that are deleted remotely, or items
// that are removed from collections or deleted entirely,
// will not disappear locally by running this method. It
// will, however, update existing items if they are outdated,
// missing, or corrupted locally.
func (r *Repository) StoreAll(saveEverything bool) error {
	// to each account, attach an authorized Client
	var accounts []accountClient
	for _, pa := range getAccounts() {
		creds, err := r.getCredentials(pa)
		if err != nil {
			return err
		}
		client, err := pa.provider.NewClient(creds)
		if err != nil {
			return fmt.Errorf("getting authenticated client: %v", err)
		}
		accounts = append(accounts, accountClient{
			account: pa,
			client:  client,
		})
	}

	// perform downloads for each account
	for _, ac := range accounts {
		var wg sync.WaitGroup
		wg.Add(1) // a "barrier" just in case Add() is never called in this loop

		listedCollections, err := ac.client.ListCollections()
		if err != nil {
			return err
		}

		for _, listedColl := range listedCollections {
			err := r.processCollection(listedColl, ac, &wg, saveEverything)
			if err != nil {
				return err
			}
		}

		wg.Done() // remove "barrier" to cover the case where Add() isn't called for this account
		wg.Wait()
	}

	return nil
}

// processCollection will process a collection from a provider.
func (r *Repository) processCollection(listedColl Collection, ac accountClient, wg *sync.WaitGroup, saveEverything bool) error {
	itemChan := make(chan Item)

	// see if we have the collection in the db already
	loadedColl, err := r.db.loadCollection(ac.account, listedColl.CollectionID())
	if err != nil {
		return err
	}

	// carefully craft the collection object... if it is a new collection,
	// we need to choose a folder name that's not in use (in case the name
	// is the same as an existing collection), otherwise use existing path.
	coll := collection{Collection: listedColl}
	if loadedColl == nil {
		// it's new! great, make sure we don't overwrite (merge) with
		// an existing collection of the same name in this account.
		coll.dirName, err = r.reserveUniqueFilename(ac.account.accountPath(), listedColl.CollectionName(), true)
		if err != nil {
			return err
		}
	} else {
		// we've seen this collection before, so use folder already on disk.
		coll.dirName = loadedColl.DirName
	}
	coll.dirPath = r.repoRelative(filepath.Join(ac.account.accountPath(), coll.dirName))

	// save collection to database
	dbc := &DBCollection{
		ID:      coll.CollectionID(),
		Name:    coll.CollectionName(),
		DirName: coll.dirName,
		DirPath: coll.dirPath,
		Saved:   time.Now(),
	}
	if saveEverything {
		dbc.Meta.API = coll.Collection
	}
	err = r.db.saveCollection(ac.account, dbc.ID, dbc)
	if err != nil {
		if loadedColl == nil {
			// this was a new collection, couldn't save it to DB,
			// so don't leave a stray folder on disk.
			os.Remove(coll.dirPath)
		}
		return fmt.Errorf("saving collection to database: %v", err)
	}

	// start some workers that will download items
	for i := 0; i < 5; i++ { // TODO: make number of downloaders (workers) adjustable
		wg.Add(1)
		go func(ac accountClient, coll collection, itemChan chan Item) {
			defer wg.Done()
			for receivedItem := range itemChan {
				err := r.processItem(receivedItem, coll, ac.account, ac.client, saveEverything)
				if err != nil {
					log.Println(err)
				}
			}
		}(ac, coll, itemChan)
	}

	// kick off the work for this account
	err = ac.client.ListCollectionItems(coll, itemChan)
	if err != nil {
		return fmt.Errorf("listing collection items: %v", err)
	}

	return nil
}

// processItem will process an item from a provider.
func (r *Repository) processItem(receivedItem Item, coll collection, pa providerAccount, client Client, saveEverything bool) error {
	itemID := receivedItem.ItemID()

	// check if we already have it
	loadedItem, err := r.db.loadItem(pa, itemID)
	if err != nil {
		return fmt.Errorf("loading item '%s' from database: %v", itemID, err)
	}

	if loadedItem == nil {
		// we don't have it; download and save item.
		fmt.Println("We don't have it")

		it := item{
			Item:     receivedItem,
			fileName: receivedItem.ItemName(),
			filePath: r.repoRelative(filepath.Join(pa.accountPath(), coll.dirName, receivedItem.ItemName())),
			isNew:    true,
		}

		err = r.downloadAndSaveItem(client, it, coll, pa, saveEverything)
		if err != nil {
			os.Remove(r.fullPath(it.filePath))
			return fmt.Errorf("downloading and saving new item: %v", err)
		}
	} else {
		fmt.Println("We already have it in the DB")

		// check ETag
		// TODO: This will be different for the same photo if it is in a different album :(
		// ALSO i've seen the same eTag for different photos in the same album :( :( :(
		// if loadedItem.Meta.API.ItemETag() != item.ItemETag() {
		// 	fmt.Println("ETag is different")
		// 	// TODO: re-download it to the path it is already at on disk
		// 	// and update the metadata in the database.
		// }

		// if we don't have it in this album already,
		// add path to text file in this album.
		has, err := r.localCollectionHasItem(pa, coll, loadedItem)
		if err != nil {
			return fmt.Errorf("checking if local collection has item: %v", err)
		}
		if !has {
			err := r.writeToMediaListFile(pa, coll, loadedItem.FilePath)
			if err != nil {
				return fmt.Errorf("writing to media list file: %v", err)
			}
		}

		chksm, err := r.hash(loadedItem.FilePath)
		if err != nil || !bytes.Equal(chksm, loadedItem.Hash) {
			// re-download, file was corrupted/changed (or missing, etc...)
			if err != nil {
				return fmt.Errorf("hashing file: %v", err)
			}
			log.Printf("[INFO] checksum mismatch, re-downloading: %s", loadedItem.FilePath)

			it := item{
				Item:     receivedItem,
				fileName: loadedItem.FileName,
				filePath: loadedItem.FilePath,
			}

			err := r.downloadAndSaveItem(client, it, coll, pa, saveEverything)
			if err != nil {
				return fmt.Errorf("re-downloading and saving existing item: %v", err)
			}
		}
	}

	return nil
}

// reserveUniqueFilename will look in dir (which must be repo-relative)
// for targetName. If it is taken, it will change the filename by
// adding a counter to the end of it, up to a certain limit, until it
// finds an available filename. This is safe for concurrent use.
// It reserves the filename by creating it in dir, and returns the
// name of the file (or directory, depending on isDir) created in dir.
func (r *Repository) reserveUniqueFilename(dir, targetName string, isDir bool) (string, error) {
	targetPath := filepath.Join(dir, targetName)
	r.itemNamesMu.Lock()
	ch, taken := r.itemNames[targetPath]
	if taken {
		r.itemNamesMu.Unlock()
		<-ch // wait for it to be available again
		r.itemNamesMu.Lock()
	}
	ch = make(chan struct{})
	r.itemNames[targetPath] = ch
	r.itemNamesMu.Unlock()

	candidate := targetName
	for i := 2; i < 1000; i++ { // this can handle up to 1000 collisions
		if !r.fileExists(filepath.Join(dir, candidate)) {
			break
		}
		parts := strings.SplitN(targetName, ".", 2)
		candidate = strings.Join(parts, fmt.Sprintf("-%03d.", i))
	}

	finalPath := r.fullPath(filepath.Join(dir, candidate))

	if isDir {
		log.Println("RESERVING DIR:", finalPath)
		err := os.MkdirAll(finalPath, 0700)
		if err != nil {
			return candidate, err
		}
	} else {
		f, err := os.Create(finalPath)
		if err != nil {
			log.Println("ERROR HERE!", dir, candidate)
			return candidate, err
		}
		f.Close()
	}

	r.itemNamesMu.Lock()
	delete(r.itemNames, targetPath)
	close(ch)
	r.itemNamesMu.Unlock()

	return candidate, nil
}

// hash loads fpath (which must be repo-relative)
// and hashes it, returning the hash in bytes.
func (r *Repository) hash(fpath string) ([]byte, error) {
	f, err := os.Open(r.fullPath(fpath))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := sha256.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return nil, err
	}

	return h.Sum(nil), nil
}

// dishonestWriter has a very niche use (unless you're a major
// news organization). It merely wraps an io.Writer so that
// if the writer tries to write to a pipe where the read end
// is closed, the function still returns a success result as
// if no error occurred. Other errors are still reported.
// (This is useful in our case when streaming data to the
// EXIF decoder as part of a MultiWriter.)
type dishonestWriter struct {
	io.Writer
}

// Write writes p to w.Writer, returning a dishonest result
// if writing fails due to a closed pipe.
func (w dishonestWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err == io.ErrClosedPipe {
		return len(p), nil
	}
	return n, err
}

func (r *Repository) downloadAndSaveItem(client Client, it item, coll collection, pa providerAccount, saveEverything bool) error {
	itemID := it.ItemID()
	mapKey := pa.provider.Name + ":" + itemID

	r.downloadingMu.Lock()
	dlPath, ok := r.downloading[mapKey]
	if ok {
		r.downloadingMu.Unlock()
		// it's already being downloaded.

		if dlPath != it.filePath {
			// ... but to a different location. so we
			// should write out that location to a text
			// file in this coll so the owner can find
			// it later if they want to, and we don't
			// duplicate the file on disk.
			log.Printf("[INFO] %s is currently being downloaded to another location", it.ItemName())

			err := r.writeToMediaListFile(pa, coll, dlPath)
			if err != nil {
				return err
			}
		}

		// TODO: Take any note in the database that it's in this coll?

		return nil
	}

	// not being downloaded; claim it for us.
	r.downloading[mapKey] = it.filePath
	r.downloadingMu.Unlock()
	defer func(mapKey string) {
		r.downloadingMu.Lock()
		delete(r.downloading, mapKey)
		r.downloadingMu.Unlock()
	}(mapKey)

	if it.isNew {
		itemFileName, err := r.reserveUniqueFilename(coll.dirPath, it.ItemName(), false)
		if err != nil {
			return fmt.Errorf("reserving unique filename: %v", err)
		}
		it = item{
			Item:     it.Item,
			fileName: itemFileName,
			filePath: r.repoRelative(filepath.Join(coll.dirPath, itemFileName)),
		}
	}

	err := os.MkdirAll(r.fullPath(coll.dirPath), 0700)
	if err != nil {
		return fmt.Errorf("creating folder for collection '%s': %v", coll.CollectionName(), err)
	}
	fmt.Println("Made collection folder, downloading item...")

	outFile, err := os.Create(r.fullPath(it.filePath))
	if err != nil {
		return fmt.Errorf("opening output file %s: %v", it.filePath, err)
	}
	defer outFile.Close()

	fmt.Println("Output file opened")

	h := sha256.New()
	pr, pw := io.Pipe()
	mw := io.MultiWriter(outFile, h, dishonestWriter{pw})

	fmt.Println("Created streams")

	var x *exif.Exif
	go func() {
		// an item may not have EXIF data, and that is not
		// an error, it just means we don't have any meta
		// data from the file. if it does have EXIF data
		// and we have trouble reading it for some reason,
		// it doesn't really matter because there's nothing
		// we can do about it; so we ignore the error.
		x, _ = exif.Decode(pr)

		// the exif.Decode() call above only reads as much
		// as needed to conclude the EXIF portion, then it
		// stops reading. this is a problem, because it blocks
		// all other writes in the MultiWriter from happening
		// since this one is not reading. the DishonestWriter
		// that we wrapped the write end of the pipe with will,
		// as a special case, report a totally successful write
		// if it gets a "write to closed pipe" error. so even
		// though the whole file has likely not been read yet,
		// it is not a bug to close the read end of this pipe.
		pr.Close()
	}()

	fmt.Println("Downloading...")

	err = client.DownloadItemInto(it.Item, mw)
	if err != nil {
		return fmt.Errorf("downloading %s: %v", it.ItemName(), err)
	}
	fmt.Println("Download finished; saving to DB.")

	setting, err := r.getSettingFromEXIF(x)
	if err != nil {
		// TODO: I don't really care about this error...
		// TODO: Either way... improve logging.
		log.Println(err)
	}

	meta := ItemMeta{Setting: setting}
	if saveEverything {
		// NOTE: If the item caption is already stored as
		// part of the Item, this will duplicate it in
		// the database. Oh well. Hopefully it's small.
		meta.API = it.Item
	}

	dbi := &DBItem{
		ID:           itemID,
		Name:         it.ItemName(),
		FileName:     it.fileName,
		FilePath:     r.repoRelative(it.filePath),
		Caption:      it.ItemCaption(),
		Meta:         meta,
		Saved:        time.Now(),
		CollectionID: coll.CollectionID(),
		Hash:         h.Sum(nil),
	}

	err = r.db.saveItem(pa, itemID, dbi)
	if err != nil {
		return fmt.Errorf("saving item '%s' to database: %v", it.fileName, err)
	}

	fmt.Println("Save to DB complete")

	return nil
}

// repoRelative turns a full path into a path that
// is relative to the repository root. Paths stored
// in the database or shown in media list files should
// always be repo-relative; only switch to full paths
// (or "relative to current directory" paths) when
// interacting with the file system.
func (r *Repository) repoRelative(fpath string) string {
	return strings.TrimPrefix(fpath, filepath.Clean(r.path)+string(filepath.Separator))
}

// fullPath converts a repo-relative path to a full path
// usable with the file system. Paths should always be stored
// as repo-relative, but must be converted to their "full"
// (or, more precisely, "absolute or relative to current
// directory") path for interaction with the file system.
func (r *Repository) fullPath(repoRelative string) string {
	return filepath.Join(r.path, repoRelative)
}

// mediaListPath returns the path to the media list file for
// the given collection. The returned path is repo-relative
// if coll.dirPath is repo-relative (which it should be).
func (r *Repository) mediaListPath(coll collection) string {
	return filepath.Join(coll.dirPath, "others.txt")
}

// getSettingFromEXIF extracts coordinate, timestamp, and
// altitude information from x.
func (r *Repository) getSettingFromEXIF(x *exif.Exif) (*Setting, error) {
	if x == nil {
		return nil, nil
	}

	// coordinates
	lat, lon, err := x.LatLong()
	if err != nil {
		return nil, fmt.Errorf("getting coordinates from EXIF: %v", err)
	}

	// timestamp
	ts, err := x.DateTime()
	if err != nil {
		return nil, fmt.Errorf("getting timestamp from EXIF: %v", err)
	}

	// altitude
	rawAlt, err := x.Get(exif.GPSAltitude)
	if err != nil {
		return nil, fmt.Errorf("getting altitude from EXIF: %v", err)
	}
	alt, err := rawAlt.Rat(0)
	if err != nil {
		return nil, fmt.Errorf("converting altitude value: %v", err)
	}
	altFlt, _ := alt.Float64()

	// altitude reference, adjust altitude if needed
	altRef, err := x.Get(exif.GPSAltitudeRef)
	if err != nil {
		return nil, fmt.Errorf("getting altitude reference from EXIF: %v", err)
	}
	altRefInt, err := altRef.Int(0)
	if err != nil {
		return nil, fmt.Errorf("converting altitude reference: %v", err)
	}
	if altRefInt == 1 && altFlt > 0 {
		// 0 indicates above sea level, 1 is below sea level.
		// we expect the altitude relative to sea level.
		altFlt *= -1.0
	}

	return &Setting{
		Latitude:   lat,
		Longitude:  lon,
		OriginTime: ts,
		Altitude:   altFlt,
	}, nil
}

// localCollectionHasItem returns true if the given collection
// has the item in it, either as an actual file or a reference
// in the media list file.
func (r *Repository) localCollectionHasItem(pa providerAccount, coll collection, localItem *DBItem) (bool, error) {
	// check for item on disk first
	// TODO: If file has the same name, but is actually a different file,
	// this could erroneously return true when it shouldn't. To be 100%
	// correct on this, we would need to keep an index of all item IDs
	// that are saved in each collection.
	if r.fileExists(localItem.FilePath) {
		return true, nil
	}

	// check others.txt file to see if item is in the list
	file, err := os.Open(r.fullPath(r.mediaListPath(coll)))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fpath := strings.TrimSpace(scanner.Text())
		if fpath == localItem.FilePath {
			return true, nil
		}
	}

	return false, scanner.Err()
}

// fileExists returns true if there is not an
// error stat'ing the file at fpath, which will
// be evaluated relative to the repo path.
func (r *Repository) fileExists(fpath string) bool {
	_, err := os.Stat(r.fullPath(fpath))
	return err == nil
}

// writeToMediaListFile adds dlPath to the media list file
// in the given collection for the given account.
func (r *Repository) writeToMediaListFile(pa providerAccount, coll collection, dlPath string) error {
	err := os.MkdirAll(coll.dirPath, 0700)
	if err != nil {
		return fmt.Errorf("making folder %s: %v", coll.dirPath, err)
	}
	mediaListFile := r.fullPath(r.mediaListPath(coll))
	of, err := os.OpenFile(mediaListFile, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("opening media list file %s: %v", mediaListFile, err)
	}
	defer of.Close()
	_, err = fmt.Fprintln(of, dlPath)
	if err != nil {
		return fmt.Errorf("appending to media list file %s: %v", mediaListFile, err)
	}
	return nil
}

// accountClient is a providerAccount with
// a Client authorized to access the account.
type accountClient struct {
	account providerAccount
	client  Client
}
