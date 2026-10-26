package addondata

import (
	"fmt"
	"strings"
)

// limit is a game's Data.lua limits (D-013.9): over data bytes the file is refused and the last good
// one stays; a market over market bytes is written with a warning.
type limit struct{ data, market int }

// Retail's markets are realm groups of 8–13k items, about 52 bytes each in Lua and 8 more
// with a week mean (a 13k-item market: 678 KB, 781 KB with one on every item).
var limits = map[string]limit{
	"forever": {data: 1 << 20, market: 200 << 10},
	"retail":  {data: 4 << 20, market: 1 << 20},
}

// Lua writes the contract's table: one global of integers, booleans and quoted keys, nothing else.
func Lua(a Answer) []byte {
	var b strings.Builder
	b.WriteString("-- Written by the Gold Forever companion and replaced on every refresh: do not edit.\n")
	b.WriteString("GoldForever_Prices = {\n  v = 1,\n")
	fmt.Fprintf(&b, "  at = %d,\n", a.At)
	b.WriteString(sharedPart(a.Shared))
	b.WriteString(dingsPart(a.Dings))
	b.WriteString("  markets = {\n")
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
	return fmt.Appendf(nil, "## Interface: %d\n## Title: Gold Forever Data\n## Notes: Prices written by the Gold Forever companion\n\nData.lua\n", iface)
}
