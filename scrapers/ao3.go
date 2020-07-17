package scrapers

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/d4l3k/ourgraph/schema"
	"github.com/pkg/errors"
	"go.uber.org/ratelimit"
	"golang.org/x/time/rate"
)

func init() {
	addScraper(&AO3Scraper{
		limiter: LimiterWrapper{Limiter: rate.NewLimiter(0.9, 1)},
	})
}

type AO3Scraper struct {
	limiter ratelimit.Limiter
	count   int
}

func (s AO3Scraper) Domain() string {
	return "archiveofourown.org"
}

func (AO3Scraper) Links(doc schema.Document) ([]schema.Link, error) {
	id := filepath.Base(doc.Url)
	return []schema.Link{
		{
			Name: "Download ePub",
			Url:  fmt.Sprintf("http://download.archiveofourown.org/downloads/%s/book.epub", id),
		},
	}, nil
}

var ao3PathRegexp = regexp.MustCompile(`/works/(\d+)`)

func (s AO3Scraper) Normalize(u url.URL) (string, error) {
	parts := ao3PathRegexp.FindStringSubmatch(u.Path)
	if len(parts) != 2 {
		return "", errors.Errorf("failed to parse URL %q", u.String())
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", err
	}
	return s.storyURL(id), nil
}

func (s AO3Scraper) storyURL(id int) string {
	return fmt.Sprintf("https://archiveofourown.org/works/%d", id)
}

func (s AO3Scraper) getLatest() (int, error) {
	s.limiter.Take()
	doc, err := goquery.NewDocument("https://archiveofourown.org/works")
	if err != nil {
		return 0, err
	}
	bestTotal := 0
	doc.Find(".work .heading a").Each(func(i int, s *goquery.Selection) {
		bits := strings.Split(s.AttrOr("href", ""), "/")
		total, _ := strconv.Atoi(bits[len(bits)-1])
		if total > bestTotal {
			bestTotal = total
		}
	})
	if bestTotal == 0 {
		return 0, errors.New("failed to determine AO3 latest work")
	}
	return bestTotal, nil
}

func (s *AO3Scraper) Scrape(ctx context.Context, c Consumer) error {
	count, err := s.getLatest()
	if err != nil {
		return err
	}
	s.count = count
	log.Printf("doc count %q = %d", s.Domain(), s.count)
	return s.scrape(ctx, c)
}

func (s AO3Scraper) scrape(ctx context.Context, c Consumer) error {
	// Launch goroutines to fetch documents
	docs := NewHttpWorkerPool(ctx, 1, s.limiter)
	users := NewHttpWorkerPool(ctx, 1, s.limiter)

	// Creates jobs
	go func() {
		defer docs.Close()

		for ctx.Err() == nil {
			url := fmt.Sprintf("https://archiveofourown.org/works/%d/bookmarks", rand.Intn(s.count))
			docs.Schedule(ctx, url)
		}
	}()

	// Handle fetched documents
	go func() {
		defer users.Close()

		for doc := range docs.Output() {
			if ctx.Err() != nil {
				break
			}

			usernames, err := s.parseDocBookmarks(doc)
			if err != nil {
				log.Println(errors.Wrapf(err, "error processing document (url=%s)", doc.Url.String()))
				continue
			}
			for _, user := range usernames {
				users.Schedule(ctx, user+"/bookmarks")
			}
		}
	}()

	for doc := range users.Output() {
		if ctx.Err() != nil {
			break
		}

		user, err := s.parseUserBookmarks(doc)
		if err != nil {
			log.Printf("%+v", errors.Wrapf(err, "error processing document (url=%s)", doc.Url.String()))
			continue
		}
		c.Users <- user
	}
	return nil
}

func (sc AO3Scraper) parseDocBookmarks(doc *goquery.Document) ([]string, error) {
	var urls []string
	doc.Find(".user .heading a").Each(func(i int, s *goquery.Selection) {
		href := s.AttrOr("href", "")
		if href != "" {
			href = strings.Split(href, "/pseuds/")[0]
			urls = append(urls, href)
		}
	})
	for i, href := range urls {
		parsed, err := url.Parse(href)
		if err != nil {
			return nil, err
		}
		urls[i] = doc.Url.ResolveReference(parsed).String()
	}
	return urls, nil
}

func (sc AO3Scraper) parseUserBookmarks(doc *goquery.Document) (schema.User, error) {
	u := schema.User{}
	normalized := strings.Split(doc.Url.String(), "/bookmarks")[0]
	name, err := url.QueryUnescape(filepath.Base(normalized))
	if err != nil {
		return schema.User{}, err
	}
	u.Name = name
	u.Username = schema.MakeSlug(u.Name)
	u.Urls = []string{
		normalized,
	}

	var stories []schema.Document
	for _, s := range subSelections(doc.Find("ol.bookmark li.bookmark")) {
		if strings.Contains(s.Find(".message").Text(), "This has been deleted, sorry!") {
			continue
		}
		st := schema.Document{}
		link := s.Find(".heading a").First()
		parsedLink, err := url.Parse(link.AttrOr("href", ""))
		if err != nil {
			return schema.User{}, err
		}
		if strings.Contains(parsedLink.String(), "/series/") {
			continue
		}
		normalized, err := sc.Normalize(*doc.Url.ResolveReference(parsedLink))
		if err != nil {
			return schema.User{}, err
		}
		st.Url = normalized
		st.Name = link.Text()
		st.Author = s.Find(".heading a[rel=author]").Text()
		st.Reviews = atoi(s.Find("dd.bookmarks").Text())
		st.LikeCount = atoi(s.Find("dd.kudos").Text())
		st.Chapters = atoi(s.Find("dd.chapters").Text())
		st.WordCount = atoi(s.Find("dd.words").Text())
		st.Complete = s.Find(".complete-yes").Size() >= 1

		st.Desc = strings.TrimSpace(s.Find("blockquote").Text())

		for _, tag := range subSelections(s.Find("a.tag")) {
			st.Tags = append(st.Tags, schema.MakeSlug(tag.Text()))
		}

		st.Tags = removeEmpty(st.Tags)
		stories = append(stories, st)
	}
	u.Likes = stories

	return u, nil
}
