package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/d4l3k/ourgraph/schema"
	"github.com/dgraph-io/dgo"
	"github.com/dgraph-io/dgo/protos/api"
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

func (s *server) handleRecommendation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	id := r.FormValue("id")
	limit := requestFormInt(r, "limit", 100)
	offset := requestFormInt(r, "offset", 0)
	if limit > 200 || limit < 0 {
		http.Error(w, "limit must be  <= 200 && >= 0", 400)
		return
	}
	if offset < 0 {
		http.Error(w, "offset must be  >= 0", 400)
		return
	}
	resp, err := s.recommendations(r.Context(), id, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	callback := r.FormValue("callback")
	if callback != "" {
		fmt.Fprintf(w, "%s(%s)", callback, jsonBytes)
	} else {
		w.Write(jsonBytes)
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

func (s *server) recommendations(ctx context.Context, id string, limit, offset int) (response, error) {
	urls := splitURLs(id)
	recs := map[string]recommendation{}
	var docs []schema.Document
	for _, url := range urls {
		resp, err := s.dgo.NewReadOnlyTxn().BestEffort().QueryWithVars(ctx,
			`query recs($url: string) {
				docs(func: eq(url, $url)) {
					url
					title
					desc
				}

				var(func: eq(url, $url)) @ignorereflex {
					~likes {
						likes {
							uids as uid
						}
					}
				}

				var(func: uid(uids)) @groupby(uid) {
					counts as count(uid)
				}

				recs(func: uid(counts)) {
					count: val(counts)
					url
					title
					desc
				}
			}
		`,
			map[string]string{"$url": url},
		)
		if err != nil {
			return response{}, err
		}
		results := struct {
			Docs []schema.Document `json:"docs"`
			Recs []schema.Document `json:"recs"`
		}{}
		if err := json.Unmarshal(resp.Json, &results); err != nil {
			return response{}, err
		}

		log.Printf("json %s", resp.Json)

		for _, doc := range results.Docs {
			docs = append(docs, doc)
		}

		for _, doc := range results.Recs {
			rec := recs[doc.Url]
			rec.Score += 1.0
			rec.Document = doc
			recs[doc.Url] = rec
		}
	}
	var recList []recommendation
	for _, rec := range recs {
		recList = append(recList, rec)
	}
	sort.Slice(recList, func(i, j int) bool {
		return recList[i].Score >= recList[j].Score
	})
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

	conn, err := grpc.Dial(*dgraphAddr, grpc.WithInsecure())
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
	http.HandleFunc("/api/v1/recommendation", s.handleRecommendation)

	log.Printf("Serving on :%s...", *port)

	return http.ListenAndServe("0.0.0.0:"+*port, nil)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
