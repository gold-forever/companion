package addondata

import (
	"fmt"
	"strings"
)

// limit is a game's Data.lua limits: over data bytes the file is refused and the last good
// one stays; a market over market bytes is written with a warning.
type limit struct{ data, market int }

// The limits are sized from the files expected, not from anything the client imposes. An item is about
// 52 bytes in Lua, 8 more with a week mean and about 21 more with sold and listed, 81–83 with all.
//
//	Forever, 3,012 items a market:  244–250 KB a market, 4 markets 0.98–1.00 MB
//	Forever, 4,000 items a market:  324–332 KB a market
//	Retail, a 13k-item realm group: 1.24 MB with every field on every item
var limits = map[string]limit{
	"forever": {data: 2 << 20, market: 400 << 10},
	"retail":  {data: 4 << 20, market: 1280 << 10},
}

// Lua writes the contract's table: one global of integers, booleans and quoted keys, nothing else.
func Lua(a Answer) []byte {
	var b strings.Builder
	b.WriteString("-- Written by the Gold Forever companion and replaced on every refresh: do not edit.\n")
	fmt.Fprintf(&b, "%s = {\n  %s = %d,\n", dataGlobal, fieldVersion, formatVersion)
	fmt.Fprintf(&b, "  %s = %d,\n", fieldAt, a.At)
	b.WriteString(sharedPart(a.Shared))
	b.WriteString(charactersPart(a.Characters))
	fmt.Fprintf(&b, "  %s = {\n", fieldMarkets)
	for _, p := range a.Markets {
		b.Write(marketPart(p))
	}
	b.WriteString("  },\n}\n")
	return []byte(b.String())
}

// marketPart is one market's entry in the markets table.
func marketPart(p Prices) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "    [%s] = {\n", quote(p.Market))
	for _, it := range p.Items {
		fmt.Fprintf(&b, "      [%d] = { %d, %d, %d, %d", it.ID, it.Value, it.MinBuyout, it.Players, it.ScannedAt)
		if it.Week > 0 {
			fmt.Fprintf(&b, ", %d", it.Week)
		}
		if it.Thin {
			fmt.Fprintf(&b, ", %s = %d", fieldUnits, it.Units)
		}
		if it.HasSold {
			fmt.Fprintf(&b, ",%s=%d", fieldSold, it.Sold)
		}
		if it.HasListed {
			fmt.Fprintf(&b, ",%s=%d", fieldListed, it.Listed)
		}
		b.WriteString(" },\n")
	}
	b.WriteString("    },\n")
	return []byte(b.String())
}

// quote writes s as a Lua string literal of printable ASCII: \\ and \" escaped, every other byte
// outside space..~ as a three-digit \ddd, so no newline, bracket trick or quote can end it early.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' || c == '"':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c > 0x7e:
			fmt.Fprintf(&b, "\\%03d", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// TOC is the data addon's table of contents for a flavour's interface number.
func TOC(iface int) []byte {
	return fmt.Appendf(nil, "## Interface: %d\n## Title: Gold Forever Data\n## Notes: Prices written by the Gold Forever companion\n\n%s\n", iface, dataFile)
}
