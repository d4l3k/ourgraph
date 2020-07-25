package scrapers

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/d4l3k/ourgraph/schema"
	"go.uber.org/ratelimit"
)

func TestGetLatest(t *testing.T) {
	t.Parallel()

	s := AO3Scraper{
		limiter: ratelimit.NewUnlimited,
	}
	id, err := s.getLatest()
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Errorf("id should be greater than zero: %d", id)
	}
}

func TestParseDocBookmarks(t *testing.T) {
	html := `
  <li class="user short blurb group" role="article">
		<div class="header module">
			<h5 class="byline heading">
				Bookmarked by
				<a href="/users/foo/pseuds/bar">bar (foo)</a>
			</h5>
			<p class="datetime">13 Jul 2020</p>
			<p class="status">
				 <a class="help symbol question modal" title="Bookmark symbols key" aria-controls="#modal" href="/help/bookmark-symbols-key.html"><span class="public" title="Public Bookmark"><span class="text">Public Bookmark</span></span></a>
			</p>
		</div>
	</li>
  <li class="user short blurb group" role="article">
		<div class="header module">
			<h5 class="byline heading">
				Bookmarked by
				<a href="/users/blah">blah</a>
			</h5>
			<p class="datetime">13 Jul 2020</p>
			<p class="status">
				 <a class="help symbol question modal" title="Bookmark symbols key" aria-controls="#modal" href="/help/bookmark-symbols-key.html"><span class="public" title="Public Bookmark"><span class="text">Public Bookmark</span></span></a>
			</p>
		</div>
	</li>
	`

	want := []string{
		"https://archiveofourown.org/users/foo",
		"https://archiveofourown.org/users/blah",
	}

	parsedURL, err := url.Parse("https://archiveofourown.org/works/10/bookmarks")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	doc.Url = parsedURL

	var s AO3Scraper
	out, err := s.parseDocBookmarks(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, out) {
		t.Errorf("got %+v; wanted %+v", out, want)
	}
}

func TestNormalize(t *testing.T) {
	want := "https://archiveofourown.org/works/10"
	parsedURL, err := url.Parse("https://archiveofourown.org/works/10/bookmarks?page=5")
	if err != nil {
		t.Fatal(err)
	}
	var s AO3Scraper
	out, err := s.Normalize(*parsedURL)
	if err != nil {
		t.Fatal(err)
	}
	if out != want {
		t.Errorf("got %q; want %q", out, want)
	}
}

func TestParseUserBookmarks(t *testing.T) {
	html := `
<ol class="bookmark index group">
<li id="bookmark_114516700" class="bookmark blurb group" role="article">

<p class="message">This has been deleted, sorry!</p>
<div class="new dynamic" id="bookmark_form_placement_for_114516700"></div>

<div class="user module group">
<!--bookmarker, time-->
<h5 class="byline heading">
Bookmarked by <a href="/users/SuperFan_Sone/pseuds/SuperFan_Sone/bookmarks">SuperFan_Sone</a>
</h5>
<p class="datetime">07 Sep 2016</p>

<!--meta-->


<!--notes-->

<!--navigation and actions-->
</div>


<div class="recent dynamic" id="recent_work_114516700" style="display: none;"></div>


</li>
<li id="bookmark_506838682" class="bookmark blurb group" role="article">

<!--bookmark icons-->
<p class="status" title="30 Bookmarks">
<a class="help symbol question modal" title="Bookmark symbols key" aria-controls="#modal" href="/help/bookmark-symbols-key.html"><span class="public" title="Public Bookmark"><span class="text">Public Bookmark</span></span></a>
<span class="count"><a href="/works/23320288/bookmarks">30</a></span>
</p>

<!--bookmark item module-->


<!--title, author, fandom-->
<div class="header module">

<h4 class="heading">
<a href="/works/23320288">what's in a name?</a>
by

<!-- do not cache -->
<a rel="author" href="/users/wordscorrupt/pseuds/wordscorrupt">wordscorrupt</a>





</h4>

<h5 class="fandoms heading">
<span class="landmark">Fandoms:</span>
<a class="tag" href="/tags/Marvel%20Cinematic%20Universe/works">Marvel Cinematic Universe</a>, <a class="tag" href="/tags/The%20Avengers%20(Marvel%20Movies)/works">The Avengers (Marvel Movies)</a>, <a class="tag" href="/tags/The%20Avengers%20(Marvel)%20-%20All%20Media%20Types/works">The Avengers (Marvel) - All Media Types</a>
&nbsp;
</h5>

<!--required tags-->
<ul class="required-tags">
<li> <a class="help symbol question modal" title="Symbols key" aria-controls="#modal" href="/help/symbols-key.html"><span class="rating-general-audience rating" title="General Audiences"><span class="text">General Audiences</span></span></a></li>
<li> <a class="help symbol question modal" title="Symbols key" aria-controls="#modal" href="/help/symbols-key.html"><span class="warning-choosenotto warnings" title="Choose Not To Use Archive Warnings"><span class="text">Choose Not To Use Archive Warnings</span></span></a></li>
<li> <a class="help symbol question modal" title="Symbols key" aria-controls="#modal" href="/help/symbols-key.html"><span class="category-multi category" title="Gen, M/M"><span class="text">Gen, M/M</span></span></a></li>
<li> <a class="help symbol question modal" title="Symbols key" aria-controls="#modal" href="/help/symbols-key.html"><span class="complete-yes iswip" title="Complete Work"><span class="text">Complete Work</span></span></a></li>
</ul>
<p class="datetime">25 Mar 2020</p>
</div>

<!--warnings again, cast, freeform tags-->
<h6 class="landmark heading">Tags</h6>
<ul class="tags commas">
<li class='warnings'><strong><a class="tag" href="/tags/Choose%20Not%20To%20Use%20Archive%20Warnings/works">Creator Chose Not To Use Archive Warnings</a></strong></li><li class='relationships'><a class="tag" href="/tags/Peter%20Parker%20*a*%20Tony%20Stark/works">Peter Parker &amp; Tony Stark</a></li> <li class='relationships'><a class="tag" href="/tags/Peter%20Parker%20*a*%20Steve%20Rogers/works">Peter Parker &amp; Steve Rogers</a></li> <li class='relationships'><a class="tag" href="/tags/Peter%20Parker%20*a*%20Steve%20Rogers%20*a*%20Tony%20Stark/works">Peter Parker &amp; Steve Rogers &amp; Tony Stark</a></li> <li class='relationships'><a class="tag" href="/tags/Steve%20Rogers*s*Tony%20Stark/works">Steve Rogers/Tony Stark</a></li><li class='characters'><a class="tag" href="/tags/Steve%20Rogers/works">Steve Rogers</a></li> <li class='characters'><a class="tag" href="/tags/Tony%20Stark/works">Tony Stark</a></li> <li class='characters'><a class="tag" href="/tags/Peter%20Parker/works">Peter Parker</a></li><li class='freeforms'><a class="tag" href="/tags/Parent%20Tony%20Stark/works">Parent Tony Stark</a></li> <li class='freeforms'><a class="tag" href="/tags/Parent%20Steve%20Rogers/works">Parent Steve Rogers</a></li> <li class='freeforms'><a class="tag" href="/tags/Superfamily/works">Superfamily</a></li> <li class='freeforms'><a class="tag" href="/tags/Toddler%20Peter%20Parker/works">Toddler Peter Parker</a></li> <li class='freeforms'><a class="tag" href="/tags/Precious%20Peter%20Parker/works">Precious Peter Parker</a></li> <li class='freeforms'><a class="tag" href="/tags/Fluff/works">Fluff</a></li>
</ul>

<!--summary-->
<h6 class="landmark heading">Summary</h6>
<blockquote class="userstuff summary">
<p>Three-year-old Peter figures out that his daddy has more than one name.</p>
</blockquote>

<h6 class="landmark heading">Series</h6>
<ul class="series">
<li>
Part <strong>9</strong> of <a href="/series/1387093">Be Still My Heart</a>
</li>
</ul>

<!--stats-->

<dl class="stats">
<dt class="language">Language:</dt>
<dd class="language">English</dd>
<dt class="words">Words:</dt>
<dd class="words">527</dd>
<dt class="chapters">Chapters:</dt>
<dd class="chapters">5/5</dd>



<dt class="comments">Comments:</dt>
<dd class="comments"><a href="/works/23320288?show_comments=true#comments">10</a></dd>
<dt class="kudos">Kudos:</dt>
<dd class="kudos"><a href="/works/23320288#comments">366</a></dd>
<dt class="bookmarks">Bookmarks:</dt>
<dd class="bookmarks"><a href="/works/23320288/bookmarks">30</a></dd>

<dt class="hits">Hits:</dt>
<dd class="hits">4016</dd>

</dl>
<div class="new dynamic" id="bookmark_form_placement_for_23320288"></div>

<div class="user module group">
<h5 class="byline heading">
Bookmarked by <a href="/users/blah/pseuds/blah/bookmarks">blah</a>
</h5>
<p class="datetime">10 Jun 2020</p>
</div>
<div class="recent dynamic" id="recent_work_506838682" style="display: none;"></div>
</li>
</ol>
	`

	want := schema.User{
		Name:     "foo bar_Yes1",
		Username: "foo-bar-yes1",
		Urls: []string{
			"https://archiveofourown.org/users/foo%20bar_Yes1",
		},
		Likes: []schema.Document{
			{
				Url:       "https://archiveofourown.org/works/23320288",
				Name:      "what's in a name?",
				Author:    "wordscorrupt",
				Chapters:  55,
				Reviews:   30,
				LikeCount: 366,
				WordCount: 527,
				Complete:  true,
				Desc:      "Three-year-old Peter figures out that his daddy has more than one name.",
				Tags: []string{
					"marvel-cinematic-universe", "the-avengers-marvel-movies", "the-avengers-marvel-all-media-types", "creator-chose-not-to-use-archive-warnings", "peter-parker-tony-stark", "peter-parker-steve-rogers", "peter-parker-steve-rogers-tony-stark", "steve-rogers-tony-stark", "steve-rogers", "tony-stark", "peter-parker", "parent-tony-stark", "parent-steve-rogers", "superfamily", "toddler-peter-parker", "precious-peter-parker", "fluff",
				},
			},
		},
	}
	parsedURL, err := url.Parse("https://archiveofourown.org/users/foo bar_Yes1/bookmarks")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	doc.Url = parsedURL

	var s AO3Scraper
	out, err := s.parseUserBookmarks(doc)
	if err != nil {
		t.Fatalf("%+v", err)
	}
	if !reflect.DeepEqual(want, out) {
		t.Errorf("got %+v; wanted %+v", out, want)
	}
}

func TestIncrementPageURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   "https://archiveofourown.org/users/foo/bookmarks",
			want: "https://archiveofourown.org/users/foo/bookmarks?page=2",
		},
		{
			in:   "https://archiveofourown.org/users/foo/bookmarks?page=2",
			want: "https://archiveofourown.org/users/foo/bookmarks?page=3",
		},
	}
	var s AO3Scraper
	for i, c := range cases {
		uri, err := url.Parse(c.in)
		if err != nil {
			t.Errorf("%d. failed to parse: %+v", i, err)
			continue
		}
		incremented, err := s.incrementPageURL(uri)
		if err != nil {
			t.Errorf("%d. failed to increment: %+v", i, err)
			continue
		}
		out := incremented.String()
		if c.want != out {
			t.Errorf("%d. wanted %q; got %q", i, c.want, out)
		}
	}
}
