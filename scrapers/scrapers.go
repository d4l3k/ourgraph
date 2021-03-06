package scrapers

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"sync"

	"github.com/d4l3k/ourgraph/schema"
)

var scrapers struct {
	sync.Mutex

	scrapers []Scraper
}

func addScraper(scraper Scraper) {
	scrapers.Lock()
	defer scrapers.Unlock()

	scrapers.scrapers = append(scrapers.scrapers, scraper)
}

func Scrapers() []Scraper {
	scrapers.Lock()
	defer scrapers.Unlock()

	return append([]Scraper{}, scrapers.scrapers...)
}

type Consumer struct {
	Users     chan<- schema.User
	Documents chan<- schema.Document
}

type Scraper interface {
	Domain() string
	Scrape(ctx context.Context, c Consumer) error
	Normalize(url url.URL) (string, error)
	Links(doc schema.Document) ([]schema.Link, error)
}

var nonNumbersRegex = regexp.MustCompile(`\D+`)

func atoi(s string) int {
	s = nonNumbersRegex.ReplaceAllString(s, "")
	i, _ := strconv.Atoi(s)
	return i
}
