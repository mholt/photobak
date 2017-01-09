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

func (db *boltDB) saveItem(acct providerAccount, id string, item *DBItem) error {
	return db.Update(func(tx *bolt.Tx) error {
		accountBucket := tx.Bucket(acct.key())
		if accountBucket == nil {
			return fmt.Errorf("account '%s' does not exist in DB", acct)
		}
		items := accountBucket.Bucket([]byte("items"))
		if items == nil {
			return fmt.Errorf("account '%s' is missing 'items' bucket", acct)
		}
		itemEnc, err := gobEncode(item)
		if err != nil {
			return err
		}
		return items.Put([]byte(id), itemEnc)
	})
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
