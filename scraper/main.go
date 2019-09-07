package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/d4l3k/ourgraph/db"
	"github.com/d4l3k/ourgraph/schema"
	"github.com/d4l3k/ourgraph/scrapers"
	"github.com/dgraph-io/dgo"
	"github.com/dgraph-io/dgo/protos/api"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

var (
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

	mu struct {
		sync.Mutex

		usernameCache map[string]string
		urlCache      map[string]string

		domainAdded map[string]int
	}
}

func (s *Server) Run(ctx context.Context) error {
	s.mu.usernameCache = map[string]string{}
	s.mu.urlCache = map[string]string{}
	s.mu.domainAdded = map[string]int{}

	conn, err := db.NewConn(ctx)
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

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		ticker := time.NewTicker(10 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return nil

			case <-ticker.C:
				s.mu.Lock()
				log.Printf("Stats %+v", s.mu.domainAdded)
				s.mu.Unlock()
			}
		}
	})

	for i := 0; i < 16; i++ {
		group.Go(func() error {
			return s.upload(ctx, users, docs)
		})
	}

	for _, s := range scrapers.Scrapers() {
		if !strings.Contains(s.Domain(), *scrapeFilter) {
			continue
		}
		log.Printf("launching scraper for %q", s.Domain())
		s := s
		group.Go(func() error {
			defer log.Printf("Scraper done %q", s.Domain())
			if err := s.Scrape(ctx, c); err != nil {
				log.Printf("scraper error %q: %+v", s.Domain(), err)
				return err
			}
			return nil
		})
	}
	return group.Wait()
}

func (s *Server) upload(ctx context.Context, users chan schema.User, docs chan schema.Document) error {
	for ctx.Err() == nil {
		if err := s.uploadSingle(ctx, users, docs); err != nil {
			log.Printf("failed to upload %+v", err)
		}
	}
	return nil
}

func (s *Server) logDomain(urlStr string) error {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.mu.domainAdded[parsed.Host] += 1
	s.mu.Unlock()
	return nil
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

	s.mu.Lock()
	uid, ok := s.mu.usernameCache[user.Username]
	s.mu.Unlock()

	if ok {
		user.Uid = uid
	} else {
		uid, err := s.getUserUid(ctx, txn, user.Username)
		if err != nil {
			return err
		}
		user.Uid = uid
	}

	for i := range user.Likes {
		doc := &user.Likes[i]
		if err := s.populateUidsDocument(ctx, txn, doc); err != nil {
			return errors.Wrapf(err, "doc %+v", doc)
		}
	}

	return nil
}

func (s *Server) getUserUid(ctx context.Context, txn *dgo.Txn, username string) (string, error) {
	if len(username) == 0 {
		return "", errors.Errorf("empty username")
	}
	resp, err := txn.QueryWithVars(
		ctx,
		`query useruid($username: string) {
				users(func: eq(username, $username)) {
					uid
				}
			}`,
		map[string]string{"$username": username},
	)
	if err != nil {
		return "", err
	}
	var results struct {
		Users []schema.User `json:"users"`
	}
	if err := json.Unmarshal(resp.Json, &results); err != nil {
		return "", err
	}
	if len(results.Users) > 1 {
		toDelete := results.Users[1:]

		s.mu.Lock()
		s.mu.usernameCache = map[string]string{}
		s.mu.Unlock()

		//log.Printf("deleting users %+v", toDelete)
		deleteJson, err := json.Marshal(toDelete)
		if err != nil {
			return "", err
		}
		if _, err := txn.Mutate(ctx, &api.Mutation{
			DeleteJson: deleteJson,
		}); err != nil {
			return "", err
		}
	}
	if len(results.Users) == 0 {
		return "", nil
	}
	uid := results.Users[0].Uid
	if len(uid) == 0 {
		return "", errors.Errorf("returned uid invalid! %q", username)
	}
	return uid, nil
}

func (s *Server) getDocUid(ctx context.Context, txn *dgo.Txn, url string) (string, error) {
	if len(url) == 0 {
		return "", errors.Errorf("empty url")
	}
	resp, err := txn.QueryWithVars(
		ctx,
		`query docuid($url: string) {
				docs(func: eq(url, $url)) {
					uid
				}
			}`,
		map[string]string{"$url": url},
	)
	if err != nil {
		return "", err
	}
	var results struct {
		Docs []schema.Document `json:"docs"`
	}
	if err := json.Unmarshal(resp.Json, &results); err != nil {
		return "", err
	}
	// if there are too many docs delete all the extras
	if len(results.Docs) > 1 {
		toDelete := results.Docs[1:]

		s.mu.Lock()
		s.mu.urlCache = map[string]string{}
		s.mu.Unlock()

		log.Printf("deleting docs %+v", toDelete)
		deleteJson, err := json.Marshal(toDelete)
		if err != nil {
			return "", err
		}
		if _, err := txn.Mutate(ctx, &api.Mutation{
			DeleteJson: deleteJson,
		}); err != nil {
			return "", err
		}
	}
	if len(results.Docs) == 0 {
		return "", nil
	}
	uid := results.Docs[0].Uid
	if len(uid) == 0 {
		return "", errors.Errorf("returned uid invalid! %q", url)
	}
	return uid, nil
}

func (s *Server) populateUidsDocument(ctx context.Context, txn *dgo.Txn, doc *schema.Document) error {
	if len(doc.Url) == 0 {
		return errors.Errorf("invalid url %q", doc.Url)
	}
	if len(doc.Name) == 0 {
		return errors.Errorf("document missing name")
	}

	s.mu.Lock()
	uid, ok := s.mu.urlCache[doc.Url]
	s.mu.Unlock()

	if ok {
		doc.Uid = uid
	} else {
		uid, err := s.getDocUid(ctx, txn, doc.Url)
		if err != nil {
			return err
		}
		doc.Uid = uid
	}

	for i := range doc.Likes {
		user := &doc.Likes[i]
		if err := s.populateUidsUser(ctx, txn, user); err != nil {
			return errors.Wrapf(err, "user %+v", user)
		}
	}

	return nil
}

func (s *Server) uploadSingle(
	ctx context.Context, users chan schema.User, docs chan schema.Document,
) error {
	if len(docs) == cap(docs) || len(users) == cap(users) {
		log.Printf("uploader overloaded!")
	}

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

	s.mu.Lock()
	s.mu.urlCache[doc.Url] = uid
	s.mu.Unlock()

	if err := s.logDomain(doc.Url); err != nil {
		return err
	}

	return nil
}

func (s *Server) uploadUser(ctx context.Context, user schema.User) error {
	if len(user.Likes) == 0 {
		//return errors.Errorf("user has no likes %q %+v", user.Username, user.Urls)
		return nil
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

	s.mu.Lock()
	s.mu.usernameCache[user.Username] = uid
	s.mu.Unlock()

	for _, url := range user.Urls {
		if err := s.logDomain(url); err != nil {
			return err
		}
	}

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
