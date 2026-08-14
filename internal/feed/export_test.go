package feed_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tokuhirom/feedla/internal/feed"
	"github.com/tokuhirom/feedla/internal/store"
)

func TestExportOPMLRoundTrips(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	if _, err := feed.ImportOPML(ctx, st, strings.NewReader(sampleOPML)); err != nil {
		t.Fatalf("ImportOPML: %v", err)
	}

	out, err := feed.ExportOPML(ctx, st)
	if err != nil {
		t.Fatalf("ExportOPML: %v", err)
	}
	if !bytes.HasPrefix(out, []byte(`<?xml version="1.0" encoding="UTF-8"?>`)) {
		t.Fatalf("export missing xml declaration: %s", out)
	}

	dbPath2 := filepath.Join(t.TempDir(), "feedla2.db")
	st2, err := store.Open(dbPath2)
	if err != nil {
		t.Fatalf("store.Open (2): %v", err)
	}
	t.Cleanup(func() { st2.Close() })

	n, err := feed.ImportOPML(ctx, st2, bytes.NewReader(out))
	if err != nil {
		t.Fatalf("ImportOPML(exported): %v", err)
	}
	if n != 3 {
		t.Fatalf("re-imported = %d, want 3", n)
	}

	folders, err := st2.ListFolders(ctx)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(folders) != 1 || folders[0].Name != "Tech" {
		t.Fatalf("folders = %+v, want single Tech folder", folders)
	}

	feeds, err := st2.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != 3 {
		t.Fatalf("len(feeds) = %d, want 3", len(feeds))
	}
}
