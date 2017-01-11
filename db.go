package photobak

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"time"

	"github.com/boltdb/bolt"
)

// The names of buckets to create in each account bucket.
var bucketNames = []string{
	"collections",
	"items",
}

type boltDB struct {
	*bolt.DB
}

// openDB opens a database.
func openDB(file string) (*boltDB, error) {
	db, err := bolt.Open(file, 0600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("checksums"))
		return err
	})
	return &boltDB{DB: db}, err
}

// createAccount creates pa in the database if it does not exist.
func (db *boltDB) createAccount(pa providerAccount) error {
	return db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(pa.key())
		if err != nil {
			return fmt.Errorf("create bucket %s: %v", pa.key(), err)
		}
		for _, b := range bucketNames {
			_, err := bucket.CreateBucketIfNotExists([]byte(b))
			if err != nil {
				return fmt.Errorf("create account bucket %s: %v", b, err)
			}
		}
		return nil
	})
}

// loadCredentials loads acct's credentials. If there are no
// credentials stored, a nil slice and nil error will be returned.
// The account must already be stored, or it is an error.
func (db *boltDB) loadCredentials(acct providerAccount) ([]byte, error) {
	var creds []byte
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(acct.key())
		if b == nil {
			return fmt.Errorf("account '%s' does not exist in DB", acct)
		}
		creds = b.Get([]byte("credentials"))
		return nil
	})
	return creds, err
}

// saveCredentials saves creds to account's bucket in the database.
// The account must already be stored.
func (db *boltDB) saveCredentials(acct providerAccount, creds []byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(acct.key())
		if b == nil {
			return fmt.Errorf("account '%s' does not exist in DB", acct)
		}
		return b.Put([]byte("credentials"), creds)
	})
}

func (db *boltDB) loadItem(acctKey []byte, itemID string) (*dbItem, error) {
	var item *dbItem
	err := db.View(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(acctKey)
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", acctKey)
		}
		items := accountBucket.Bucket([]byte("items"))
		if items == nil {
			return fmt.Errorf("account '%s' is missing 'items' bucket", acctKey)
		}
		return gobDecode(items.Get([]byte(itemID)), &item)
	})
	return item, err
}

func (db *boltDB) deleteItem(acct providerAccount, itemID string) error {
	return db.Update(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(acct.key())
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", acct)
		}
		items := accountBucket.Bucket([]byte("items"))
		if items == nil {
			return fmt.Errorf("account '%s' is missing 'items' bucket", acct)
		}
		// delete from checksum index
		var item *dbItem
		err := gobDecode(items.Get([]byte(itemID)), &item)
		if err != nil {
			return fmt.Errorf("loading item to get its hash: %v", err)
		}
		err = db.removeItemFromChecksumIndex(tx, item, acct.key())
		if err != nil {
			return err
		}
		// finally, delete item from DB
		return items.Delete([]byte(itemID))
	})
}

// removeItemFromChecksumIndex removes item from the checksum
// index; item must belong to the account given by acctKey.
// It is meant for use by already-open DB transactions.
func (db *boltDB) removeItemFromChecksumIndex(tx *bolt.Tx, item *dbItem, acctKey []byte) error {
	checksums := tx.Bucket([]byte("checksums"))
	if checksums == nil {
		return fmt.Errorf("no checksums bucket")
	}
	var list []accountItem
	err := gobDecode(checksums.Get(item.Checksum), &list)
	if err != nil {
		return fmt.Errorf("loading list of hashed items: %v", err)
	}
	for i, li := range list {
		if bytes.Equal(li.AcctKey, acctKey) && li.ItemID == item.ID {
			list = append(list[:i], list[i+1:]...)
		}
	}
	if len(list) == 0 {
		err := checksums.Delete(item.Checksum)
		if err != nil {
			return err
		}
	} else {
		listEnc, err := gobEncode(list)
		if err != nil {
			return err
		}
		err = checksums.Put(item.Checksum, listEnc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (db *boltDB) deleteCollection(acct providerAccount, collID string) error {
	return db.Update(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(acct.key())
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", acct)
		}
		items := accountBucket.Bucket([]byte("collections"))
		if items == nil {
			return fmt.Errorf("account '%s' is missing 'collections' bucket", acct)
		}
		return items.Delete([]byte(collID))
	})
}

func (db *boltDB) saveItem(acctKey []byte, itemID string, item *dbItem) error {
	return db.Update(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(acctKey)
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", acctKey)
		}

		// first, load the item so we have access to the previous checksum
		items := accountBucket.Bucket([]byte("items"))
		if items == nil {
			return fmt.Errorf("account '%s' is missing 'items' bucket", acctKey)
		}
		var savedItem *dbItem
		err := gobDecode(items.Get([]byte(itemID)), &savedItem)
		if err != nil {
			return fmt.Errorf("loading item %s: %v", itemID, err)
		}

		// then save this item
		itemEnc, err := gobEncode(item)
		if err != nil {
			return err
		}
		err = items.Put([]byte(itemID), itemEnc)
		if err != nil {
			return err
		}

		// then update the collections so they know they contain this item
		collections := accountBucket.Bucket([]byte("collections"))
		if collections == nil {
			return fmt.Errorf("account '%s' is missing 'collections' bucket", acctKey)
		}
		for collID := range item.Collections {
			err = db.addItemToCollection(accountBucket, itemID, collID)
			if err != nil {
				return fmt.Errorf("saving item to collection in DB: %v", err)
			}
		}

		// then update the checksums index so we know which items have this content
		checksums := tx.Bucket([]byte("checksums"))
		if checksums == nil {
			return fmt.Errorf("no 'checksums' bucket")
		}
		// if checksum has changed, detach this item from index at old checksum
		if savedItem != nil && !bytes.Equal(savedItem.Checksum, item.Checksum) {
			err := db.removeItemFromChecksumIndex(tx, savedItem, acctKey)
			if err != nil {
				return err
			}
		}
		// now add this item to its checksum's list
		var list []accountItem
		err = gobDecode(checksums.Get(item.Checksum), &list)
		if err != nil {
			return fmt.Errorf("getting list of items with same checksum: %v", err)
		}
		// only add this item to the list if it's not already there
		var found bool
		for _, li := range list {
			if bytes.Equal(li.AcctKey, acctKey) && li.ItemID == itemID {
				found = true
				break
			}
		}
		if !found {
			list = append(list, accountItem{AcctKey: acctKey, ItemID: itemID})
			encList, err := gobEncode(list)
			if err != nil {
				return fmt.Errorf("encoding list of items with same checksum: %v", err)
			}
			return checksums.Put(item.Checksum, encList)
		}
		return nil
	})
}

func (db *boltDB) saveItemToCollection(pa providerAccount, itemID, collID string) error {
	return db.Update(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(pa.key())
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", pa)
		}
		return db.addItemToCollection(accountBucket, itemID, collID)
	})
}

func (db *boltDB) addItemToCollection(accountBucket *bolt.Bucket, itemID, collID string) error {
	// first get the item
	items := accountBucket.Bucket([]byte("items"))
	if items == nil {
		return fmt.Errorf("missing 'items' bucket")
	}
	item := &dbItem{Collections: make(map[string]struct{})}
	err := gobDecode(items.Get([]byte(itemID)), &item)
	if err != nil {
		return fmt.Errorf("decoding item: %v", err)
	}

	// then add the collection ID to the item
	item.Collections[collID] = struct{}{}

	// save the item
	itemEnc, err := gobEncode(item)
	if err != nil {
		return err
	}
	err = items.Put([]byte(itemID), itemEnc)
	if err != nil {
		return err
	}

	// then open the collections bucket
	collections := accountBucket.Bucket([]byte("collections"))
	if collections == nil {
		return fmt.Errorf("account is missing 'collections' bucket")
	}

	// get the collection
	coll := dbCollection{Items: make(map[string]struct{})}
	err = gobDecode(collections.Get([]byte(collID)), &coll)
	if err != nil {
		return fmt.Errorf("decoding collection: %v", err)
	}

	// update its set of items to include this one
	coll.Items[itemID] = struct{}{}
	collEnc, err := gobEncode(coll)
	if err != nil {
		return fmt.Errorf("encoding collection: %v", err)
	}

	// save the collection
	err = collections.Put([]byte(collID), collEnc)
	if err != nil {
		return fmt.Errorf("saving collection: %v", err)
	}

	return nil
}

func (db *boltDB) collectionIDs(pa providerAccount) ([]string, error) {
	var list []string
	err := db.View(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(pa.key())
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", pa)
		}
		collections := accountBucket.Bucket([]byte("collections"))
		if collections == nil {
			return fmt.Errorf("account '%s' is missing 'collections' bucket", pa)
		}
		return collections.ForEach(func(k, v []byte) error {
			list = append(list, string(k))
			return nil
		})
	})
	return list, err
}

func (db *boltDB) loadCollection(acctKey []byte, collID string) (*dbCollection, error) {
	var coll *dbCollection
	err := db.View(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(acctKey)
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", acctKey)
		}
		collections := accountBucket.Bucket([]byte("collections"))
		if collections == nil {
			return fmt.Errorf("account '%s' is missing 'collections' bucket", acctKey)
		}
		return gobDecode(collections.Get([]byte(collID)), &coll)
	})
	return coll, err
}

func (db *boltDB) saveCollection(acctKey []byte, id string, coll *dbCollection) error {
	return db.Update(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(acctKey)
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", acctKey)
		}
		collections := accountBucket.Bucket([]byte("collections"))
		if collections == nil {
			return fmt.Errorf("account '%s' is missing 'collections' bucket", acctKey)
		}
		collEnc, err := gobEncode(coll)
		if err != nil {
			return err
		}
		return collections.Put([]byte(id), collEnc)
	})
}

func (db *boltDB) itemsWithChecksum(chksm []byte) ([]accountItem, error) {
	var list []accountItem
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("checksums"))
		if bucket == nil {
			return fmt.Errorf("checksums bucket does not exist in DB")
		}
		return gobDecode(bucket.Get(chksm), &list)
	})
	return list, err
}

// account key: provider:username (or email address)

/*
	ROOT
	|-- checksums
		|-- <sha> -> list of <accountKey>::<itemID>
	|-- googlephotos:my@email.com
		|-- credentials -> (token)
		|-- collections
			|-- (collection ID) -> (collection)
			|-- ...
		|-- items
			|-- (item ID) -> (item)
			|-- ...
	|-- googlephotos:foo@bar.com
		|-- ...
*/

// gobEncode gob encodes value.
func gobEncode(value interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(value)
	return buf.Bytes(), err
}

// gobDecode gob decodes buf into into.
func gobDecode(buf []byte, into interface{}) error {
	if buf == nil {
		return nil
	}
	dec := gob.NewDecoder(bytes.NewReader(buf))
	return dec.Decode(into)
}
