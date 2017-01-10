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
	return &boltDB{DB: db}, err
}

// createAccount creates pa in the database if it does not exist.
func (db *boltDB) createAccount(pa providerAccount) error {
	return db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(pa.key())
		if bucket == nil {
			var err error
			bucket, err = tx.CreateBucket(pa.key())
			if err != nil {
				return fmt.Errorf("create bucket %s: %v", pa.key(), err)
			}
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

func (db *boltDB) loadItem(acct providerAccount, itemID string) (*DBItem, error) {
	var item *DBItem
	err := db.View(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(acct.key())
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", acct)
		}
		items := accountBucket.Bucket([]byte("items"))
		if items == nil {
			return fmt.Errorf("account '%s' is missing 'items' bucket", acct)
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
		return items.Delete([]byte(itemID))
	})
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

func (db *boltDB) saveItem(pa providerAccount, id string, item *DBItem) error {
	return db.Update(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(pa.key())
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", pa)
		}

		// first save the item
		items := accountBucket.Bucket([]byte("items"))
		if items == nil {
			return fmt.Errorf("account '%s' is missing 'items' bucket", pa)
		}
		itemEnc, err := gobEncode(item)
		if err != nil {
			return err
		}
		err = items.Put([]byte(id), itemEnc)
		if err != nil {
			return err
		}

		// then update the collections so they know they contain this item
		collections := accountBucket.Bucket([]byte("collections"))
		if collections == nil {
			return fmt.Errorf("account '%s' is missing 'collections' bucket", pa)
		}
		for collID := range item.Collections {
			err = db.addItemToCollection(accountBucket, id, collID)
			if err != nil {
				return fmt.Errorf("saving item to collection in DB: %v", err)
			}
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
	item := &DBItem{Collections: make(map[string]struct{})}
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
	coll := DBCollection{Items: make(map[string]struct{})}
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

func (db *boltDB) loadCollection(pa providerAccount, collID string) (*DBCollection, error) {
	var coll *DBCollection
	err := db.View(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(pa.key())
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", pa)
		}
		collections := accountBucket.Bucket([]byte("collections"))
		if collections == nil {
			return fmt.Errorf("account '%s' is missing 'collections' bucket", pa)
		}
		return gobDecode(collections.Get([]byte(collID)), &coll)
	})
	return coll, err
}

func (db *boltDB) saveCollection(pa providerAccount, id string, coll *DBCollection) error {
	return db.Update(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(pa.key())
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", pa)
		}
		collections := accountBucket.Bucket([]byte("collections"))
		if collections == nil {
			return fmt.Errorf("account '%s' is missing 'collections' bucket", pa)
		}
		collEnc, err := gobEncode(coll)
		if err != nil {
			return err
		}
		return collections.Put([]byte(id), collEnc)
	})
}

// account key: provider:username (or email address)

/*
	ROOT
	|-- googlephotos:Matthew.Holt@gmail.com
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
