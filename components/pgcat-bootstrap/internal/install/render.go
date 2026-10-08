package install

import "strconv"

// Go quoted strings share escapes used here with TOML basic strings.
// Replace Go-only hex escapes to TOML unicode escapes for control characters.
func quote(s string) string {
	q := strconv.Quote(s)
	out := ""
	for i := 0; i < len(q); i++ {
		if q[i] == '\\' && i+1 < len(q) {
			switch q[i+1] {
			case 'x':
				out += "\\u00" + q[i+2:i+4]
				i += 3
				continue
			case 'a':
				out += "\\u0007"
				i++
				continue
			case 'v':
				out += "\\u000b"
				i++
				continue
			}
			out += q[i : i+2]
			i++
			continue
		}
		out += q[i : i+1]
	}
	return out
}
