package main

import (
	"log"
	"net/http"
	"os"

	"github.com/boltdb/bolt"
)

var GCalClient *http.Client

func setupGcalSync() {
	gcalCredFile := os.Getenv("ICESCRAPER_GCAL_CRED_FILE")
	gcalTokenFile := os.Getenv("ICESCRAPER_GCAL_TOKEN_FILE")

	// We need the authfile but tokenfile is optional
	if gcalCredFile != "" {
		ga, err := NewAuthenticator(gcalCredFile, gcalTokenFile)
		if err != nil {
			log.Println("Can't create GCal client - no syncing", err)
		}

		GCalClient = &http.Client{Transport: ga}
	}
}

const DefaultDbName = "ice-info.db"
const DefaultProductsName = "products.json"

func main() {
	if len(os.Args) != 2 {
		log.Fatalln("Specify argument")
	}

	dbName := os.Getenv("ICESCRAPER_DB_FILE")
	if dbName == "" {
		dbName = DefaultDbName
	}

	db, err := bolt.Open(dbName, 0644, nil)
	if err != nil {
		log.Fatalln("Can't open database:", err)
	}
	defer db.Close()

	setupGcalSync()

	prodFile := os.Getenv("ICESCRAPER_PRODUCTS_FILE")
	if prodFile == "" {
		prodFile = DefaultProductsName
	}
	if err := loadProducts(prodFile); err != nil {
		log.Fatalln("Can't load products:", err)
	}

	switch os.Args[1] {

	// Run this daily to find what products are on which days
	case "check-calendar":
		checkForNewDays(db)

	// Run this a few times a day to discover events for known
	// products, and update the booking info
	case "check-events":
		checkForEvents(db, false)

	// Run this more frequently, doing the same for just today's events
	case "check-todays-events":
		checkForEvents(db, true)

	// Run this all the time - it only does work if an event is about to start
	case "check-if-events-starting-soon":
		checkIfEventsStartingSoon(db)

	// Debugging / help commands
	case "summary": // From today onwards
		showSummary(db, true, false)
	case "brief-summary": // Today and tomorrow only
		showSummary(db, true, true)
	case "full-summary": // Whole database
		showSummary(db, false, false)
	case "dump-db":
		dumpDb(db)
	default:
		log.Fatalln("no such command:", os.Args[1])
	}
}
