// Command feedla is the single-binary feed reader server and CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/tokuhirom/feedla/internal/config"
	"github.com/tokuhirom/feedla/internal/feed"
	"github.com/tokuhirom/feedla/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "import-opml":
		err = cmdImportOPML(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "feedla:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: feedla <command> [args]

commands:
  import-opml <file.opml>   import feeds/folders from an OPML file`)
}

func cmdImportOPML(args []string) error {
	fs := flag.NewFlagSet("import-opml", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError already exits on parse failure
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: feedla import-opml <file.opml>")
	}
	path := fs.Arg(0)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store %s: %w", cfg.DBPath, err)
	}
	defer st.Close()

	n, err := feed.ImportOPML(context.Background(), st, f)
	if err != nil {
		return err
	}

	fmt.Printf("imported %d feed(s) into %s\n", n, cfg.DBPath)
	return nil
}
