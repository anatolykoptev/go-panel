package main

import "github.com/anatolykoptev/go-panel/components"

// showcaseUnits is one collection's worth of numbered places, one per tone, so
// the showcase shows every look (and one unsafe href that must render unlinked).
func showcaseUnits() components.UnitRow {
	return components.UnitRow{
		Label: "Places of Top-10 coffee shops",
		Units: []components.Unit{
			{Num: 1, Tone: components.UnitFree, Title: "Place 1 · Free", Href: showcasePath},
			{Num: 2, Tone: components.UnitHold, Note: "to 12.11", Title: "Place 2 · Hold until 12.11 · Romashka LLC", Href: showcasePath},
			{Num: 3, Tone: components.UnitSale, Title: "Place 3 · Sold · Coffee Atlas", Href: showcasePath},
			{Num: 4, Tone: components.UnitRenewalDue, Note: "to 30.11", Title: "Place 4 · Ends 30.11, no renewal", Href: showcasePath},
			{Num: 5, Tone: components.UnitUnverified, Title: "Place 5 · Under review"},
			{Num: 6, Tone: components.UnitConflict, Title: "Place 6 · Overlapping bookings", Href: showcasePath},
			{Num: 7, Tone: components.UnitSale, Title: "Place 7 · Unsafe link is dropped", Href: "//evil.example"},
		},
	}
}
