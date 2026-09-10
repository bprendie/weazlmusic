package main

import (
	"log"
	"net/http"
	"weazltunes.local/web/internal/testnav"
)

func main() {
	log.Print("Test fixture on 127.0.0.1:4534. alice/bob, password: test-password")
	log.Fatal(http.ListenAndServe("127.0.0.1:4534", testnav.New()))
}
