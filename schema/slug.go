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

func SplitTags(tags string) []string {
	parts := strings.Split(tags, ",")
	var out []string
	for _, p := range parts {
		tag := MakeSlug(p)
		if len(tag) == 0 {
			continue
		}
		out = append(out, tag)
	}
	return out
}
