package main

import (
	"context"
	"log"

	"github.com/d4l3k/ourgraph/db"
	"github.com/dgraph-io/dgo"
	"github.com/dgraph-io/dgo/protos/api"
)

func main() {
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

	resp, err := txn.Query(ctx, "schema {}")
	if err != nil {
		return err
	}

	log.Printf("%s", resp.Json)

	return nil
}
