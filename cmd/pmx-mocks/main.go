package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/5Sigma/pmx/internal/mock"
)

func main() {
	opts := mock.Options{}
	flag.StringVar(&opts.IMAPAddr, "imap-addr", "127.0.0.1:1143", "IMAP listen address")
	flag.StringVar(&opts.SMTPAddr, "smtp-addr", "127.0.0.1:1025", "SMTP listen address")
	flag.StringVar(&opts.User, "user", "user", "mailbox username")
	flag.StringVar(&opts.Pass, "pass", "pass", "mailbox password")
	flag.Parse()

	opts.Logger = log.New(os.Stdout, "[pmx-mocks] ", log.LstdFlags)

	srv, err := mock.Start(opts)
	if err != nil {
		log.Fatalf("failed to start mock servers: %v", err)
	}

	log.Printf("mock servers running. IMAP=%s SMTP=%s", srv.IMAPAddr(), srv.SMTPAddr())
	log.Printf("credentials: user=%s pass=%s", opts.User, opts.Pass)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Println("shutting down mock servers...")
	if err := srv.Close(); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
