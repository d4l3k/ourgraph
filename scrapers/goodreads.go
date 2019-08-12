package scrapers

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"sort"
	"strconv"
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
	s.limit = ratelimit.New(5)

	var eg errgroup.Group

	ids := make(chan int, 100)
	eg.Go(func() error {
		return errors.Wrapf(s.idGenerator(ctx, ids), "idGenerator")
	})

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

type goodreadsSiteMapIndex struct {
	Maps []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"sitemap"`
}

type goodreadsSiteMap struct {
	URLs []struct {
		Loc        string `xml:"loc"`
		LastMod    string `xml:"lastmod"`
		ChangeFreq string `xml:"changefreq"`
	} `xml:"url"`
}

func (s GoodreadsScraper) getSiteMapIndex() (goodreadsSiteMapIndex, error) {
	const sitemapIndexUrl = "https://www.goodreads.com/siteindex.user.xml"

	s.limit.Take()

	resp, err := http.Get(sitemapIndexUrl)
	if err != nil {
		return goodreadsSiteMapIndex{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return goodreadsSiteMapIndex{}, errors.Errorf("invalid status code %+v", resp.StatusCode)
	}

	var idx goodreadsSiteMapIndex
	if err := xml.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return goodreadsSiteMapIndex{}, err
	}
	return idx, nil
}

func (s GoodreadsScraper) getSiteMap(url string) (goodreadsSiteMap, error) {
	s.limit.Take()

	resp, err := http.Get(url)
	if err != nil {
		return goodreadsSiteMap{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return goodreadsSiteMap{}, errors.Errorf("invalid status code %+v", resp.StatusCode)
	}

	var reader io.Reader = resp.Body
	/*
		if strings.HasSuffix(url, ".gz") {
			gzReader, err := gzip.NewReader(reader)
			if err != nil {
				return goodreadsSiteMap{}, err
			}
			reader = gzReader
		}
	*/

	var idx goodreadsSiteMap
	if err := xml.NewDecoder(reader).Decode(&idx); err != nil {
		return goodreadsSiteMap{}, err
	}
	return idx, nil
}

var goodreadsUserIDRegexp = regexp.MustCompile(`https://www.goodreads.com/user/show/(\d+)`)

func (s GoodreadsScraper) idGenerator(ctx context.Context, ids chan int) error {
	defer close(ids)

	// First fetch the users id from the site maps.
	idx, err := s.getSiteMapIndex()
	if err != nil {
		return err
	}
	if len(idx.Maps) == 0 {
		return errors.Errorf("got empty sitemapindex")
	}
	// randomize order of maps
	rand.Shuffle(len(idx.Maps), func(i, j int) {
		idx.Maps[i], idx.Maps[j] = idx.Maps[j], idx.Maps[i]
	})

	for _, m := range idx.Maps {
		siteMap, err := s.getSiteMap(m.Loc)
		if err != nil {
			return errors.Wrapf(err, "fetching sitemap %q", m.Loc)
		}
		if len(siteMap.URLs) == 0 {
			return errors.Errorf("got empty sitemap %q", m.Loc)
		}

		// randomize order of urls
		rand.Shuffle(len(siteMap.URLs), func(i, j int) {
			siteMap.URLs[i], siteMap.URLs[j] = siteMap.URLs[j], siteMap.URLs[i]
		})

		for _, url := range siteMap.URLs {
			matches := goodreadsUserIDRegexp.FindStringSubmatch(url.Loc)
			if len(matches) != 2 {
				return errors.Errorf("couldn't find user ID in url %q: %+v", url.Loc, matches)
			}
			id, err := strconv.Atoi(matches[1])
			if err != nil {
				return err
			}
			if id == 0 {
				return errors.Errorf("got invalid user ID in url %q: %d", url.Loc, id)
			}
			select {
			case <-ctx.Done():
				return nil
			case ids <- id:
			}
		}
	}

	// Fall back to random guessing.
	count, err := s.userCount()
	if err != nil {
		return err
	}
	log.Printf("user count = %d", count)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ids <- rand.Intn(count):
		}
	}
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

	u := schema.User{
		Urls: []string{profileURL},
	}

	if og.Type == "profile" {
		if og.Profile == nil {
			return schema.User{}, errors.Errorf("missing profile metadata for user %q", profileURL)
		}
		u.Name = strings.TrimSpace(og.Profile.FirstName + " " + og.Profile.LastName)
		u.Username = og.Profile.Username
	} else if og.Type == "books.author" {
		u.Name = og.Title
	} else {
		return schema.User{}, errors.Errorf("unknown page type %q", og.Type)
	}

	if len(u.Username) == 0 {
		u.Username = fmt.Sprintf("%d-%s", id, u.Name)
	}
	u.Username = schema.MakeSlug(u.Username)

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
