// Package remotebackup mirrors feedla's daily local backup snapshots
// (produced by internal/maintenance's VACUUM INTO job) into an
// S3-compatible object storage bucket, such as Sakura Cloud Object
// Storage, and prunes old generations so storage cost stays bounded.
package remotebackup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config points at an S3-compatible bucket to mirror backups into.
// Endpoint == "" means remote backup is disabled; callers should not
// construct a Client in that case.
type Config struct {
	Endpoint    string
	Region      string
	Bucket      string
	AccessKey   string
	SecretKey   string
	Prefix      string // key prefix within the bucket, e.g. "feedla/"
	Generations int    // most-recent snapshots to keep per file extension; <=0 disables pruning
}

// Client uploads backup snapshots to a bucket and prunes old generations.
type Client struct {
	s3          *s3.Client
	bucket      string
	prefix      string
	generations int
}

// New builds a Client from cfg. It does not contact the bucket.
func New(cfg Config) *Client {
	s3Client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		UsePathStyle: true,
		// Sakura's Object Storage (and other non-AWS S3-compatible
		// providers) doesn't reliably support the trailing checksum
		// headers the SDK sends by default; only send/require them when
		// an operation actually needs one.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &Client{s3: s3Client, bucket: cfg.Bucket, prefix: cfg.Prefix, generations: cfg.Generations}
}

// Store uploads the file at localPath to key (under Config.Prefix), then
// deletes objects sharing key's extension beyond the Config.Generations
// most recent. Keys are expected to embed a sortable date (e.g.
// feedla-20260818.db) so lexicographic order matches chronological order.
func (c *Client) Store(ctx context.Context, key, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("remotebackup: open %s: %w", localPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("remotebackup: stat %s: %w", localPath, err)
	}

	fullKey := c.prefix + key
	if _, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(fullKey),
		Body:          f,
		ContentLength: aws.Int64(info.Size()),
	}); err != nil {
		return fmt.Errorf("remotebackup: put %s: %w", fullKey, err)
	}

	if err := c.prune(ctx, path.Ext(key)); err != nil {
		return fmt.Errorf("remotebackup: prune %s: %w", path.Ext(key), err)
	}
	return nil
}

// Latest returns the most recent object key under Config.Prefix whose name
// ends in ext (e.g. ".db"), or found=false if none exist. As with prune,
// keys are expected to embed a sortable date (e.g. feedla-20260818.db), so
// the lexicographically largest key is also the most recent. The returned
// key already includes Config.Prefix and can be passed directly to Download.
func (c *Client) Latest(ctx context.Context, ext string) (key string, found bool, err error) {
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(c.prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", false, fmt.Errorf("remotebackup: list objects: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key != nil && strings.HasSuffix(*obj.Key, ext) {
				keys = append(keys, *obj.Key)
			}
		}
	}
	if len(keys) == 0 {
		return "", false, nil
	}
	sort.Strings(keys)
	return keys[len(keys)-1], true, nil
}

// Download fetches the object at key (a full key as returned by Latest,
// already including Config.Prefix) to destPath. It writes to a sibling temp
// file and renames into place on success, so a failed/canceled download
// never leaves a truncated file at destPath.
func (c *Client) Download(ctx context.Context, key, destPath string) error {
	obj, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("remotebackup: get %s: %w", key, err)
	}
	defer func() { _ = obj.Body.Close() }()

	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("remotebackup: create %s: %w", tmpPath, err)
	}
	if _, err := io.Copy(f, obj.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("remotebackup: download %s: %w", key, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("remotebackup: close %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("remotebackup: rename into place: %w", err)
	}
	return nil
}

// prune deletes objects under Config.Prefix whose key ends in ext beyond
// the Config.Generations most recent.
func (c *Client) prune(ctx context.Context, ext string) error {
	if c.generations <= 0 {
		return nil
	}

	var keys []string
	paginator := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(c.prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list objects: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key != nil && strings.HasSuffix(*obj.Key, ext) {
				keys = append(keys, *obj.Key)
			}
		}
	}

	for _, key := range pruneTargets(keys, c.generations) {
		if _, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(key),
		}); err != nil {
			return fmt.Errorf("delete %s: %w", key, err)
		}
	}
	return nil
}

// pruneTargets returns the keys to delete, keeping only the `keep`
// lexicographically-largest (== most recent, given the feedla-YYYYMMDD.ext
// naming scheme) keys.
func pruneTargets(keys []string, keep int) []string {
	if keep <= 0 || len(keys) <= keep {
		return nil
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	return sorted[:len(sorted)-keep]
}
