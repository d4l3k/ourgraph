package scrapers

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/d4l3k/ourgraph/schema"
)

func TestFFNetNormalize(t *testing.T) {
	t.Parallel()
	in := "https://www.fanfiction.net/s/13082265/1/foo-bar-1"
	want := "https://www.fanfiction.net/s/13082265"
	s := FFNetScraper{
		domain: "www.fanfiction.net",
	}
	u, err := url.Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Normalize(*u)
	if err != nil {
		t.Fatal(err)
	}
	if out != want {
		t.Fatalf("wanted %q; got %q", want, out)
	}
}

func TestDocToUser(t *testing.T) {
	t.Parallel()
	cases := []struct {
		html string
		want schema.User
	}{
		{
			`
			<div id="content_wrapper_inner"><span>Title Foo</span></div>
			<div class="favstories" data-category="Harry Potter"
			data-storyid="3449061" data-title="foo"
			data-wordcount="48219" data-datesubmit="1174348577"
			data-dateupdate="1527040475" data-ratingtimes="246" data-chapters="58"
			data-statusid="1"><a class="stitle"
			href="/s/3449061/1/foo"><img class="cimage "
			src="//foo.com/bar" width="50"
			height="66">foo</a> <a
			href="/s/3449061/58/foo"><span
			class="icon-chevron-right xicon-section-arrow"></span></a>  by <a
			href="/u/1088176/bar">bar</a>  <a class="reviews"
			href="/r/3449061/">reviews</a>
				<div class="z-indent z-padtop">Foo bar?<div class="z-padtop2
				xgray">Harry Potter - Rated: T - English - Drama - Chapters: 58 - Words:
				48,219 - Reviews: 246 - Favs: 102 - Follows: 80 - Updated: <span
				data-xutime="1527040475">May 22, 2018</span> - Published: <span
				data-xutime="1174348577">Mar 19, 2007</span>
				</div></div>
				</div>
			`,
			schema.User{
				Name: "Title Foo",
				Urls: []string{
					"https://www.fanfiction.net/u/1000",
				},
				Username: "title-foo",
				Likes: []schema.Document{
					{
						Desc:      "Foo bar?",
						Url:       "https://www.fanfiction.net/s/3449061",
						Name:      "foo",
						Created:   1174348577,
						Updated:   1527040475,
						Author:    "bar",
						Reviews:   246,
						LikeCount: 102,
						WordCount: 48219,
						Chapters:  58,
						Image:     "//foo.com/bar",
						Tags:      []string{"harry-potter", "t", "english", "drama"},
					},
				},
			},
		},
		{
			`
			<div id="content_wrapper_inner"><span>Title Foo</span></div>
			<div class="favstories" >
				<div><div>
					RWBY - Rated: T - English - Sci-Fi/Fantasy - Chapters: 58 - Words: 78,803 - Reviews: 75 - Favs: 58 - Follows: 72 - Updated: 3/30/2015 - Published: 8/23/2013 - [OC, Blake B.] Ruby R., Weiss S.
				</div></div>
				</div>
			</div>
			`,
			schema.User{
				Name: "Title Foo",
				Urls: []string{
					"https://www.fanfiction.net/u/1000",
				},
				Username: "title-foo",
				Likes: []schema.Document{
					{
						Url:       "https://www.fanfiction.net/s/0",
						LikeCount: 58,
						Tags:      []string{"t", "english", "sci-fi", "fantasy", "oc", "blake-b", "ruby-r", "weiss-s"},
					},
				},
			},
		},
	}

	scraper := FFNetScraper{
		domain: "www.fanfiction.net",
	}
	urlStr := scraper.userURL(1000)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		t.Fatal(err)
	}

	for i, c := range cases {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(c.html))
		if err != nil {
			t.Fatal(err)
		}
		doc.Url = parsedURL

		out, err := scraper.docToUser(doc)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(c.want, out) {
			t.Errorf("%d. docToUser(%q) = \n%+v; not \n%+v", i, c.html, out, c.want)
		}
	}
}
