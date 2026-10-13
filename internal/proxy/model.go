package proxy

import "strings"

// pickModel chooses the EVREN model for a request. A name that EVREN offers is
// used as it is; a name that contains "haiku" becomes the small model; any
// other name becomes the default. The chosen model is what the response names.
func pickModel(requested string, models []string, def, small string) string {
	for _, m := range models {
		if m == requested {
			return requested
		}
	}
	if strings.Contains(requested, "haiku") {
		return small
	}
	return def
}
