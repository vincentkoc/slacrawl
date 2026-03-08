package main

import (
	"context"
	"fmt"
	"os"

	"github.com/vincentkoc/slacrawl/internal/cli"
)

func main() {
	ctx := context.Background()
	app := cli.New()
	if err := app.Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
