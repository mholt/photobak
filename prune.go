package photobak

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Prune will update the local repository to match deletions
// and removals from the remote. It does not perform additive
// operations.
func (r *Repository) Prune() error {
	accounts, err := r.authorizedAccounts()
	if err != nil {
		return err
	}

	for _, ac := range accounts {
		state, err := r.getRemoteState(ac)
		if err != nil {
			log.Printf("[ERROR] %v", err)
			continue
		}

		localCollections, err := r.db.collectionIDs(ac.account)
		if err != nil {
			log.Printf("[ERROR] %v", err)
			continue
		}

		for _, collID := range localCollections {
			coll, err := r.db.loadCollection(ac.account.key(), collID)
			if err != nil {
				return err
			}

			if _, ok := state[collID]; !ok {
				// collection does not exist remotely anymore; delete locally.
				Info.Printf("Collection '%s' does not exist remotely anymore; deleting local copy", coll.DirName)
				err := r.deleteCollection(ac.account, coll)
				if err != nil {
					log.Printf("[ERROR] %v", err)
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
					item, err := r.db.loadItem(ac.account.key(), itemID)
					if err != nil {
						return err
					}
					Info.Printf("Item '%s' does not exist in '%s' anymore; deleting local copy", item.FileName, coll.DirName)
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

func (r *Repository) deleteCollection(pa providerAccount, dbc *dbCollection) error {
	for itemID := range dbc.Items {
		item, err := r.db.loadItem(pa.key(), itemID)
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

// deleteItem cleanly removes from the repository the item dbi
// that belongs to pa and is in collection dbc.
func (r *Repository) deleteItem(pa providerAccount, dbc *dbCollection, dbi *dbItem) error {
	// this item may or may not have a physical presence in dbc's folder.
	// it won't if it is a duplicate of another item, in which case the
	// medialist file in dbc's folder will point to it and it will get
	// removed below as we call removeItemFromCollection. However, if the
	// physical file for this item does exist in this folder, we will need
	// to do some bookkeeping, either by deleting the file (if no others
	// with the same checksum point to it) or by moving it to another item
	// with the same checksum and re-pointing everything to the new path.

	if r.fileExists(filepath.Join(dbc.DirPath, dbi.FileName)) {
		// find out if this is the last item that uses this file
		list, err := r.db.itemsWithChecksum(dbi.Checksum)
		if err != nil {
			return err
		}
		for i, li := range list {
			if bytes.Equal(li.AcctKey, pa.key()) && li.ItemID == dbi.ID {
				// delete this item from the checksum index
				list = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(list) == 0 {
			// that was the last one, so we're good to delete the file
			err := os.Remove(r.fullPath(dbi.FilePath))
			if err != nil {
				log.Printf("[ERROR] deleting file for %s: %v", dbi.Name, err)
			}
		} else {
			// other items still reference this file, so move it to any one of them
			otherItem, err := r.db.loadItem(list[0].AcctKey, list[0].ItemID)
			if err != nil {
				return err
			}
			_, err = r.movePhysicalFile(pa.key(), dbc, dbi, otherItem, list[0].AcctKey)
			if err != nil {
				return err
			}
		}
	}

	// delete all references to the item in medialist files
	// and in the database's collections bucket, for each collection.
	for collID := range dbi.Collections {
		err := r.removeItemFromCollection(pa, dbi, collID)
		if err != nil {
			log.Printf("[ERROR] %v", err)
			continue
		}
	}

	// delete item from the database
	return r.db.deleteItem(pa, dbi.ID)
}

// removeItemFromCollection removes pa's item dbi from collID.
// It does not delete the file on disk but just removes it
// from the collection. dbi is saved to the DB at the end.
func (r *Repository) removeItemFromCollection(pa providerAccount, dbi *dbItem, collID string) error {
	dbc, err := r.db.loadCollection(pa.key(), collID)
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
	err = r.db.saveCollection(pa.key(), dbc.ID, dbc)
	if err != nil {
		return fmt.Errorf("updating collection %s in database: %v", dbc.Name, err)
	}
	delete(dbi.Collections, dbc.ID)
	err = r.db.saveItem(pa.key(), dbi.ID, dbi)
	if err != nil {
		return fmt.Errorf("updating item %s in database: %v", dbi.Name, err)
	}

	return nil
}

func (r *Repository) deleteItemFromCollection(pa providerAccount, dbi *dbItem, dbc *dbCollection) error {
	if len(dbi.Collections) == 1 {
		// this is the only collection with the item,
		// so delete it entirely.
		return r.deleteItem(pa, dbc, dbi)
	}

	if r.fileExists(filepath.Join(dbc.DirPath, dbi.FileName)) {
		// this collection is the lucky one with the hard copy of
		// the file, so we need to move it to another collection
		// that has it and re-point all the references on disk to
		// the new path.

		newFilePath, err := r.movePhysicalFile(pa.key(), dbc, dbi, dbi, pa.key())
		if err != nil {
			return err
		}

		// update item's path (the call to removeItemFromCollection
		// will save the item in the DB)
		dbi.FilePath = newFilePath
	}

	return r.removeItemFromCollection(pa, dbi, dbc.ID)
}

// movePhysicalFile moves the contents (the actual file on disk)
// referred to by origin.FilePath to any of the collections
// in dest. The providerAccount passed in should be the owner
// of the DESTINATION item (dest). The moved file will inherit the
// name of dest.FileName. origin and dest can be the same item.
// It returns the new file path.
func (r *Repository) movePhysicalFile(originAcctKey []byte, originColl *dbCollection, origin, dest *dbItem, destAcctKey []byte) (string, error) {
	// choose another collection to be the destination
	var destCollID string
	for collID := range dest.Collections {
		if originColl == nil || collID != originColl.ID {
			destCollID = collID
			break
		}
	}
	if destCollID == "" {
		return "", fmt.Errorf("could not find another collection to move %s to", origin.FilePath)
	}
	destColl, err := r.db.loadCollection(destAcctKey, destCollID)
	if err != nil {
		return "", err
	}

	// find unique filename in the collection
	itemFileName, err := r.reserveUniqueFilename(destColl.DirPath, dest.Name, false)
	if err != nil {
		return "", fmt.Errorf("reserving unique filename: %v", err)
	}

	// get destination path and move file
	newFilePath := filepath.Join(destColl.DirPath, itemFileName)
	err = os.Rename(r.fullPath(origin.FilePath), r.fullPath(newFilePath))
	if err != nil {
		return newFilePath, err
	}

	// that destination should have this item in its media list file,
	// so delete that entry, because now it lives in that collection.
	err = r.replaceInMediaListFile(destColl.DirPath, origin.FilePath, "")
	if err != nil {
		return newFilePath, err
	}

	// update all other media list files to point to the new file path.
	for collID := range origin.Collections {
		if collID == destColl.ID || (originColl != nil && collID == originColl.ID) {
			// skip the destination collection (we removed it
			// from that file already), and skip the collection
			// it was removed from
			continue
		}
		otherColl, err := r.db.loadCollection(originAcctKey, collID)
		if err != nil {
			return newFilePath, err
		}
		err = r.replaceInMediaListFile(otherColl.DirPath, origin.FilePath, newFilePath)
		if err != nil {
			return newFilePath, err
		}
	}

	// update all items with the same checksum to point to the new location
	err = r.moveSharedChecksumFile(originAcctKey, origin, newFilePath)
	if err != nil {
		return newFilePath, err
	}

	return newFilePath, nil
}

// moveSharedChecksumFile moves all items with the same checksum
// as acctKey's item dbi to point to a file at newFilePath.
func (r *Repository) moveSharedChecksumFile(acctKey []byte, dbi *dbItem, newFilePath string) error {
	list, err := r.db.itemsWithChecksum(dbi.Checksum)
	if err != nil {
		return err
	}

	for _, li := range list {
		if bytes.Equal(li.AcctKey, acctKey) && li.ItemID == dbi.ID {
			continue // skip this item, it's being deleted anyway
		}

		// load the other item that has this content
		otherItem, err := r.db.loadItem(li.AcctKey, li.ItemID)
		if err != nil {
			return err
		}

		// update all the media list files so they point to the new path
		for collID := range otherItem.Collections {
			otherColl, err := r.db.loadCollection(li.AcctKey, collID)
			if err != nil {
				return err
			}
			err = r.replaceInMediaListFile(otherColl.DirPath, otherItem.FilePath, newFilePath)
			if err != nil {
				return err
			}
		}

		// finally, update the file path on the item and save it
		otherItem.FilePath = newFilePath
		err = r.db.saveItem(li.AcctKey, li.ItemID, otherItem)
		if err != nil {
			return err
		}
	}

	return nil
}
