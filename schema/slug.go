package schema

import (
	"regexp"
	"strings"
)

var slugRegex = regexp.MustCompile("[^0-9a-zA-Z]+")

// MakeSlug returns the given string in a "foo-bar-yes" format
func MakeSlug(str string) string {
	return strings.ToLower(strings.Trim(slugRegex.ReplaceAllString(str, "-"), "-"))
}
