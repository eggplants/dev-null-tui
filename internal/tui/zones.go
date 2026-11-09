package tui

import "os"

// zoneTable is the machine's table of IANA timezones, or nothing if it has none.
func zoneTable() string {
	for _, path := range []string{"/usr/share/zoneinfo/zone1970.tab", "/usr/share/zoneinfo/zone.tab"} {
		if data, err := os.ReadFile(path); err == nil {
			return string(data)
		}
	}

	return ""
}
