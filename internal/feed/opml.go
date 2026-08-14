// Package feed handles feed discovery, normalization and OPML import/export.
package feed

import (
	"encoding/xml"
	"fmt"
	"io"
)

// Outline is a single <outline> node of an OPML document. Nodes without an
// xmlUrl attribute are treated as folders; their children inherit that
// folder.
type Outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr"`
	Type     string    `xml:"type,attr"`
	XMLURL   string    `xml:"xmlUrl,attr"`
	HTMLURL  string    `xml:"htmlUrl,attr"`
	Outlines []Outline `xml:"outline"`
}

type opmlDocument struct {
	XMLName xml.Name `xml:"opml"`
	Body    struct {
		Outlines []Outline `xml:"outline"`
	} `xml:"body"`
}

// ImportedFeed is one <outline> that resolved to a feed subscription.
type ImportedFeed struct {
	Title      string
	FeedURL    string
	SiteURL    string
	FolderName string // "" if the feed isn't nested under a folder outline
}

// ParseOPML reads an OPML document and flattens it into a list of feeds,
// each tagged with the (innermost) folder it was nested under, if any.
func ParseOPML(r io.Reader) ([]ImportedFeed, error) {
	var doc opmlDocument
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("feed: parse opml: %w", err)
	}

	var feeds []ImportedFeed
	collectOutlines(doc.Body.Outlines, "", &feeds)
	return feeds, nil
}

func collectOutlines(outlines []Outline, folder string, feeds *[]ImportedFeed) {
	for _, o := range outlines {
		name := o.Title
		if name == "" {
			name = o.Text
		}

		if o.XMLURL == "" {
			// Folder outline: recurse using this outline's name as the
			// folder for its children.
			collectOutlines(o.Outlines, name, feeds)
			continue
		}

		*feeds = append(*feeds, ImportedFeed{
			Title:      name,
			FeedURL:    o.XMLURL,
			SiteURL:    o.HTMLURL,
			FolderName: folder,
		})
	}
}
