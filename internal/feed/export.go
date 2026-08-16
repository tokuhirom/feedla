package feed

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/tokuhirom/feedla/internal/store"
)

const xmlDeclaration = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// ExportOPML renders every subscription userID has as an OPML document,
// nesting feeds under their folder's outline the same way ImportOPML
// expects to read them back.
func ExportOPML(ctx context.Context, st *store.Store, userID int64) ([]byte, error) {
	subs, err := st.ListSubscriptionViews(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("feed: export opml: list subscriptions: %w", err)
	}
	folders, err := st.ListFolders(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("feed: export opml: list folders: %w", err)
	}

	byFolder := make(map[int64][]Outline)
	var unfoldered []Outline
	for _, s := range subs {
		if s.Kind == "pagewatch" {
			// pagewatch subscriptions have no real feed URL (feed_url is a
			// "pagewatch:" pseudo-scheme resolved only inside feedla) and
			// aren't meaningful to another OPML-reading tool, so they're
			// excluded from export rather than round-tripped as broken
			// entries (§12 #7).
			continue
		}
		o := Outline{
			Text:    s.Title,
			Title:   s.Title,
			Type:    "rss",
			XMLURL:  s.FeedURL,
			HTMLURL: s.SiteURL,
		}
		if s.FolderID != nil {
			byFolder[*s.FolderID] = append(byFolder[*s.FolderID], o)
		} else {
			unfoldered = append(unfoldered, o)
		}
	}

	var doc opmlDocument
	doc.Version = "2.0"
	doc.Head.Title = "feedla subscriptions"
	for _, f := range folders {
		feeds := byFolder[f.ID]
		if len(feeds) == 0 {
			continue
		}
		doc.Body.Outlines = append(doc.Body.Outlines, Outline{
			Text:     f.Name,
			Title:    f.Name,
			Outlines: feeds,
		})
	}
	doc.Body.Outlines = append(doc.Body.Outlines, unfoldered...)

	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("feed: export opml: marshal: %w", err)
	}
	return append([]byte(xmlDeclaration), out...), nil
}
