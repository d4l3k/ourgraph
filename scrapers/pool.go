package scrapers

import (
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/pkg/errors"
	"go.uber.org/ratelimit"
)

const HttpFetchTimeout = 1 * time.Minute

type HttpWorkerPool struct {
	client  http.Client
	wg      sync.WaitGroup
	jobs    chan string
	docs    chan *goquery.Document
	limiter ratelimit.Limiter
}

func NewHttpWorkerPool(capacity int, limiter ratelimit.Limiter) *HttpWorkerPool {
	p := &HttpWorkerPool{
		jobs:    make(chan string, capacity),
		docs:    make(chan *goquery.Document, capacity),
		limiter: limiter,
		client: http.Client{
			Timeout: HttpFetchTimeout,
		},
	}
	for i := 0; i < capacity; i++ {
		go p.worker()
	}
	return p
}

func (p *HttpWorkerPool) Close() error {
	close(p.jobs)
	p.wg.Wait()
	close(p.docs)
	return nil
}

// Output returns a channel with the fetched documents.
func (p *HttpWorkerPool) Output() <-chan *goquery.Document {
	return p.docs
}

// Schedule schedules some work to be completed. If too many things are being
// scheduled schedule will block.
// Not thread safe.
func (p *HttpWorkerPool) Schedule(url string) {
	time.Sleep(time.Now().Sub(p.limiter.Take()))
	p.jobs <- url
}

func (p *HttpWorkerPool) worker() {
	for u := range p.jobs {
		if err := p.fetch(u); err != nil {
			log.Printf("fetch failed: %s", err)
		}
	}
}

func (p *HttpWorkerPool) fetch(urlStr string) error {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return err
	}
	resp, err := p.client.Get(urlStr)
	if err != nil {
		return errors.Wrapf(err, "failed to fetch %q", urlStr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("fetch %q status = %s", urlStr, resp.Status)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return err
	}
	doc.Url = parsed
	p.docs <- doc
	return nil
}
