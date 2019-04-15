package main

import (
	"bytes"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// idEncoder is an encoder to make almost valid Google Calendar ids.
// Once converted to lower case, they are valid.  See the `id` entry here:
// https://developers.google.com/calendar/v3/reference/events/insert
var idEncoder = base32.HexEncoding.WithPadding(base32.NoPadding)

// Event is a selection of fields from the underlying API model, chosen
// for usefulness in this application.  Many more fields exist.
// https://developers.google.com/calendar/v3/reference/events#resource
type GCalEvent struct {
	Id          string `json:"id,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	Start       struct {
		DateTime time.Time `json:"dateTime,omitempty"`
	} `json:"start,omitempty"`
	End struct {
		DateTime time.Time `json:"dateTime,omitempty"`
	} `json:"end,omitempty"`
}

// referenceDate is a parse string to let us parse the
// day and time taken from the ice schedule
// day is written in 2006-01-02 format,
// and time in 15:04:05 format
// We'll stick them together with a space between them
const referenceDate = "2006-01-02 15:04:05"

var localTimezone *time.Location

func initialiseLocalTimezone() {
	if localTimezone == nil {
		// localTimezone needs to be explicit, as I run my
		// Raspberry Pis in UTC all the time
		var err error
		localTimezone, err = time.LoadLocation("Europe/London")
		if err != nil {
			log.Println("Can't load timezone, defaulting to UTC")
			localTimezone = time.UTC
		}
	}
}

func parseTimeLocally(day, tim string) (time.Time, error) {
	// Make sure the timezone is initialised
	initialiseLocalTimezone()

	return time.ParseInLocation(referenceDate, fmt.Sprintf("%s %s", day, tim), localTimezone)
}

// makeProductLink returns a link to the booking page for the given product
// It looks like appending the product ID but base64-encoded after the url,
// using `!` as a separator is enough, although this doesn't pin down the day
// or session sadly.
func makeProductLink(productId ProductId) string {
	return fmt.Sprintf("https://bookings.national-ice-centre.com/booking/ice-sports-details!%v",
		base64.RawURLEncoding.EncodeToString([]byte(productId)))
}

func makeGCalEvent(ev EventInfo, evCtx EventContext, ts time.Time) (*GCalEvent, error) {
	// Make sure the timezone is initialised
	initialiseLocalTimezone()

	newEvent := GCalEvent{
		Id:       strings.ToLower(idEncoder.EncodeToString([]byte(ev.SessionId))),
		Summary:  ev.ProductName,
		Location: ev.Location,
		Description: fmt.Sprintf("%v Academy, %v other booked\n%v\nLast updated: %v\n",
			ev.CapacityFreeAcademy-ev.AvailableFreeSpaces,
			ev.TotalSpaces-ev.AvailableSpaces,
			makeProductLink(evCtx.Product),
			ts.In(localTimezone).Format(time.Stamp)),
	}

	startTime, err := parseTimeLocally(evCtx.Day, ev.StartTime)
	if err != nil {
		return nil, errors.Wrap(err, "parsing event start time")
	}
	newEvent.Start.DateTime = startTime

	endTime, err := parseTimeLocally(evCtx.Day, ev.EndTime)
	if err != nil {
		return nil, errors.Wrap(err, "parsng event end time")
	}
	newEvent.End.DateTime = endTime

	return &newEvent, nil
}

func insertCalendarEvent(c *http.Client, calendarId string, ev *GCalEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return errors.Wrap(err, "marshalling new event")
	}

	url := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%v/events", calendarId)

	resp, err := c.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return errors.Wrap(err, "inserting event")
	}

	jsonData, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	newEvent := GCalEvent{}
	if err := json.Unmarshal(jsonData, &newEvent); err != nil {
		return errors.Wrap(err, "parsing inserted event")
	}

	return nil
}

var ErrNotFound = errors.New("Not found")

func updateCalendarEvent(c *http.Client, calendarId string, ev *GCalEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return errors.Wrap(err, "marshalling new event")
	}

	url := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%v/events/%v", calendarId, ev.Id)

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return errors.Wrap(err, "creating put request")
	}
	req.Header.Add("Content-Type", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return errors.Wrap(err, "updating event")
	}

	jsonData, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	// If the update failed because the event doesn't exist,
	// return an error to trigger an insert operation instead
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}

	newEvent := GCalEvent{}
	if err := json.Unmarshal(jsonData, &newEvent); err != nil {
		return errors.Wrap(err, "parsing updated event")
	}

	return nil
}

func optionallyUpdateCalendar(ev EventInfo, evCtx EventContext, ts time.Time) {
	if GCalClient == nil {
		return
	}

	calEv, err := makeGCalEvent(ev, evCtx, ts)
	if err != nil {
		log.Print("Can't convert calendar event:", err)
		return
	}

	err = updateCalendarEvent(GCalClient, GCalCalendarId, calEv)
	if err == ErrNotFound {
		log.Print("Calendar event not found, inserting...")
		err = insertCalendarEvent(GCalClient, GCalCalendarId, calEv)
	}

	if err != nil {
		log.Print("Calendar event update failed:", err)
	}
}
