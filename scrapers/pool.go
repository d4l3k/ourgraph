package scrapers

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/pkg/errors"
	"go.uber.org/ratelimit"
	"golang.org/x/sync/errgroup"
)

const HttpFetchTimeout = 1 * time.Minute

type HttpWorkerPool struct {
	client  http.Client
	eg      *errgroup.Group
	jobs    chan string
	docs    chan *goquery.Document
	limiter ratelimit.Limiter
}

func NewHttpWorkerPool(ctx context.Context, workers int, limiter ratelimit.Limiter) *HttpWorkerPool {
	const capacity = 16

	eg, ctx := errgroup.WithContext(ctx)
	p := &HttpWorkerPool{
		jobs:    make(chan string, capacity),
		docs:    make(chan *goquery.Document, capacity),
		limiter: limiter,
		client: http.Client{
			Timeout: HttpFetchTimeout,
		},
		eg: eg,
	}
	for i := 0; i < workers; i++ {
		eg.Go(func() error {
			return p.worker(ctx)
		})
	}
	return p
}

func (p *HttpWorkerPool) Close() error {
	log.Printf("closed")
	close(p.jobs)
	defer close(p.docs)
	if err := p.eg.Wait(); err != nil {
		return err
	}
	return nil
}

// Output returns a channel with the fetched documents.
func (p *HttpWorkerPool) Output() <-chan *goquery.Document {
	return p.docs
}

// Schedule schedules some work to be completed. If too many things are being
// scheduled schedule will block.
// Not thread safe.
func (p *HttpWorkerPool) Schedule(ctx context.Context, url string) {
	select {
	case p.jobs <- url:
	case <-ctx.Done():
	}
}

func (p *HttpWorkerPool) worker(ctx context.Context) error {
	for u := range p.jobs {
		if ctx.Err() != nil {
			return nil
		}

		if err := p.fetch(ctx, u); err != nil {
			log.Printf("fetch failed: %s", err)
		}
	}
	return nil
}

func (p *HttpWorkerPool) fetch(ctx context.Context, urlStr string) error {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return err
	}
	p.limiter.Take()
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	resp, err := p.client.Do(req)
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
