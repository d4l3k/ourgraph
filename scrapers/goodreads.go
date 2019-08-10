package scrapers

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"strings"

	"github.com/d4l3k/ourgraph/schema"
	"github.com/dyatlov/go-opengraph/opengraph"
	"github.com/pkg/errors"
	"go.uber.org/ratelimit"
	"golang.org/x/sync/errgroup"
)

func init() {
	addScraper(&GoodreadsScraper{})
}

type GoodreadsScraper struct {
	count int
	limit ratelimit.Limiter
}

func (GoodreadsScraper) Domain() string {
	return "www.goodreads.com"
}

func (s GoodreadsScraper) userRSSURL(id int) string {
	return fmt.Sprintf("https://www.goodreads.com/review/list_rss/%d?shelf=read", id)
}

func (s GoodreadsScraper) userProfileURL(id int) string {
	return fmt.Sprintf("https://www.goodreads.com/user/show/%d", id)
}

func (s GoodreadsScraper) storyURL(id int) string {
	return fmt.Sprintf("https://www.goodreads.com/book/show/%d", id)
}

func (s GoodreadsScraper) userExists(id int) (bool, error) {
	s.limit.Take()

	resp, err := http.Get(s.userRSSURL(id))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusNotFound {
		return false, errors.Errorf("got invalid status %s", resp.Status)
	}

	return resp.StatusCode != http.StatusNotFound, nil
}

func (s GoodreadsScraper) userCountUpperBound() (int, error) {
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

func (s GoodreadsScraper) userCount() (int, error) {
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

func (s *GoodreadsScraper) Scrape(ctx context.Context, c Consumer) error {
	s.limit = ratelimit.New(1)
	count, err := s.userCount()
	if err != nil {
		return err
	}
	s.count = count
	log.Printf("user count = %d", s.count)

	ids := make(chan int, 100)
	go func() {
		defer close(ids)

		for {
			select {
			case <-ctx.Done():
				return
			case ids <- rand.Intn(s.count):
			}
		}
	}()

	var eg errgroup.Group

	for i := 0; i < 10; i++ {
		eg.Go(func() error {
			return s.worker(ctx, c, ids)
		})
	}

	return eg.Wait()
}

func (s GoodreadsScraper) worker(ctx context.Context, c Consumer, ids <-chan int) error {
	for id := range ids {
		user, err := s.fetchUser(ctx, id)
		if err != nil {
			log.Printf("failed to fetch user id %d: %s", id, err)
			continue
		}
		c.Users <- user
	}
	return nil
}

type goodreadsXML struct {
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title        string `xml:"title"`
			BookId       int    `xml:"book_id"`
			BookImageUrl string `xml:"book_image_url"`
			BookDesc     string `xml:"book_description"`
			UserRating   int    `xml:"user_rating"`
			UserShelves  string `xml:"user_shelves"`
		} `xml:"item"`
	} `xml:"channel"`
}

func (s GoodreadsScraper) getRSSFeed(url string) (goodreadsXML, error) {
	s.limit.Take()

	resp, err := http.Get(url)
	if err != nil {
		return goodreadsXML{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return goodreadsXML{}, errors.Errorf("got invalid status for %q: %s", url, resp.Status)
	}

	var out goodreadsXML
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		return goodreadsXML{}, err
	}
	return out, nil
}

func (s GoodreadsScraper) getOG(url string) (*opengraph.OpenGraph, error) {
	s.limit.Take()

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("got invalid status for %q: %s", url, resp.Status)
	}
	og := opengraph.NewOpenGraph()
	if err := og.ProcessHTML(resp.Body); err != nil {
		return nil, err
	}
	return og, nil
}

func (s GoodreadsScraper) fetchUser(ctx context.Context, id int) (schema.User, error) {
	feed, err := s.getRSSFeed(s.userRSSURL(id))
	if err != nil {
		return schema.User{}, errors.Wrapf(err, "getRSS")
	}

	profileURL := s.userProfileURL(id)
	og, err := s.getOG(profileURL)
	if err != nil {
		return schema.User{}, errors.Wrapf(err, "getOG")
	}

	if og.Profile == nil {
		return schema.User{}, errors.Errorf("missing profile metadata for user %q", profileURL)
	}

	u := schema.User{
		Name: strings.TrimSpace(og.Profile.FirstName + " " + og.Profile.LastName),
		Urls: []string{profileURL},
	}

	username := og.Profile.Username
	if len(username) == 0 {
		username = fmt.Sprintf("%d-%s", id, u.Name)
	}
	u.Username = schema.MakeSlug(username)

	for _, item := range feed.Channel.Items {
		var d schema.Document
		d.Name = item.Title
		d.Desc = item.BookDesc
		if item.BookId == 0 {
			return schema.User{}, errors.Errorf("invalid book id")
		}
		d.Url = s.storyURL(item.BookId)
		d.Tags = schema.SplitTags(item.UserShelves)
		d.LikesRating = item.UserRating
		d.Image = item.BookImageUrl

		u.Likes = append(u.Likes, d)
	}

	return u, nil
}
