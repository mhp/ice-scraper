package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/boltdb/bolt"
)

func showSummary(db *bolt.DB, startToday, endTomorrow bool) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintf(w, "Date\tStart\tEnd\tPad\t#Academy\t#Other\tType\n")
	if err := db.View(func(tx *bolt.Tx) error {
		c := tx.Cursor()

		firstDay, _ := c.First()
		var count int
		if startToday {
			today := time.Now()
			todayKey := []byte(fmt.Sprintf("%04d-%02d-%02d", today.Year(), today.Month(), today.Day()))
			firstDay, _ = c.Seek(todayKey)

			if endTomorrow {
				// Only valid if starting today!  Emit 2 summaries
				count = 2
			}
		}

		for day := firstDay; day != nil; day, _ = c.Next() {
			b := tx.Bucket(day)
			evs := b.Bucket([]byte("events"))
			if evs != nil {
				summariseDay(w, evs, string(day))
			}

			if endTomorrow {
				count -= 1
				if count <= 0 {
					break
				}
			}
		}
		return nil
	}); err != nil {
		log.Println("Can't summarise db:", err)
	}

	w.Flush()
}

type summary struct {
	StartTime string
	EndTime   string
	Location  string
	Academy   int
	Other     int
	Type      string
}

func summariseDay(w io.Writer, evs *bolt.Bucket, day string) {
	todaysEvents := []summary{}
	evs.ForEach(func(sessionId, _ []byte) error {
		_, evJson := evs.Bucket(sessionId).Cursor().Last()
		ev := EventInfo{}
		if json.Unmarshal(evJson, &ev) == nil {
			todaysEvents = append(todaysEvents, summary{
				StartTime: ev.StartTime,
				EndTime:   ev.EndTime,
				Location:  ev.Location,
				Academy:   ev.CapacityFreeAcademy - ev.AvailableFreeSpaces,
				Other:     ev.TotalSpaces - ev.AvailableSpaces,
				Type:      ev.ProductName})
		}
		return nil
	})
	sort.SliceStable(todaysEvents, func(i, j int) bool {
		return todaysEvents[i].StartTime < todaysEvents[j].StartTime
	})
	for _, entry := range todaysEvents {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			day,
			entry.StartTime, entry.EndTime, entry.Location,
			entry.Academy, entry.Other,
			entry.Type)
		// Suppress repeating the same date
		day = ""
	}
}
