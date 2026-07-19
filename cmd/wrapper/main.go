package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/force1267/big-brain/internal/app"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: wrapper [serve]\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// ponytail: single "serve" command for now; add a real command switch
	// when a second command exists.
	if cmd := flag.Arg(0); cmd != "" && cmd != "serve" {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.New().Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
