package scrapers

import (
	"context"
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
}
