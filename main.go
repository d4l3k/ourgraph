package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/d4l3k/ourgraph/schema"
	"github.com/d4l3k/ourgraph/scrapers"
	"github.com/dgraph-io/dgo"
	"github.com/dgraph-io/dgo/protos/api"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

var (
	dgraphAddr   = flag.String("dgraphaddr", "localhost:9080", "address of the dgraph instance")
	scrapeFilter = flag.String("scrapefilter", "", "scrape only matching domains")
)

func main() {
	flag.Parse()
	log.SetFlags(log.Flags() | log.Lshortfile)
	rand.Seed(time.Now().Unix())

	s := Server{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Run(ctx); err != nil {
		log.Fatalf("%+v", err)
	}
}

type Server struct {
	dc  api.DgraphClient
	dgo *dgo.Dgraph

	usernameCache map[string]string
	urlCache      map[string]string
}

func (s *Server) Run(ctx context.Context) error {
	s.usernameCache = map[string]string{}
	s.urlCache = map[string]string{}

	conn, err := grpc.Dial(*dgraphAddr, grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()
	s.dc = api.NewDgraphClient(conn)
	s.dgo = dgo.NewDgraphClient(s.dc)

	users := make(chan schema.User, 100)
	docs := make(chan schema.Document, 100)
	c := scrapers.Consumer{
		Users:     users,
		Documents: docs,
	}

	go s.upload(ctx, users, docs)

	group, ctx := errgroup.WithContext(ctx)
	for _, s := range scrapers.Scrapers() {
		if !strings.Contains(s.Domain(), *scrapeFilter) {
			continue
		}
		log.Printf("launching scraper for %q", s.Domain())
		s := s
		group.Go(func() error {
			defer log.Printf("Scraper done %q", s.Domain())
			return s.Scrape(ctx, c)
		})
	}
	return group.Wait()
}

func (s *Server) upload(ctx context.Context, users chan schema.User, docs chan schema.Document) {
	for ctx.Err() == nil {
		if err := s.uploadSingle(ctx, users, docs); err != nil {
			log.Printf("failed to upload %+v", err)
		}
	}
	close(docs)
	close(users)
}

func (s *Server) populateUidsUser(ctx context.Context, txn *dgo.Txn, user *schema.User) error {
	if len(user.Username) == 0 {
		return errors.Errorf("invalid username %q", user.Username)
	}
	if len(user.Urls) == 0 {
		return errors.Errorf("user missing url")
	}
	if len(user.Name) == 0 {
		return errors.Errorf("user missing name")
	}

	if uid, ok := s.usernameCache[user.Username]; ok {
		user.Uid = uid
	} else {
		resp, err := txn.QueryWithVars(
			ctx,
			`query useruid($username: string) {
				users(func: eq(username, $username)) {
					uid
				}
			}`,
			map[string]string{"$username": user.Username},
		)
		if err != nil {
			return err
		}
		results := struct {
			Users []struct {
				Uid string `json:"uid"`
			} `json:"users"`
		}{}
		if err := json.Unmarshal(resp.Json, &results); err != nil {
			return err
		}
		if len(results.Users) > 1 {
			return errors.Errorf("too many users %q: %s", user.Username, resp.Json)
		}
		if len(results.Users) > 0 {
			user.Uid = results.Users[0].Uid
			if len(user.Uid) == 0 {
				return errors.Errorf("returned uid invalid! %q", user.Username)
			}
		}
	}

	for i := range user.Likes {
		if err := s.populateUidsDocument(ctx, txn, &user.Likes[i]); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) populateUidsDocument(ctx context.Context, txn *dgo.Txn, doc *schema.Document) error {
	if len(doc.Url) == 0 {
		return errors.Errorf("invalid url %q", doc.Url)
	}
	if len(doc.Name) == 0 {
		return errors.Errorf("document missing name")
	}
	if len(doc.Desc) == 0 {
		return errors.Errorf("document missing description")
	}

	if uid, ok := s.urlCache[doc.Url]; ok {
		doc.Uid = uid
	} else {
		resp, err := txn.QueryWithVars(
			ctx,
			`query docuid($url: string) {
				docs(func: eq(url, $url)) {
					uid
				}
			}`,
			map[string]string{"$url": doc.Url},
		)
		if err != nil {
			return err
		}
		results := struct {
			Docs []struct {
				Uid string `json:"uid"`
			} `json:"docs"`
		}{}
		if err := json.Unmarshal(resp.Json, &results); err != nil {
			return err
		}
		if len(results.Docs) > 1 {
			return errors.Errorf("too many docs %q", doc.Url)
		}
		if len(results.Docs) > 0 {
			doc.Uid = results.Docs[0].Uid
			if len(doc.Uid) == 0 {
				return errors.Errorf("returned uid invalid! %q", doc.Url)
			}
		}
	}

	for i := range doc.Likes {
		if err := s.populateUidsUser(ctx, txn, &doc.Likes[i]); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) uploadSingle(
	ctx context.Context, users chan schema.User, docs chan schema.Document,
) error {
	select {
	case <-ctx.Done():
		return nil

	case doc := <-docs:
		return s.uploadDocument(ctx, doc)

	case user := <-users:
		return s.uploadUser(ctx, user)
	}
}

func (s *Server) uploadDocument(ctx context.Context, doc schema.Document) error {
	txn := s.dgo.NewTxn()
	if err := s.populateUidsDocument(ctx, txn, &doc); err != nil {
		return err
	}
	doc.Uid = "_:doc"
	assigned, err := s.applyMutation(ctx, txn, doc)
	if err != nil {
		return err
	}
	uid, ok := assigned.Uids["doc"]
	if !ok || len(uid) == 0 {
		return errors.Errorf("didn't return UID")
	}
	s.urlCache[doc.Url] = uid
	return nil
}

func (s *Server) uploadUser(ctx context.Context, user schema.User) error {
	if len(user.Likes) == 0 {
		return errors.Errorf("user has no likes %q", user.Username)
	}

	txn := s.dgo.NewTxn()
	if err := s.populateUidsUser(ctx, txn, &user); err != nil {
		return err
	}
	user.Uid = "_:user"
	assigned, err := s.applyMutation(ctx, txn, user)
	if err != nil {
		return err
	}
	uid, ok := assigned.Uids["user"]
	if !ok || len(uid) == 0 {
		return errors.Errorf("didn't return UID")
	}
	s.usernameCache[user.Username] = uid
	return nil
}

func (s *Server) applyMutation(ctx context.Context, txn *dgo.Txn, v interface{}) (*api.Assigned, error) {
	pb, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	return txn.Mutate(ctx, &api.Mutation{
		SetJson:   pb,
		CommitNow: true,
	})
}
