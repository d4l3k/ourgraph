package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/d4l3k/ourgraph/ui"
)

var bind = flag.String("bind", ":6060", "address to bind to")

func main() {
	flag.Parse()

	log.Printf("Listening %s...", *bind)
	if err := http.ListenAndServe(*bind, http.HandlerFunc(ui.Handler)); err != nil {
		log.Fatalf("%+v", err)
	}
}
