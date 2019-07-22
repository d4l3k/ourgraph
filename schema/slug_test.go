package schema

import "testing"

func TestMakeSlug(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Foo Bar", "foo-bar"},
		{"  Foo Bar 112 ", "foo-bar-112"},
	}

	for i, c := range cases {
		out := MakeSlug(c.in)
		if out != c.want {
			t.Errorf("%d. MakeSlug(%q) = %q; not %q", i, c.in, out, c.want)
		}
	}
}
