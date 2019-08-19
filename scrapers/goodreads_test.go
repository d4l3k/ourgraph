package scrapers

import (
	"encoding/xml"
	"net/url"
	"testing"
)

func TestGoodreadsNormalize(t *testing.T) {
	in := "https://www.goodreads.com/book/show/136251.Harry_Potter_and_the_Deathly_Hallows"
	want := "https://www.goodreads.com/book/show/136251"
	var s GoodreadsScraper
	u, err := url.Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Normalize(*u)
	if err != nil {
		t.Fatal(err)
	}
	if out != want {
		t.Fatalf("wanted %q; got %q", out, want)
	}
}

func TestGoodreadsXML(t *testing.T) {
	in := []byte(`
<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" >
  <channel>
    <xhtml:meta xmlns:xhtml="http://www.w3.org/1999/xhtml" name="robots" content="noindex" />
    <title>Thomas's bookshelf: read</title>
    <copyright><![CDATA[Copyright (C) 2019 Goodreads Inc. All rights reserved.]]>
    </copyright>
    <link><![CDATA[https://www.goodreads.com/review/list_rss/2018505?shelf=read]]></link>
    <atom:link href="https://www.goodreads.com/review/list_rss/2018505?shelf=read" rel="self" type="application/rss+xml" />
    <description><![CDATA[Thomas's bookshelf: read]]></description>
    <language>en-US</language>
    <lastBuildDate>Wed, 07 Aug 2019 14:51:25 -0700</lastBuildDate>
    <ttl>60</ttl>
    <image>
      <title>Thomas's bookshelf: read</title>
      <link><![CDATA[https://www.goodreads.com/review/list_rss/2018505?shelf=read]]></link>
      <width>144</width>
      <height>41</height>
      <url>https://www.goodreads.com/images/layout/goodreads_logo_144.jpg</url>
    </image>

  <item>
    <guid><![CDATA[https://www.goodreads.com/review/show/2843744549?utm_medium=api&utm_source=rss]]></guid>
    <pubDate><![CDATA[Wed, 07 Aug 2019 14:51:25 -0700]]></pubDate>
    <title>Skim</title>
    <link><![CDATA[https://www.goodreads.com/review/show/2843744549?utm_medium=api&utm_source=rss]]></link>
    <book_id>2418888</book_id>
    <book_image_url><![CDATA[https://i.gr-assets.com/images/S/compressed.photo.goodreads.com/books/1328770750l/2418888._SX50_.jpg]]></book_image_url>
    <book_small_image_url><![CDATA[https://i.gr-assets.com/images/S/compressed.photo.goodreads.com/books/1328770750l/2418888._SX50_.jpg]]></book_small_image_url>
    <book_medium_image_url><![CDATA[https://i.gr-assets.com/images/S/compressed.photo.goodreads.com/books/1328770750l/2418888._SX98_.jpg]]></book_medium_image_url>
    <book_large_image_url><![CDATA[https://i.gr-assets.com/images/S/compressed.photo.goodreads.com/books/1328770750l/2418888.jpg]]></book_large_image_url>
    <book_description><![CDATA[book desc]]></book_description>
    <book id="2418888">
      <num_pages>143</num_pages>
    </book>
    <author_name>Mariko Tamaki</author_name>
    <isbn>0888997531</isbn>
    <user_name>Thomas</user_name>
    <user_rating>3</user_rating>
    <user_read_at><![CDATA[Wed, 07 Aug 2019 16:55:23 -0700]]></user_read_at>
    <user_date_added><![CDATA[Wed, 07 Aug 2019 14:51:25 -0700]]></user_date_added>
    <user_date_created><![CDATA[Sun, 02 Jun 2019 13:02:11 -0700]]></user_date_created>
    <user_shelves><![CDATA[own-physical, graphic-novel, lgbtq, realistic-fiction, young-adult]]></user_shelves>
    <user_review><![CDATA[blah blah blah]]></user_review>
    <average_rating>3.80</average_rating>
    <book_published>2008</book_published>
    <description>
      <![CDATA[some data nice]]>
    </description>
  </item>
	</channel>
	</rss>
	`)

	var out goodreadsXML
	if err := xml.Unmarshal(in, &out); err != nil {
		t.Fatal(err)
	}
	t.Logf("out %+v", out)
	if len(out.Channel.Items) != 1 {
		t.Fatalf("wrong number of items")
	}
}
