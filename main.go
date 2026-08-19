package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"golang.org/x/term"

	"ku-crud/internal/api"
	"ku-crud/internal/meta"
)

//go:embed all:web/dist
var dist embed.FS

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	data := flag.String("data", "ku-crud.db", "sqlite metadata path")
	createUser := flag.String("create-user", "", "create a user then exit")
	flag.Parse()

	store, err := meta.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	if *createUser != "" {
		fmt.Print("password: ")
		pw, _ := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if len(pw) < 4 {
			log.Fatal("password must be at least 4 characters")
		}
		if err := store.CreateUser(*createUser, string(pw)); err != nil {
			log.Fatal(err)
		}
		fmt.Println("user created")
		return
	}

	srv := api.New(store)
	mux := srv.Routes()
	static, _ := fs.Sub(dist, "web/dist")
	mux.Handle("/", http.FileServer(http.FS(static)))

	log.Printf("ku-crud listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
