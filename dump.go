package main

import (
	"fmt"
	"log"

	"github.com/boltdb/bolt"
)

// bucket for days - keys of the form 2019-03-27
//   "products": list of product IDs for events on that day
//   also contains bucket for events - keys to be event ids?
//     each event contains metadata as extracted

// /2019-03-27/
// /2019-03-27/products:[list-of-products]
// /2019-03-27/events/
// /2019-03-27/events/session-id/
// /2019-03-27/events/session-id/<nextsequence>:json(eventInfo)

func dumpDb(db *bolt.DB) {
	if err := db.View(func(tx *bolt.Tx) error {

		c := tx.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			if v == nil {
				fmt.Println("root-Bucket-start", string(k))
				dumpBucket(tx.Bucket(k))
				fmt.Println("root-Bucket-end", string(k))
			} else {
				fmt.Println("unexpected key in root", k, v)
			}
		}

		return nil
	}); err != nil {
		log.Println("Can't dump db:", err)
	}
}

func dumpBucket(b *bolt.Bucket) error {
	c := b.Cursor()

	for k, v := c.First(); k != nil; k, v = c.Next() {
		if v == nil {
			fmt.Println("Bucket-start", string(k))
			dumpBucket(b.Bucket(k))
			fmt.Println("Bucket-end", string(k))
		} else {
			fmt.Printf("key=%s, value=%s\n", k, v)
		}
	}
	return nil
}
