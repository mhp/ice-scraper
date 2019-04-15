package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/boltdb/bolt"
)

func checkForNewDays(db *bolt.DB) error {
	today := time.Now()
	thisMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.Local)

	c := &http.Client{}

	// Check this month and next for practice ice events...
	for _, month := range []time.Time{
		thisMonth,
		thisMonth.AddDate(0, 1, 0),
	} {
		log.Println("Checking", month)
		dwi, err := checkIceCalendar(c, month.Month(), month.Year())
		if err != nil {
			log.Println("Can't check calendar", month.Month(), "/", month.Year(), err)
			return err
		}

		newDays, err := addDays(db, dwi)
		if err != nil {
			log.Println("Can't add days to db", err)
			return err
		}
		if len(newDays) > 0 {
			log.Println("Added", len(newDays), "new days")
		}
	}
	return nil
}

type DayKey []byte

// addDays iterates over DaysWithIce, adding new ones to the database
// and returning a list of newly added keys
func addDays(db *bolt.DB, dwi DaysWithIce) ([]DayKey, error) {
	newKeys := []DayKey{}

	err := db.Update(func(tx *bolt.Tx) error {
		for ts, prods := range dwi {
			key := []byte(fmt.Sprintf("%04d-%02d-%02d", ts.Year(), ts.Month(), ts.Day()))
			b := tx.Bucket(key)
			if b == nil {
				// New day with events - create a bucket
				var err error
				b, err = tx.CreateBucket(key)
				if err != nil {
					return fmt.Errorf("Can't create bucket %v: %v", key, err)
				}

				// Note newly added key
				newKeys = append(newKeys, key)
			}

			// Prepare current products to compare/update
			v, err := json.Marshal(prods)
			if err != nil {
				return fmt.Errorf("Can't marshal products %v: %v", prods, err)
			}

			// Compare lengths rather than expecting product list to be sorted
			current := b.Get([]byte("products"))
			if len(current) != len(v) {
				if err := b.Put([]byte("products"), v); err != nil {
					return fmt.Errorf("Can't write products: %v", err)
				}
			}

		}
		return nil
	})

	return newKeys, err
}

// DaysWithIce is a map of times representing days to a list of products available on that day
type DaysWithIce map[time.Time][]ProductId

func checkIceCalendar(c *http.Client, month time.Month, year int) (DaysWithIce, error) {
	dwi := make(DaysWithIce)

	for _, product := range products {
		cal, err := getCalendar(c, month, year, product)
		if err != nil {
			return nil, fmt.Errorf("Can't get product calendar: %v", err)
		} else {
			for _, d := range cal.Dates {
				if d.HasEvent {
					t, _ := parseJSDate(d.Date)
					dwi[t] = append(dwi[t], product)
				}
			}
		}
	}
	return dwi, nil
}

// dateRe is a regular expression that matches the date representation
// used in the calendar json, and captures the timestamp (millis since 1970)
// and timezone information.
var dateRe = regexp.MustCompile(`/Date\(([0-9]+)\+([0-9]{4})\)/`)

// parseJSDate interprets strings like "/Date(1551398400000+0000)/" to
// extract a time.Time representation.  The timezone is ignored, since it
// doesn't appear to be used at the moment.
func parseJSDate(d string) (time.Time, error) {
	matches := dateRe.FindStringSubmatch(d)
	if len(matches) != 3 {
		return time.Time{}, fmt.Errorf("Can't parse time '%v'", d)
	}

	millis, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("Can't convert '%v'", matches[1])
	}
	// FIXME - timezone?  daylight saving maybe?
	return time.Unix(millis/1000, 0).In(time.UTC), nil
}
