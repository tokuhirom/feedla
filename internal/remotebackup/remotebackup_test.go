package remotebackup

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// newTestClient spins up an in-process fake S3 server (gofakes3, backed by
// an in-memory bucket) and returns a Client pointed at it, so tests
// exercise the real S3 request/response wire format instead of a hand-rolled
// interface fake.
func newTestClient(t *testing.T, generations int) *Client {
	t.Helper()

	backend := s3mem.New()
	if err := backend.CreateBucket("test-bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	faker := gofakes3.New(backend)
	srv := httptest.NewServer(faker.Server())
	t.Cleanup(srv.Close)

	return New(Config{
		Endpoint:    srv.URL,
		Region:      "jp-north-1",
		Bucket:      "test-bucket",
		AccessKey:   "test-access-key",
		SecretKey:   "test-secret-key",
		Prefix:      "feedla/",
		Generations: generations,
	})
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func listKeys(t *testing.T, c *Client) []string {
	t.Helper()
	out, err := c.s3.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
	})
	if err != nil {
		t.Fatalf("list objects: %v", err)
	}
	var keys []string
	for _, obj := range out.Contents {
		keys = append(keys, *obj.Key)
	}
	sort.Strings(keys)
	return keys
}

func TestClient_Store_UploadsObject(t *testing.T) {
	c := newTestClient(t, 5)
	src := writeTempFile(t, "feedla-20260101.db", "snapshot-1")

	if err := c.Store(context.Background(), "feedla-20260101.db", src); err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, err := c.s3.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String("test-bucket"),
		Key:    aws.String("feedla/feedla-20260101.db"),
	})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	defer func() { _ = got.Body.Close() }()
	buf := make([]byte, 64)
	n, _ := got.Body.Read(buf)
	if string(buf[:n]) != "snapshot-1" {
		t.Errorf("object body = %q, want %q", buf[:n], "snapshot-1")
	}
}

func TestClient_Store_PrunesOldGenerations(t *testing.T) {
	c := newTestClient(t, 2)
	ctx := context.Background()

	days := []string{"20260101", "20260102", "20260103", "20260104"}
	for _, d := range days {
		src := writeTempFile(t, "feedla-"+d+".db", "snapshot-"+d)
		if err := c.Store(ctx, "feedla-"+d+".db", src); err != nil {
			t.Fatalf("Store(%s): %v", d, err)
		}
	}

	got := listKeys(t, c)
	want := []string{"feedla/feedla-20260103.db", "feedla/feedla-20260104.db"}
	if len(got) != len(want) {
		t.Fatalf("keys after pruning = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("keys after pruning = %v, want %v", got, want)
			break
		}
	}
}

func TestClient_Store_PrunesExtensionsIndependently(t *testing.T) {
	c := newTestClient(t, 1)
	ctx := context.Background()

	days := []string{"20260101", "20260102"}
	for _, d := range days {
		dbPath := writeTempFile(t, "feedla-"+d+".db", "db-"+d)
		if err := c.Store(ctx, "feedla-"+d+".db", dbPath); err != nil {
			t.Fatalf("Store(db, %s): %v", d, err)
		}
		opmlPath := writeTempFile(t, "feedla-"+d+".opml", "opml-"+d)
		if err := c.Store(ctx, "feedla-"+d+".opml", opmlPath); err != nil {
			t.Fatalf("Store(opml, %s): %v", d, err)
		}
	}

	got := listKeys(t, c)
	want := []string{"feedla/feedla-20260102.db", "feedla/feedla-20260102.opml"}
	if len(got) != len(want) {
		t.Fatalf("keys after pruning = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("keys after pruning = %v, want %v", got, want)
			break
		}
	}
}

func TestClient_Store_GenerationsZeroDisablesPruning(t *testing.T) {
	c := newTestClient(t, 0)
	ctx := context.Background()

	for _, d := range []string{"20260101", "20260102", "20260103"} {
		src := writeTempFile(t, "feedla-"+d+".db", "snapshot-"+d)
		if err := c.Store(ctx, "feedla-"+d+".db", src); err != nil {
			t.Fatalf("Store(%s): %v", d, err)
		}
	}

	got := listKeys(t, c)
	if len(got) != 3 {
		t.Fatalf("keys = %v, want 3 objects (pruning disabled)", got)
	}
}

func TestPruneTargets(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		keep int
		want []string
	}{
		{"empty", nil, 5, nil},
		{"under keep", []string{"a", "b"}, 5, nil},
		{"exact keep", []string{"a", "b"}, 2, nil},
		{"keep disabled", []string{"a", "b", "c"}, 0, nil},
		{
			"prunes oldest",
			[]string{"feedla-20260103.db", "feedla-20260101.db", "feedla-20260102.db"},
			2,
			[]string{"feedla-20260101.db"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneTargets(tt.keys, tt.keep)
			if len(got) != len(tt.want) {
				t.Fatalf("pruneTargets(%v, %d) = %v, want %v", tt.keys, tt.keep, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("pruneTargets(%v, %d) = %v, want %v", tt.keys, tt.keep, got, tt.want)
				}
			}
		})
	}
}
