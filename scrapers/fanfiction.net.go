package scrapers

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/d4l3k/ourgraph/schema"
	"github.com/pkg/errors"
	"go.uber.org/ratelimit"
	"golang.org/x/net/html"
)

func init() {
	addScraper(&FFNetScraper{domain: "www.fanfiction.net"})
	addScraper(&FFNetScraper{domain: "www.fictionpress.com"})
}

type FFNetScraper struct {
	domain string
	count  int
}

func (s FFNetScraper) Domain() string {
	return s.domain
}

func (FFNetScraper) Links(doc schema.Document) ([]schema.Link, error) {
	return []schema.Link{
		{
			Name: "Download ePub",
			Url:  "http://ficsave.xyz/?format=epub&e=&auto_download=yes&story_url=" + doc.Url,
		},
	}, nil
}

var pathRegexp = regexp.MustCompile(`/s/(\d+)`)

func (s FFNetScraper) Normalize(u url.URL) (string, error) {
	parts := pathRegexp.FindStringSubmatch(u.Path)
	if len(parts) != 2 {
		return "", errors.Errorf("failed to parse URL %q", u.String())
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", err
	}
	return s.storyURL(id), nil
}

func (s FFNetScraper) userExists(id int) (bool, error) {
	resp, err := http.Get(s.userURL(id))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, errors.Errorf("got invalid status %s", resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	return !bytes.Contains(body, []byte("User does not exist")), nil
}

func (s FFNetScraper) userCountUpperBound() (int, error) {
	for id := 1000000; ; id *= 2 {
		exists, err := s.userExists(id)
		if err != nil {
			return 0, err
		}
		if !exists {
			return id, nil
		}
	}
}

func (s FFNetScraper) userCount() (int, error) {
	upper, err := s.userCountUpperBound()
	if err != nil {
		return 0, err
	}
	var exists bool
	count := sort.Search(upper, func(i int) bool {
		if err != nil {
			return false
		}
		exists, err = s.userExists(i)
		return !exists
	})
	if err != nil {
		return 0, err
	}
	if count == upper {
		return 0, errors.Errorf("upper bound is too low")
	}
	return count, nil
}

func (s *FFNetScraper) Scrape(ctx context.Context, c Consumer) error {
	count, err := s.userCount()
	if err != nil {
		return err
	}
	s.count = count
	log.Printf("user count %q = %d", s.domain, s.count)
	return s.scrapeFFGroup(ctx, c)
}

func (s FFNetScraper) scrapeFFGroup(ctx context.Context, c Consumer) error {
	// Launch goroutines to fetch documents
	p := NewHttpWorkerPool(ctx, 100, ratelimit.New(5))

	// Creates jobs
	go func() {
		defer p.Close()

		for ctx.Err() == nil {
			url := fmt.Sprintf("https://%s/u/%d", s.domain, rand.Intn(s.count))
			p.Schedule(ctx, url)
		}
	}()

	// Handle fetched documents
	for doc := range p.Output() {
		if ctx.Err() != nil {
			break
		}

		user, err := s.docToUser(doc)
		if err != nil {
			log.Println(errors.Wrapf(err, "error processing document (url=%s)", doc.Url.String()))
			continue
		}
		if len(user.Name) == 0 {
			continue
		}
		c.Users <- user

	}
	return nil
}

var (
	ffFavCountRegex = regexp.MustCompile(`Favs: ([0-9,]+) -`)
	ffDescRegex     = regexp.MustCompile(
		`- Rated: (\S+) -( \S+ -)? (\S+) - Chapters: .+`,
	)
	ffCharactersRegex = regexp.MustCompile(`Published: .+ - (.+)( - Complete)?`)
	ffCharacterRegex  = regexp.MustCompile(`[\[\],]+`)
)

func atoi(s string) int {
	i, _ := strconv.Atoi(s)
	return i
}

func (s FFNetScraper) userURL(id int) string {
	return fmt.Sprintf("https://%s/u/%d", s.domain, id)
}

func (s FFNetScraper) storyURL(id int) string {
	return fmt.Sprintf("https://%s/s/%d", s.domain, id)
}

func subSelections(s *goquery.Selection) []*goquery.Selection {
	var out []*goquery.Selection
	for _, n := range s.Nodes {
		out = append(out, &goquery.Selection{Nodes: []*html.Node{n}})
	}
	return out
}

func nodeText(n *html.Node) string {
	var sb strings.Builder
	for ; n != nil; n = n.NextSibling {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
	}
	return sb.String()
}

func removeEmpty(a []string) []string {
	out := make([]string, 0, len(a))
	for _, item := range a {
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (sc FFNetScraper) docToUser(doc *goquery.Document) (schema.User, error) {
	u := schema.User{}
	contentSpans := doc.Find("#content_wrapper_inner span")
	u.Name = strings.TrimSpace(contentSpans.First().Text())
	u.Username = schema.MakeSlug(u.Name)
	u.Urls = []string{
		doc.Url.String(),
	}

	var stories []schema.Document
	for _, typ := range []string{".favstories", ".mystories"} {
		for _, s := range subSelections(doc.Find(typ)) {
			st := schema.Document{}
			id := atoi(s.AttrOr("data-storyid", ""))
			st.Url = sc.storyURL(id)
			st.Name = s.AttrOr("data-title", "")
			st.Reviews = atoi(s.AttrOr("data-ratingtimes", ""))
			st.Chapters = atoi(s.AttrOr("data-chapters", ""))
			st.WordCount = atoi(s.AttrOr("data-wordcount", ""))
			st.Complete = s.AttrOr("data-statusid", "") == "2"
			st.Image = s.Find("img").AttrOr("src", "")

			st.Created = atoi(s.AttrOr("data-datesubmit", ""))
			st.Updated = atoi(s.AttrOr("data-dateupdate", ""))
			category := s.AttrOr("data-category", "")
			if len(category) > 0 {
				st.Tags = append(st.Tags, schema.MakeSlug(category))
			}

			s.Find("a").Each(func(i int, s *goquery.Selection) {
				href := s.AttrOr("href", "")
				if strings.HasPrefix(href, "/u/") {
					st.Author = s.Text()
				}
			})

			contentDiv := s.Find("div").First()
			st.Desc = nodeText(contentDiv.Nodes[0].FirstChild)

			meta := contentDiv.Find("div").Text()
			matches := ffFavCountRegex.FindStringSubmatch(meta)
			if len(matches) == 2 {
				st.LikeCount = atoi(strings.Replace(matches[1], ",", "", -1))
			}

			matches = ffDescRegex.FindStringSubmatch(meta)
			if len(matches) == 0 {
				return schema.User{}, errors.Errorf("not enough matches %+v in %q", matches, meta)
			}
			st.Tags = append(st.Tags, schema.MakeSlug(matches[1]))
			var genreIdx int
			if len(matches) > 3 {
				st.Tags = append(st.Tags, schema.MakeSlug(matches[2]))
				genreIdx = 3
			} else {
				genreIdx = 2
			}
			genres := strings.Split(matches[genreIdx], "/")
			for _, genre := range genres {
				st.Tags = append(st.Tags, schema.MakeSlug(genre))
			}

			if matches := ffCharactersRegex.FindStringSubmatch(meta); len(matches) > 1 {
				characters := ffCharacterRegex.Split(matches[1], -1)
				for _, character := range characters {
					st.Tags = append(st.Tags, schema.MakeSlug(character))
				}
			}

			st.Tags = removeEmpty(st.Tags)
			stories = append(stories, st)
		}
	}
	u.Likes = stories

	return u, nil
}
