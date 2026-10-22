package prices

import (
	"fmt"
	"strings"
)

// Lua writes the contract's table: one global of integers and one quoted market key, nothing else.
func Lua(p Prices) []byte {
	var b strings.Builder
	b.WriteString("-- Written by the Gold Forever companion and replaced on every refresh: do not edit.\n")
	b.WriteString("GoldForever_Prices = {\n  v = 1,\n")
	fmt.Fprintf(&b, "  at = %d,\n  markets = {\n    [%s] = {\n", p.At, quote(p.Market))
	for _, it := range p.Items {
		fmt.Fprintf(&b, "      [%d] = { %d, %d, %d, %d },\n", it.ID, it.Value, it.MinBuyout, it.Players, it.ScannedAt)
	}
	b.WriteString("    },\n  },\n}\n")
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
