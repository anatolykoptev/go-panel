package main

import (
	"time"

	"github.com/anatolykoptev/go-panel/components"
)

// occDay parses an ISO date; a bad literal is a programming error in the demo.
func occDay(iso string) time.Time {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		panic(err)
	}
	return t
}

// showcaseOccupancy is a fixed-date board (the component never reads the
// clock): a sale, an adjacent renewal, a hold overtaken by a sale, a lease
// that crosses the window edge and a one-day hole.
const occRows = 7

func showcaseOccupancy(months int) components.Occupancy {
	rows := make([]components.OccupancyRow, 0, occRows)
	for i := 1; i <= occRows; i++ {
		rows = append(rows, components.OccupancyRow{Num: i, Label: "Position " + string(rune('0'+i))})
	}
	return components.Occupancy{
		Label: "Occupancy by position", Rows: rows, Months: months,
		Start: occDay("2026-09-01"), Today: occDay("2026-10-26"),
		GapHref: showcasePath + "?from={from}&to={to}&row={row}", ZoomHref: showcasePath + "?months={months}",
		Spans: []components.Span{
			{Row: 1, From: occDay("2026-01-01"), To: occDay("2026-12-01"), Tone: components.SpanSale, Label: "Coffee Atlas", Href: showcasePath},
			{Row: 1, From: occDay("2026-12-01"), To: occDay("2027-06-01"), Tone: components.SpanHold, Label: "Renewal hold", Href: showcasePath},
			{Row: 2, From: occDay("2026-08-01"), To: occDay("2026-11-20"), Tone: components.SpanRenewalDue, Label: "Fitness Atlant", Href: showcasePath},
			{Row: 3, From: occDay("2026-10-01"), To: occDay("2027-01-01"), Tone: components.SpanHold, Label: "Hold: Barbershop", Href: showcasePath},
			{Row: 3, From: occDay("2026-11-01"), To: occDay("2027-05-01"), Tone: components.SpanSale, Label: "Dental Plus", Href: showcasePath},
			{Row: 4, From: occDay("2026-03-01"), To: occDay("2026-10-01"), Tone: components.SpanEnded, Label: "Old lease", Href: showcasePath},
			{Row: 4, From: occDay("2026-10-02"), To: occDay("2027-04-01"), Tone: components.SpanSale, Label: "Bakery No.5", Href: showcasePath},
			{Row: 6, From: occDay("2026-09-15"), To: occDay("2026-12-15"), Tone: components.SpanConflict, Label: "Double booking", Href: showcasePath},
			{Row: 6, From: occDay("2026-11-15"), To: occDay("2027-02-15"), Tone: components.SpanSale, Label: "Yoga Studio", Href: showcasePath},
		},
	}
}
