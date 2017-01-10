package photobak

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// StoreAllAndSync is like StoreAll, but after storing new items,
// items that are saved locally that have been deleted from remote
// will be deleted locally, and collections that no longer have
// items in them will be updated locally to reflect that.
func (r *Repository) StoreAllAndSync(saveEverything bool) error {
	return r.storeAll(saveEverything, true)
}

// Sync will update the local repository to match deletions
// and removals from the remote. It does not perform additive
// operations.
func (r *Repository) Sync() error {
	accounts, err := r.authorizedAccounts()
	if err != nil {
		return err
	}

	for _, ac := range accounts {
		state, err := r.getRemoteState(ac)
		if err != nil {
			log.Println("[ERROR] %v", err)
			continue
		}

		localCollections, err := r.db.collectionIDs(ac.account)
		if err != nil {
			log.Println("[ERROR] %v", err)
			continue
		}

		for _, collID := range localCollections {
			coll, err := r.db.loadCollection(ac.account, collID)
			if err != nil {
				return err
			}

			if _, ok := state[collID]; !ok {
				// collection does not exist remotely anymore; delete locally.
				err := r.deleteCollection(ac.account, coll)
				if err != nil {
					log.Println("[ERROR] %v", err)
					continue
				}
				continue
			}

			// check for items in the collection that may
			// not exist remotely anymore
			for itemID := range coll.Items {
				if _, ok := state[collID][itemID]; !ok {
					// item does not exist remotely anymore, remove it
					// from this collection.
					item, err := r.db.loadItem(ac.account, itemID)
					if err != nil {
						return err
					}
					err = r.deleteItemFromCollection(ac.account, item, coll)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func (r *Repository) deleteCollection(pa providerAccount, dbc *DBCollection) error {
	for itemID := range dbc.Items {
		item, err := r.db.loadItem(pa, itemID)
		if err != nil {
			return err
		}
		err = r.deleteItemFromCollection(pa, item, dbc)
		if err != nil {
			return err
		}
	}

	err := r.db.deleteCollection(pa, dbc.ID)
	if err != nil {
		return err
	}

	// we'll delete the collection's folder now, but just
	// to be nice (and safe) we'll make sure it's empty.
	// it SHOULD be empty if nobody is tampering with the
	// repository.
	fullDirPath := r.fullPath(dbc.DirPath)
	f, err := os.Open(fullDirPath)
	if err != nil {
		return err
	}
	names, err := f.Readdirnames(2)
	f.Close()
	if err != nil && err != io.EOF {
		return err
	}

	// delete the folder if empty or if the
	// only files are those stupid hidden
	// ones created by file explorer programs
	delFolder := len(names) == 0
	for _, name := range names {
		if len(name) > 0 && name[0] != '.' && name != "Thumbs.db" {
			delFolder = false
			break
		}
		delFolder = true
	}
	if delFolder {
		return os.RemoveAll(fullDirPath)
	}

	return nil
}

type idSet map[string]struct{}

func (r *Repository) getRemoteState(ac accountClient) (map[string]idSet, error) {
	remote := make(map[string]idSet)

	collections, err := ac.client.ListCollections()
	if err != nil {
		return remote, err
	}

	for _, coll := range collections {
		itemChan := make(chan Item)
		collID := coll.CollectionID()

		remote[collID] = make(idSet)

		var wg sync.WaitGroup
		wg.Add(1)
		go func(collID string, itemChan chan Item) {
			defer wg.Done()
			for item := range itemChan {
				remote[collID][item.ItemID()] = struct{}{}
			}
		}(collID, itemChan)

		err = ac.client.ListCollectionItems(coll, itemChan)
		if err != nil {
			return remote, fmt.Errorf("listing collection items: %v", err)
		}
		wg.Wait()
	}

	return remote, nil
}

func (r *Repository) deleteItem(pa providerAccount, dbi *DBItem) error {
	// delete file on disk
	// delete all references to it in medialist files (by iterating collections on the item)
	// delete references to item in db collections bucket (by iterating collections on the item)
	// delete item from database

	// delete file on disk
	err := os.Remove(r.fullPath(dbi.FilePath))
	if err != nil {
		log.Println("[ERROR] deleting file for %s: %v", dbi.Name, err)
	}

	// delete all references to the item in medialist files
	// and in the database's collections bucket, for each collection.
	for collID := range dbi.Collections {
		err = r.removeItemFromCollection(pa, dbi, collID)
		if err != nil {
			log.Println("[ERROR] %v", err)
			continue
		}
	}

	// delete item from the database
	err = r.db.deleteItem(pa, dbi.ID)

	return nil
}

func (r *Repository) removeItemFromCollection(pa providerAccount, dbi *DBItem, collID string) error {
	dbc, err := r.db.loadCollection(pa, collID)
	if err != nil {
		return fmt.Errorf("loading collection %s from DB: %v", collID, err)
	}

	// delete from media list file
	err = r.replaceInMediaListFile(dbc.DirPath, dbi.FilePath, "")
	if err != nil {
		return fmt.Errorf("removing item %s from collection media path: %v", dbi.Name, err)
	}

	// delete from database indexes
	delete(dbc.Items, dbi.ID)
	err = r.db.saveCollection(pa, dbc.ID, dbc)
	if err != nil {
		return fmt.Errorf("updating collection %s in database: %v", dbc.Name, err)
	}
	delete(dbi.Collections, dbc.ID)
	err = r.db.saveItem(pa, dbi.ID, dbi)
	if err != nil {
		return fmt.Errorf("updating item %s in database: %v", dbi.Name, err)
	}

	return nil
}

// deleteItemFromCollection properly deletes dbi from dbc.
func (r *Repository) deleteItemFromCollection(pa providerAccount, dbi *DBItem, dbc *DBCollection) error {
	if r.fileExists(filepath.Join(dbc.DirPath, dbi.FileName)) {
		// this collection is the lucky one with the hard copy of
		// the file, so we need to move it to another collection
		// that has it and re-point all the references on disk to
		// the new path.
		if len(dbi.Collections) == 1 {
			// this is the only collection with the item,
			// so delete it entirely.
			return r.deleteItem(pa, dbi)
		}

		// there are other collections with this item; move the file.

		// choose another collection to be the destination
		var destCollID string
		for collID := range dbi.Collections {
			if collID != dbc.ID {
				destCollID = collID
				break
			}
		}
		if destCollID == "" {
			return fmt.Errorf("could not find another collection to move %s to", dbi.FilePath)
		}
		destColl, err := r.db.loadCollection(pa, destCollID)
		if err != nil {
			return err
		}

		// find unique filename in the collection
		itemFileName, err := r.reserveUniqueFilename(destColl.DirPath, dbi.Name, false)
		if err != nil {
			return fmt.Errorf("reserving unique filename: %v", err)
		}

		// get destination path and move file
		newFilePath := filepath.Join(destColl.DirPath, itemFileName)
		err = os.Rename(r.fullPath(dbi.FilePath), r.fullPath(newFilePath))
		if err != nil {
			return err
		}

		// that destination should have this item in its media list file,
		// so delete that entry, because now it lives in that collection.
		err = r.replaceInMediaListFile(destColl.DirPath, dbi.FilePath, "")
		if err != nil {
			return err
		}

		// update all other media list files to point to the new file path.
		for collID := range dbi.Collections {
			if collID == destColl.ID || collID == dbc.ID {
				// skip the destination collection (we removed it
				// from that file already), and skip the collection
				// it was removed from
				continue
			}
			err = r.replaceInMediaListFile(dbc.DirPath, dbi.FilePath, newFilePath)
			if err != nil {
				return err
			}
		}

		// update item in DB
		dbi.FilePath = newFilePath
		return r.removeItemFromCollection(pa, dbi, dbc.ID)
	}

	// if we got here, it should be in a media list file
	// in dbc, which implies that dbc is not the only
	// collection with the item, so this is easy.
	return r.removeItemFromCollection(pa, dbi, dbc.ID)
}
