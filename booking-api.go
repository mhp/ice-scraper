package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// The ice-sports calendar provides information about event availability on
// each day of the specified month, for a specific product.  It also contains
// entries for additional days either side of the specified month, so as to
// make the displayed widget slightly more intuitive, but no event data is
// made available for these extra days.

// calendar is a go representation of the json data for a particular month
type Calendar struct {
	CurrentMonth string
	Year         int
	Month        int
	Dates        []struct {
		DayOfWeek int
		Date      string
		HasEvent  bool
	}
}

// baseMonthUrl is the endpoint that provides the json calendar data
// Add query parameters for "month" (1-based integer), "year" (integer) and
// "productId" (UUID) to retrieve data for the given month and product
const baseMonthUrl = "https://bookings.national-ice-centre.com/booking/ice-sports-calendar"

// getCalendar retrieves the calendar information for the specified month and product
// returning a pointer to the imported data structure
func getCalendar(c *http.Client, month time.Month, year int, product ProductId) (*Calendar, error) {
	u, err := url.Parse(baseMonthUrl)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("month", strconv.FormatUint(uint64(month), 10))
	query.Set("year", strconv.FormatUint(uint64(year), 10))
	query.Set("productId", string(product))

	u.RawQuery = query.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	cal := Calendar{}
	if err := dec.Decode(&cal); err != nil {
		return nil, err
	}

	return &cal, nil
}

// EventsInfo is an array of structures representing events
// which exist on the specified day for the specified productId
// Much of the information provided over the API is ignored, as not relevant.
type EventsInfo []EventInfo

type EventInfo struct {
	SessionId   string
	ProductName string
	Location    string
	StartTime   string
	EndTime     string

	TotalSpaces         int
	AvailableSpaces     int
	CapacityFreeAcademy int
	AvailableFreeSpaces int
}

const baseEventTimesUrl = "https://bookings.national-ice-centre.com/booking/ice-sports-times"

// getEventTimes retrieves the list of event  information for the specified day and product
// returning a pointer to the imported data structure
func getEventsInfo(c *http.Client, date string, product ProductId) (*EventsInfo, error) {
	u, err := url.Parse(baseEventTimesUrl)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("date", date)
	query.Set("productId", string(product))

	u.RawQuery = query.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	ei := EventsInfo{}
	if err := dec.Decode(&ei); err != nil {
		return nil, err
	}

	return &ei, nil
}
