// Package s3 lists objects in users' S3 / S3-compatible buckets (PLAN.md §5.2).
package s3

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"msapi/internal/model"
)

// MaxObjects caps a resource listing.
const MaxObjects = 1000

func client(c model.S3Conn) (*minio.Client, error) {
	lookup := minio.BucketLookupPath
	if c.VirtualHosting {
		lookup = minio.BucketLookupDNS
	}
	return minio.New(c.EndpointOrAWS(), &minio.Options{
		Creds:        credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure:       c.UseHTTPS,
		Region:       c.Region,
		BucketLookup: lookup,
	})
}

// Test checks the bucket exists and the credentials can list it.
func Test(ctx context.Context, c model.S3Conn) error {
	cl, err := client(c)
	if err != nil {
		return err
	}
	ok, err := cl.BucketExists(ctx, c.Bucket)
	if err != nil {
		return fmt.Errorf("bucket check: %w", err)
	}
	if !ok {
		return fmt.Errorf("bucket %q does not exist", c.Bucket)
	}
	for obj := range cl.ListObjects(ctx, c.Bucket, minio.ListObjectsOptions{Prefix: c.Prefix, MaxKeys: 1}) {
		if obj.Err != nil {
			return fmt.Errorf("list objects: %w", obj.Err)
		}
		break
	}
	return nil
}

// Object is a publishable raster file.
type Object struct {
	Path         string    `json:"path"` // key inside the bucket
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

// ListTIFFs lists .tif/.tiff objects under the store prefix. truncated is
// true when more than MaxObjects exist.
func ListTIFFs(ctx context.Context, c model.S3Conn) (objs []Object, truncated bool, err error) {
	cl, err := client(c)
	if err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for obj := range cl.ListObjects(ctx, c.Bucket, minio.ListObjectsOptions{Prefix: c.Prefix, Recursive: true}) {
		if obj.Err != nil {
			return nil, false, obj.Err
		}
		lower := strings.ToLower(obj.Key)
		if !strings.HasSuffix(lower, ".tif") && !strings.HasSuffix(lower, ".tiff") {
			continue
		}
		if len(objs) == MaxObjects {
			return objs, true, nil
		}
		objs = append(objs, Object{Path: obj.Key, Size: obj.Size, LastModified: obj.LastModified})
	}
	return objs, false, nil
}
