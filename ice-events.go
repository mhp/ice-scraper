package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/boltdb/bolt"
)

type EventContext struct {
	Day     string
	Product ProductId
}

func checkForEvents(db *bolt.DB, onlyToday bool) error {
	today := time.Now()
	todayKey := []byte(fmt.Sprintf("%04d-%02d-%02d", today.Year(), today.Month(), today.Day()))

	client := &http.Client{}

	return db.Update(func(tx *bolt.Tx) error {
		c := tx.Cursor()
		evCtx := EventContext{}

		for day, _ := c.Seek(todayKey); day != nil; day, _ = c.Next() {
			evCtx.Day = string(day)
			b := tx.Bucket(day)
			if b == nil {
				// No bucket, so no products available
				continue
			}

			if err := checkEventsForDay(client, b, evCtx); err != nil {
				return err
			}

			if onlyToday {
				break
			}
		}

		return nil
	})
}

const soonThreshold = time.Minute * 5

func checkIfEventsStartingSoon(db *bolt.DB) error {
	today := time.Now()
	todayKey := []byte(fmt.Sprintf("%04d-%02d-%02d", today.Year(), today.Month(), today.Day()))

	return db.Update(func(tx *bolt.Tx) error {
		b, _ := tx.Cursor().Seek(todayKey)
		if b == nil {
			// No events today!
			return nil
		}

		eventStartingSoon := false

		evs := tx.Bucket(b).Bucket([]byte("events"))
		if evs == nil {
			// No events recorded yet
			return nil
		}
		evs.ForEach(func(sess, v []byte) error {
			// Find last entry if it exists and deserialise it
			// compare to current.  If different, append current
			k, lastEvJson := evs.Bucket(sess).Cursor().Last()
			if k != nil {
				lastEv := wrappedEventInfo{}
				if err := json.Unmarshal(lastEvJson, &lastEv); err != nil {
					return fmt.Errorf("Can't parse last event info (%v): %v", string(k), err)
				}

				t, err := parseTimeLocally(string(todayKey), lastEv.StartTime)
				if err != nil {
					return err
				}

				// Work out when now is in the same timezone as the event times
				localNow := today.In(t.Location())

				if t.Before(localNow) {
					// Session already started, ignore it
					return nil
				}

				if t.Sub(localNow) < soonThreshold {
					eventStartingSoon = true
				}
			}

			return nil
		})

		if eventStartingSoon {
			client := &http.Client{}
			evCtx := EventContext{Day: string(todayKey)}
			return checkEventsForDay(client, tx.Bucket(b), evCtx)
		}
		return nil
	})
}

func checkEventsForDay(client *http.Client, b *bolt.Bucket, evCtx EventContext) error {
	productsAvailable := []ProductId{}
	if err := json.Unmarshal(b.Get([]byte("products")), &productsAvailable); err != nil {
		return fmt.Errorf("Can't parse products {%s}: %v", b.Get([]byte("products")), err)
	}

	evBucket, err := b.CreateBucketIfNotExists([]byte("events"))
	if err != nil {
		return fmt.Errorf("Can't create 'events' bucket: %v", err)
	}

	for _, pid := range productsAvailable {
		evCtx.Product = pid
		evs, err := getEventsInfo(client, evCtx.Day, pid)
		if err != nil || evs == nil {
			return fmt.Errorf("Can't retrieve event info: %v", err)
		}

		// Add 'em
		now := time.Now()
		for _, ev := range *evs {
			if err := updateEvent(evBucket, ev, evCtx, now); err != nil {
				return fmt.Errorf("Can't write event: %v", err)
			}
		}
	}
	return nil
}

// wrappedEventInfo embeds the EventInfo with an additional timestamp
type wrappedEventInfo struct {
	EventInfo
	UpdatedAt time.Time
}

// update event details by comparing with last poll result (in bucket named
// by session ID) and adding if different or if this is the first poll of
// the event
func updateEvent(eventsBucket *bolt.Bucket, ev EventInfo, evCtx EventContext, ts time.Time) error {
	b, err := eventsBucket.CreateBucketIfNotExists([]byte(ev.SessionId))
	if err != nil {
		return fmt.Errorf("Can't create bucket for session: %v", err)
	}

	// Find last entry if it exists and deserialise it
	// compare to current.  If different, append current
	k, lastEvJson := b.Cursor().Last()
	if k != nil {
		lastEv := wrappedEventInfo{}
		if err := json.Unmarshal(lastEvJson, &lastEv); err != nil {
			return fmt.Errorf("Can't parse last event info (%v): %v", string(k), err)
		}

		// If all of these fields are the same, no need to write the new event
		if ev.ProductName == lastEv.ProductName &&
			ev.Location == lastEv.Location &&
			ev.StartTime == lastEv.StartTime &&
			ev.EndTime == lastEv.EndTime &&
			ev.TotalSpaces == lastEv.TotalSpaces &&
			ev.AvailableSpaces == lastEv.AvailableSpaces &&
			ev.CapacityFreeAcademy == lastEv.CapacityFreeAcademy &&
			ev.AvailableFreeSpaces == lastEv.AvailableFreeSpaces {
			return nil
		}
		log.Println("Updating event info:", evCtx.Day, ev)
	} else {
		log.Println("Creating event info:", evCtx.Day, ev)
	}

	optionallyUpdateCalendar(ev, evCtx, ts)

	wev := wrappedEventInfo{ev, ts}
	evJson, err := json.Marshal(wev)
	if err != nil {
		return fmt.Errorf("Can't marshal event info: %v", err)
	}

	// Create the next key in the sequence, to log this event info
	id, _ := b.NextSequence()
	newKey := make([]byte, 8)
	binary.BigEndian.PutUint64(newKey, uint64(id))

	// Save this event info
	return b.Put(newKey, evJson)
}
