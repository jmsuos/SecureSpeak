// securespeak-server is the SecureSpeak voice server. It relays end-to-end
// encrypted voice and chat between clients and can never decrypt either.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"securespeak/internal/server"
)

func banner(token, path string) {
	fmt.Println("==================================================================")
	fmt.Println(" One-time OWNER privilege key:")
	fmt.Println()
	fmt.Println("     " + token)
	fmt.Println()
	fmt.Println(" Connect with the client, then: Admin -> Use privilege key.")
	fmt.Println(" Whoever redeems it becomes the permanent server owner, recognized")
	fmt.Println(" by their identity key. The key stops working after first use.")
	fmt.Println(" (Also saved in " + path + " until used.)")
	fmt.Println("==================================================================")
}

func main() {
	dataDir := flag.String("data", "./data", "data directory (config, certificate, channels, roles, bans)")
	setServerPw := flag.String("set-server-password", "", "require this password to connect, then exit")
	clearServerPw := flag.Bool("clear-server-password", false, "remove the server password and exit")
	showFP := flag.Bool("fingerprint", false, "print the certificate fingerprint and exit")
	resetOwner := flag.Bool("reset-owner", false, "remove the current owner and print a new one-time owner key, then exit")
	showToken := flag.Bool("owner-token", false, "print the pending (unused) owner key, if any, then exit")
	setAdmin := flag.String("set-admin-password", "", "(removed in 3.0: use privilege keys)")
	flag.Parse()

	log.SetFlags(log.LstdFlags)
	if *setAdmin != "" {
		fmt.Println("Admin passwords were replaced by privilege keys and roles in SecureSpeak 3.0.")
		fmt.Println("Use -owner-token to see the pending owner key, or -reset-owner to issue a new one.")
		os.Exit(2)
	}
	if err := os.MkdirAll(*dataDir, 0700); err != nil {
		log.Fatal(err)
	}
	cfgPath := filepath.Join(*dataDir, "config.json")
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		if err := server.SaveJSON(cfgPath, server.DefaultConfig()); err != nil {
			log.Fatal(err)
		}
		fmt.Println("First run: created", cfgPath)
	}

	if *setServerPw != "" || *clearServerPw {
		cfg, err := server.LoadConfig(cfgPath)
		if err != nil {
			log.Fatal(err)
		}
		if *setServerPw != "" {
			cfg.ServerPasswordHash = server.HashPassword(*setServerPw)
			fmt.Println("server password set")
		}
		if *clearServerPw {
			cfg.ServerPasswordHash = ""
			fmt.Println("server password removed")
		}
		if err := server.SaveJSON(cfgPath, cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	srv, err := server.New(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	tokenPath := filepath.Join(*dataDir, "owner-token.txt")
	switch {
	case *showFP:
		fmt.Println(srv.CertFP)
		return
	case *resetOwner:
		banner(srv.ResetOwner(), tokenPath)
		fmt.Println("Restart the server if it is running.")
		return
	case *showToken:
		t := srv.PendingOwnerToken()
		if t == "" {
			t = srv.EnsureOwnerToken()
		}
		if t != "" {
			fmt.Println(t)
		} else {
			fmt.Println("No pending owner key (the owner already exists). Use -reset-owner if the owner lost access.")
		}
		return
	}
	if t := srv.EnsureOwnerToken(); t != "" {
		banner(t, tokenPath)
	} else if t := srv.PendingOwnerToken(); t != "" {
		log.Printf("the server has no owner yet; unused owner key: %s", t)
	}

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		<-ch
		log.Println("shutting down")
		srv.Close()
		os.Exit(0)
	}()
	if err := srv.Run(); err != nil {
		log.Fatal(err)
	}
}
