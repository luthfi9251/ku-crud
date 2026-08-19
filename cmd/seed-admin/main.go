package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/term"

	"ku-crud/internal/meta"
)

// Seed an admin user into the metadata store.
//
//	go run ./cmd/seed-admin -data ku-crud.db -username admin
//
// Prompts for the password on a TTY; reads one line from stdin otherwise
// (scriptable: echo 'secret' | go run ./cmd/seed-admin ...).
func main() {
	data := flag.String("data", "ku-crud.db", "sqlite metadata path")
	username := flag.String("username", "", "user to create (required)")
	flag.Parse()
	if *username == "" {
		log.Fatal("-username is required")
	}

	store, err := meta.Open(*data)
	if err != nil {
		log.Fatal(err)
	}

	var pw []byte
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print("password: ")
		pw, _ = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
	} else {
		fmt.Fprintln(os.Stderr, "reading password from stdin")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}
		pw = []byte(strings.TrimSpace(line))
	}
	if len(pw) < 4 {
		log.Fatal("password must be at least 4 characters")
	}
	if err := store.CreateUser(*username, string(pw)); err != nil {
		log.Fatal(err)
	}
	fmt.Println("admin user created")
}
