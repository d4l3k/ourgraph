package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/d4l3k/ourgraph/schema"
	"github.com/dgraph-io/dgo"
	"github.com/dgraph-io/dgo/protos/api"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
)

var (
	port       = flag.String("port", "6060", "port to run on")
	dgraphAddr = flag.String("dgraphaddr", "localhost:9080", "address of the dgraph instance")
)

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

func (s *server) handleRecommendation(r *http.Request) (interface{}, error) {
	id := r.FormValue("id")
	limit := requestFormInt(r, "limit", 100)
	offset := requestFormInt(r, "offset", 0)
	if limit > 200 || limit < 0 {
		return nil, errors.Errorf("limit must be  <= 200 && >= 0")
	}
	if offset < 0 {
		return nil, errors.Errorf("offset must be  >= 0")
	}
	return s.recommendations(r.Context(), id, limit, offset)
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
	Score    float32
	Document schema.Document
}

type response struct {
	Documents       []schema.Document
	Recommendations []recommendation
}

func splitURLs(urls string) []string {
	arr := strings.Split(urls, "|")
	for i, url := range arr {
		arr[i] = strings.ToLower(strings.TrimSpace(url))
	}
	return arr
}

func getDocsByURL(ctx context.Context, txn *dgo.Txn, url string) ([]schema.Document, error) {
	return getDocsInternal(
		ctx,
		txn,
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
			}
		}`,
		map[string]string{"$url": url},
	)
}

var uidRegex = regexp.MustCompile("^0x[0-9a-fA-F]+$")

func validUID(uid string) bool {
	return uidRegex.MatchString(uid)
}

func getDocsByUIDs(ctx context.Context, txn *dgo.Txn, uids []string) ([]schema.Document, error) {
	// This method is pretty dangerous since it's doing raw template manipulation.
	for _, uid := range uids {
		if !validUID(uid) {
			return nil, errors.Errorf("invalid uid %q", uid)
		}
	}
	return getDocsInternal(
		ctx,
		txn,
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
				}
			}`,
			strings.Join(uids, ","),
		),
		map[string]string{},
	)
}

func getDocsInternal(ctx context.Context, txn *dgo.Txn, query string, params map[string]string) ([]schema.Document, error) {
	resp, err := txn.QueryWithVars(ctx, query, params)
	if err != nil {
		return nil, errors.Wrapf(err, "QueryWithVars")
	}
	results := struct {
		Docs []schema.Document `json:"docs"`
	}{}
	if err := json.Unmarshal(resp.Json, &results); err != nil {
		return nil, errors.Wrapf(err, "error unmarshalling dgraph response")
	}
	return results.Docs, nil
}

func (s *server) recommendations(ctx context.Context, id string, limit, offset int) (response, error) {
	urls := splitURLs(id)
	txn := s.dgo.NewReadOnlyTxn().BestEffort()

	// Fetch documents first
	var docs []schema.Document
	for _, url := range urls {
		doc, err := getDocsByURL(ctx, txn, url)
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
		for offset := 0; offset < 1000000; offset += 1000 {
			resp, err := txn.QueryWithVars(ctx,
				`query recs($url: string, $offset: int) {
				recs(func: eq(url, $url)) @ignorereflex {
					~likes (first: 1000, offset: $offset) {
						likes {
							uid
						}
					}
				}
			}
		`,
				map[string]string{
					"$url":    url,
					"$offset": strconv.Itoa(offset),
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
					for _, doc := range user.Likes {
						if doc.Uid == "" {
							return response{}, errors.Errorf("invalid uid for doc %+v", doc)
						}
						rec := recs[doc.Uid]
						rec.Score += 1.0
						rec.Document = doc
						recs[doc.Uid] = rec
					}
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
	recDocs, err := getDocsByUIDs(ctx, txn, uids)
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
	return response{
		Documents:       docs,
		Recommendations: recList,
	}, nil
}

type server struct {
	dc  api.DgraphClient
	dgo *dgo.Dgraph
}

func run() error {
	log.SetFlags(log.Flags() | log.Lshortfile)
	flag.Parse()

	conn, err := grpc.Dial(
		*dgraphAddr,
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(100*1000*1000)),
	)
	if err != nil {
		return err
	}
	defer conn.Close()
	var s server
	s.dc = api.NewDgraphClient(conn)
	s.dgo = dgo.NewDgraphClient(s.dc)

	fs := http.FileServer(http.Dir("ui"))
	http.Handle("/static/", fs)

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/v1/recommendation", jsonHandler(s.handleRecommendation))

	log.Printf("Serving on :%s...", *port)

	return http.ListenAndServe("0.0.0.0:"+*port, nil)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
