package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/d4l3k/ourgraph/db"
	"github.com/d4l3k/ourgraph/schema"
	"github.com/d4l3k/ourgraph/scrapers"
	"github.com/dgraph-io/dgo/v200"
	"github.com/dgraph-io/dgo/v200/protos/api"
	"github.com/pkg/errors"
)

// Average user rating for a document out of 5.
const avgRating = 4

func handleIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "ui/static/index.html")
}

func requestFormInt(r *http.Request, field string, def int) int {
	val := r.FormValue(field)
	if len(val) == 0 {
		return def
	}
	num, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return num
}

func handleRecommendation(r *http.Request) (interface{}, error) {
	id := r.FormValue("id")
	limit := requestFormInt(r, "limit", 100)
	offset := requestFormInt(r, "offset", 0)
	if limit > 200 || limit < 0 {
		return nil, errors.Errorf("limit must be  <= 200 && >= 0")
	}
	if offset < 0 {
		return nil, errors.Errorf("offset must be  >= 0")
	}

	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Minute)
	defer cancel()

	conn, err := db.NewConn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	s := server{
		scrapers: map[string]scrapers.Scraper{},
	}
	s.dc = api.NewDgraphClient(conn)
	s.dgo = dgo.NewDgraphClient(s.dc)

	for _, scraper := range scrapers.Scrapers() {
		s.scrapers[scraper.Domain()] = scraper
	}

	return s.recommendations(ctx, id, limit, offset)
}

func jsonHandler(f func(r *http.Request) (interface{}, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		resp, err := f(r)
		log.Printf("%s %s: %+v", r.Method, r.URL.Path, err)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		if _, err := w.Write(jsonBytes); err != nil {
			log.Printf("failed to write %+v", err)
		}
	}
}

type recommendation struct {
	Score    float32         `json:"score"`
	Document schema.Document `json:"document"`
	Links    []schema.Link   `json:"links"`
}

type response struct {
	Documents       []recommendation
	Recommendations []recommendation
}

func splitURLs(urls string) []string {
	arr := strings.Split(urls, "|")
	for i, url := range arr {
		arr[i] = strings.ToLower(strings.TrimSpace(url))
	}
	return arr
}

func (s *server) getDocsByURL(ctx context.Context, url string) ([]schema.Document, error) {
	return s.getDocsInternal(
		ctx,
		`query docs($url: string) {
			docs(func: eq(url, $url)) {
				uid
				url
				title
				desc
				author
				tags
				chapters
				complete
				reviews
				likecount
				isbn
			}
		}`,
		map[string]string{"$url": url},
	)
}

var uidRegex = regexp.MustCompile("^0x[0-9a-fA-F]+$")

func validUID(uid string) bool {
	return uidRegex.MatchString(uid)
}

func (s *server) getDocsByUIDs(ctx context.Context, uids []string) ([]schema.Document, error) {
	// This method is pretty dangerous since it's doing raw template manipulation.
	for _, uid := range uids {
		if !validUID(uid) {
			return nil, errors.Errorf("invalid uid %q", uid)
		}
	}
	return s.getDocsInternal(
		ctx,
		fmt.Sprintf(
			`{
				docs(func: uid(%s)) {
					uid
					url
					title
					desc
					author
					tags
					chapters
					complete
					reviews
					likecount
					isbn
					image
				}
			}`,
			strings.Join(uids, ","),
		),
		map[string]string{},
	)
}

func (s *server) getDocsInternal(ctx context.Context, query string, params map[string]string) ([]schema.Document, error) {
	resp, err := s.queryWithVars(ctx, query, params)
	if err != nil {
		return nil, err
	}
	results := struct {
		Docs []schema.Document `json:"docs"`
	}{}
	if err := json.Unmarshal(resp.Json, &results); err != nil {
		return nil, errors.Wrapf(err, "error unmarshalling dgraph response")
	}
	return results.Docs, nil
}

func (s *server) normalizeURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}
	scraper, ok := s.scrapers[u.Host]
	if !ok {
		return "", errors.Errorf("unknown website %q", u.Host)
	}
	return scraper.Normalize(*u)
}

func (s *server) queryWithVars(ctx context.Context, query string, vars map[string]string) (*api.Response, error) {
	b := backoff.WithContext(backoff.NewExponentialBackOff(), ctx)
	var resp *api.Response
	if err := backoff.Retry(func() error {
		var err error
		resp, err = s.dgo.NewReadOnlyTxn().BestEffort().QueryWithVars(
			b.Context(), query, vars,
		)
		return err
	}, b); err != nil {
		return nil, errors.Wrapf(err, "queryWithVars")
	}
	return resp, nil
}

func (s *server) recommendations(ctx context.Context, id string, limit, offset int) (response, error) {
	urls := splitURLs(id)

	for i, url := range urls {
		u, err := s.normalizeURL(url)
		if err != nil {
			return response{}, err
		}
		urls[i] = u
	}

	// Fetch documents first
	var docs []schema.Document
	for _, url := range urls {
		doc, err := s.getDocsByURL(ctx, url)
		if err != nil {
			return response{}, err
		}
		if len(doc) == 0 {
			return response{}, errors.Errorf("unknown document %q", url)
		}
		docs = append(docs, doc...)
	}

	// Fetch recommendations
	recs := map[string]recommendation{}
	for _, url := range urls {
		resp, err := s.queryWithVars(ctx,
			`query recs($url: string) {
				recs(func: eq(url, $url)) @ignorereflex {
					~likes (first: 1000) @facets(rating) {
						likes(first: 1000) @facets(rating) {
							uid
							url
						}
					}
				}
			}
		`,
			map[string]string{
				"$url": url,
			},
		)
		if err != nil {
			return response{}, err
		}
		results := struct {
			Recs []schema.Document `json:"recs"`
		}{}
		if err := json.Unmarshal(resp.Json, &results); err != nil {
			return response{}, err
		}

		if len(results.Recs) == 0 {
			break
		}

		for _, origDoc := range results.Recs {
			for _, user := range origDoc.Likes {
				userRating := float32(1.0)
				if user.LikesRating != 0 {
					userRating = float32(user.LikesRating) / avgRating
				}

				for _, doc := range user.Likes {
					// Work around @ignorereflex @facets bug in dgraph.
					if doc.Uid == "" && doc.LikesRating != 0 {
						continue
					}

					if doc.Uid == "" {
						return response{}, errors.Errorf("invalid uid for doc %+v: %s", doc, resp.Json)
					}

					docRating := float32(1.0)
					if doc.LikesRating != 0 {
						docRating = float32(doc.LikesRating) / avgRating
					}

					rec := recs[doc.Uid]
					rec.Score += userRating * docRating
					rec.Document = doc
					recs[doc.Uid] = rec
				}
			}
		}
	}
	var recList []recommendation
	for _, rec := range recs {
		recList = append(recList, rec)
	}
	sort.Slice(recList, func(i, j int) bool {
		return recList[i].Score >= recList[j].Score
	})
	if len(recList) >= offset {
		recList = recList[offset:]
	} else {
		recList = recList[:0]
	}
	if len(recList) >= limit {
		recList = recList[:limit]
	}
	var uids []string
	for _, rec := range recList {
		uids = append(uids, rec.Document.Uid)
	}
	recDocs, err := s.getDocsByUIDs(ctx, uids)
	if err != nil {
		return response{}, err
	}
	docMap := map[string]schema.Document{}
	for _, doc := range recDocs {
		docMap[doc.Uid] = doc
	}
	for i, rec := range recList {
		rec.Document = docMap[rec.Document.Uid]
		recList[i] = rec
	}

	var docList []recommendation
	for _, doc := range docs {
		docList = append(docList, recommendation{
			Document: doc,
		})
	}

	if err := s.annotateRecs(docList); err != nil {
		return response{}, err
	}
	if err := s.annotateRecs(recList); err != nil {
		return response{}, err
	}

	return response{
		Documents:       docList,
		Recommendations: recList,
	}, nil
}

func (s server) annotateRecs(recs []recommendation) error {
	for i, rec := range recs {
		u, err := url.Parse(rec.Document.Url)
		if err != nil {
			return err
		}
		s, ok := s.scrapers[u.Host]
		if !ok {
			return errors.Errorf("unknown hostname for %q", u.Host)
		}
		rec.Links, err = s.Links(rec.Document)
		if err != nil {
			return err
		}
		sort.Slice(rec.Links, func(i, j int) bool {
			return rec.Links[i].Name < rec.Links[j].Name
		})
		recs[i] = rec
	}
	return nil
}

type server struct {
	dc  api.DgraphClient
	dgo *dgo.Dgraph

	scrapers map[string]scrapers.Scraper
}

func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "s-max-age=3600, max-age=0, public")

	log.SetFlags(log.Flags() | log.Lshortfile)

	mux := http.NewServeMux()

	fs := http.FileServer(http.Dir("ui"))

	mux.Handle("/static/", fs)

	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/v1/recommendation", jsonHandler(handleRecommendation))

	mux.ServeHTTP(w, r)
}
