package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/d4l3k/ourgraph/db"
	"github.com/d4l3k/ourgraph/schema"
	"github.com/dgraph-io/dgo"
	"github.com/dgraph-io/dgo/protos/api"
	"golang.org/x/sync/errgroup"
	"gonum.org/v1/hdf5"
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

type dictionary struct {
	Relations []string `json:"relations"`
	Entities  struct {
		User []string `json:"user"`
		Doc  []string `json:"doc"`
	} `json:"entities"`
}

func hdf5SetIntAttr(g *hdf5.Group, key string, val int) error {
	scalar, err := hdf5.CreateDataspace(hdf5.S_SCALAR)
	if err != nil {
		return err
	}
	defer scalar.Close()
	attr, err := g.CreateAttribute(key, hdf5.T_NATIVE_INT, scalar)
	if err != nil {
		return err
	}
	defer attr.Close()
	if err := attr.Write(&val, hdf5.T_NATIVE_INT); err != nil {
		return err
	}
	return nil
}

func run() error {
	ctx := context.Background()

	if err := os.MkdirAll(*out, 0755); err != nil {
		return err
	}

	f, err := hdf5.CreateFile(filepath.Join(*out, "edges_0_0.h5"), hdf5.F_ACC_TRUNC)
	if err != nil {
		return err
	}
	defer f.Close()

	g, err := f.OpenGroup("/")
	if err != nil {
		return err
	}
	defer g.Close()

	if err := hdf5SetIntAttr(g, "format_version", 1); err != nil {
		return err
	}

	const chunkSize = 10000
	const compress = hdf5.NoCompression

	lhs, err := f.CreateTableFrom("lhs", int32(0), chunkSize, compress)
	if err != nil {
		return err
	}
	defer lhs.Close()
	rel, err := f.CreateTableFrom("rel", int32(0), chunkSize, compress)
	if err != nil {
		return err
	}
	defer rel.Close()
	rhs, err := f.CreateTableFrom("rhs", int32(0), chunkSize, compress)
	if err != nil {
		return err
	}
	defer rhs.Close()

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
		userCount = results.Users[0].Count
	}

	var fmu sync.Mutex

	const workers = 16
	const batchSize = 10000

	offsetChan := make(chan int, workers)

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		defer close(offsetChan)
		for offset := 0; offset < userCount; offset += batchSize {
			offsetChan <- offset
		}
		return nil
	})

	dict := dictionary{
		Relations: []string{"l"},
	}

	userIDs := map[string]int32{}
	docIDs := map[string]int32{}

	processResults := func(users []schema.User) error {
		fmu.Lock()
		defer fmu.Unlock()

		var lhsData []interface{}
		var relData []interface{}
		var rhsData []interface{}

		for _, u := range users {
			uid, ok := userIDs[u.Uid]
			if !ok {
				uid = int32(len(dict.Entities.User))
				userIDs[u.Uid] = uid
				dict.Entities.User = append(dict.Entities.User, u.Uid)
			}

			for _, d := range u.Likes {
				did, ok := docIDs[d.Uid]
				if !ok {
					did = int32(len(dict.Entities.Doc))
					docIDs[d.Uid] = did
					dict.Entities.Doc = append(dict.Entities.Doc, d.Uid)
				}

				lhsData = append(lhsData, int32(uid))
				relData = append(relData, int32(0))
				rhsData = append(rhsData, int32(did))
				//fmt.Fprintf(f, "%s\tl\t%s\n", u.Uid, d.Uid)
				//fmt.Fprintf(f, "%s\tb\t%s\n", d.Uid, u.Uid)
			}
		}

		if err := lhs.Append(lhsData...); err != nil {
			return err
		}
		if err := rel.Append(relData...); err != nil {
			return err
		}
		if err := rhs.Append(rhsData...); err != nil {
			return err
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

				if err := processResults(results.Users); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	if err := ioutil.WriteFile(
		filepath.Join(*out, "entity_count_doc_0.txt"),
		[]byte(strconv.Itoa(len(docIDs))),
		0755,
	); err != nil {
		return err
	}

	if err := ioutil.WriteFile(
		filepath.Join(*out, "entity_count_user_0.txt"),
		[]byte(strconv.Itoa(len(userIDs))),
		0755,
	); err != nil {
		return err
	}

	df, err := os.OpenFile(
		filepath.Join(*out, "dictionary.json"),
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
		0755,
	)
	if err != nil {
		return err
	}
	defer df.Close()
	if err := json.NewEncoder(df).Encode(dict); err != nil {
		return err
	}

	return nil
}
