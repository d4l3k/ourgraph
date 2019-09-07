package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/d4l3k/ourgraph/db"
	"github.com/d4l3k/ourgraph/schema"
	"github.com/dgraph-io/dgo"
	"github.com/dgraph-io/dgo/protos/api"
	"golang.org/x/sync/errgroup"
)

var out = flag.String("out", "edges.tsv", "file to write out to")

func main() {
	flag.Parse()

	if err := run(); err != nil {
		log.Fatalf("%+v", err)
	}
}

func run() error {
	ctx := context.Background()
	conn, err := db.NewConn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	dc := api.NewDgraphClient(conn)
	dgo := dgo.NewDgraphClient(dc)

	txn := dgo.NewReadOnlyTxn().BestEffort()

	resp, err := txn.Query(ctx, `{
		docs(func: has(url)) {
			count(uid)
		}
		users(func: has(username)) {
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
		Users []struct {
			Count int `json:"count"`
		} `json:"users"`
	}{}
	if err := json.Unmarshal(resp.Json, &results); err != nil {
		return err
	}
	log.Printf("user info %+v", results)

	f, err := os.OpenFile(*out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer f.Close()

	var fmu sync.Mutex

	const workers = 16
	const batchSize = 10000

	offsetChan := make(chan int, workers)

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		defer close(offsetChan)
		for offset := 0; offset < results.Users[0].Count; offset += batchSize {
			offsetChan <- offset
		}
		return nil
	})

	for i := 0; i < workers; i++ {
		eg.Go(func() error {
			for offset := range offsetChan {
				log.Printf("offset = %d", offset)
				resp, err := txn.Query(
					ctx,
					fmt.Sprintf(
						`{
					users(func: has(username), first: %d, offset: %d) {
						uid
						likes {
							uid
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
					Users []schema.User `json:"users"`
				}{}
				if err := json.Unmarshal(resp.Json, &results); err != nil {
					return err
				}
				for _, u := range results.Users {
					for _, d := range u.Likes {
						const format = "%s\tl\t%s\n"

						fmu.Lock()
						fmt.Fprintf(f, format, u.Uid, d.Uid)
						fmt.Fprintf(f, format, d.Uid, u.Uid)
						fmu.Unlock()
					}
				}
			}
			return nil
		})
	}
	return eg.Wait()
}
