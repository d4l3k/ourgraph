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
	ClientFactory func() *http.Client
	eg            *errgroup.Group
	jobs          chan string
	priorityJobs  chan string
	docs          chan *goquery.Document
	limiter       ratelimit.Limiter
}

func NewHttpWorkerPool(ctx context.Context, workers int, limiter ratelimit.Limiter) *HttpWorkerPool {
	const capacity = 16

	client := &http.Client{
		Timeout: HttpFetchTimeout,
	}

	eg, ctx := errgroup.WithContext(ctx)
	p := &HttpWorkerPool{
		jobs:         make(chan string, capacity),
		priorityJobs: make(chan string, workers),
		docs:         make(chan *goquery.Document, capacity),
		limiter:      limiter,
		ClientFactory: func() *http.Client {
			return client
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
func (p *HttpWorkerPool) Schedule(ctx context.Context, url string) {
	select {
	case p.jobs <- url:
	case <-ctx.Done():
	}
}

// SchedulePriority schedules some work to be completed before schedule. If too
// many things are being scheduled schedule will discard
func (p *HttpWorkerPool) SchedulePriority(ctx context.Context, url string) {
	select {
	case p.priorityJobs <- url:
	case <-ctx.Done():
	default:
	}
}

func (p *HttpWorkerPool) worker(ctx context.Context) error {
	for {
		var urlStr string
		select {
		case <-ctx.Done():
			return ctx.Err()
		case urlStr = <-p.priorityJobs:
		case urlStr = <-p.jobs:
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := p.fetch(ctx, urlStr); err != nil {
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
	req.Header.Set("User-Agent", "ourgraph/1.0")
	req = req.WithContext(ctx)
	resp, err := p.ClientFactory().Do(req)
	if err != nil {
		return errors.Wrapf(err, "failed to fetch %q", urlStr)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		select {
		case p.jobs <- urlStr:
		default:
		}
		p.limiter.Take()
		p.limiter.Take()
		p.limiter.Take()
	}
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
