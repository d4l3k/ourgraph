package scrapers

import "testing"

func TestAtoi(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"12,123", 12123},
		{"", 0},
		{"123", 123},
	}

	for i, c := range cases {
		out := atoi(c.in)
		if out != c.want {
			t.Errorf("%d. atoi(%q) = %d; not %d", i, c.in, out, c.want)
		}
	}
}
