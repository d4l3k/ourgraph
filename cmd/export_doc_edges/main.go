package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/d4l3k/ourgraph/db"
	"github.com/d4l3k/ourgraph/schema"
	"github.com/dgraph-io/dgo"
	"github.com/dgraph-io/dgo/protos/api"
	"golang.org/x/sync/errgroup"
)

var (
	out   = flag.String("dir", "data/ourgraph", "file to write out to")
	debug = flag.Bool("debug", false, "whether to run with a very small amount of data")
)

func main() {
	flag.Parse()

	if err := run(); err != nil {
		log.Fatalf("%+v", err)
	}
}

func run() error {
	ctx := context.Background()

	if err := os.MkdirAll(*out, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(*out, "docgraph.json.gz"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer f.Close()

	w := gzip.NewWriter(f)
	defer w.Close()
	jsonenc := json.NewEncoder(w)

	conn, err := db.NewConn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	dc := api.NewDgraphClient(conn)
	dgo := dgo.NewDgraphClient(dc)

	txn := dgo.NewReadOnlyTxn().BestEffort()

	userCount := 1000

	if !*debug {
		log.Printf("fetching doc and user counts...")
		resp, err := txn.Query(ctx, `{
			docs(func: has(url)) {
				count(uid)
			}
		}`)
		if err != nil {
			return err
		}
		results := struct {
			Docs []struct {
				Count int `json:"count"`
			} `json:"docs"`
		}{}
		if err := json.Unmarshal(resp.Json, &results); err != nil {
			return err
		}
		userCount = results.Docs[0].Count
		log.Printf("user info %+v", results)
	}

	var fmu sync.Mutex

	const workers = 16
	const batchSize = 1

	offsetChan := make(chan int, workers)

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		defer close(offsetChan)
		for offset := 0; offset < userCount; offset += batchSize {
			offsetChan <- offset
		}
		return nil
	})

	dict := schema.DocGraph{}
	docIDs := map[string]int{}

	docID := func(uid string) int {
		id, ok := docIDs[uid]
		if ok {
			return id
		}
		id = len(dict.Docs)
		docIDs[uid] = id
		return id
	}

	processResults := func(docs []schema.Document) error {
		fmu.Lock()
		defer fmu.Unlock()

		for _, a := range docs {
			aid := docID(a.Uid)
			for _, user := range a.Likes {
				for _, b := range user.Likes {
					bid := docID(b.Uid)
					dict.Edges = append(dict.Edges, []int{aid, bid})
				}
			}
		}

		return nil
	}

	for i := 0; i < workers; i++ {
		eg.Go(func() error {
			for offset := range offsetChan {
				log.Printf("fetching offset = %d...", offset)
				resp, err := txn.Query(
					ctx,
					fmt.Sprintf(
						`{
							docs(func: has(url), first: %d, offset: %d) @ignorereflex {
								uid
								likes (first: 100) {
									~likes (first: 100)  {
										uid
									}
								}
							}
						}`,
						batchSize, offset,
					),
				)
				if err != nil {
					return err
				}
				results := struct {
					Docs []schema.Document `json:"docs"`
				}{}
				if err := json.Unmarshal(resp.Json, &results); err != nil {
					return err
				}

				if err := processResults(results.Docs); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	return jsonenc.Encode(dict)
}
