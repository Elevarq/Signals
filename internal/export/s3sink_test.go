// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

package export

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeS3Builder is a minimal snapshotSource: it yields a fixed set of target
// ids and writes a trivial body per target. It lets the S3 export path run
// with no database.
type fakeS3Builder struct {
	ids      []int64
	writeErr error
}

func (f fakeS3Builder) LatestTargetIDs() ([]int64, error) { return f.ids, nil }
func (f fakeS3Builder) WriteTo(w io.Writer, _ Options) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	_, err := w.Write([]byte("snapshot-zip-bytes"))
	return err
}

// fakePutAPI records PutObject calls. It implements ONLY PutObject — the same
// one-method contract as s3PutAPI — so the test proves by construction that the
// sink never lists, gets, or deletes (least-privilege, #472).
type fakePutAPI struct {
	puts []*s3.PutObjectInput
	err  error
}

func (f *fakePutAPI) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.puts = append(f.puts, in)
	return &s3.PutObjectOutput{}, nil
}

func fixedNow() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }

// SE-AC(S3 normal): with an s3:// destination and two targets, a cycle uploads
// one object per database to the prefix with per-DB unique keys and SSE-S3.
func TestScheduledExportS3_PerDatabasePutObjects(t *testing.T) {
	put := &fakePutAPI{}
	e := NewScheduledExporterS3(fakeS3Builder{ids: []int64{1, 2}}, put, "mybucket", "snapshots", "", "inst", fixedNow, nil)

	written, err := e.ExportLatest(context.Background())
	if err != nil {
		t.Fatalf("ExportLatest: %v", err)
	}
	if len(written) != 2 || len(put.puts) != 2 {
		t.Fatalf("want 2 uploads, got written=%d puts=%d", len(written), len(put.puts))
	}
	sawT1, sawT2 := false, false
	for _, in := range put.puts {
		if in.Bucket == nil || *in.Bucket != "mybucket" {
			t.Errorf("bad bucket: %v", in.Bucket)
		}
		if in.Key == nil || !strings.HasPrefix(*in.Key, "snapshots/") || !strings.HasSuffix(*in.Key, ".zip") {
			t.Errorf("bad key: %v", in.Key)
		}
		if in.ServerSideEncryption != types.ServerSideEncryptionAes256 {
			t.Errorf("want SSE AES256, got %q", in.ServerSideEncryption)
		}
		if in.SSEKMSKeyId != nil {
			t.Errorf("no KMS key expected, got %v", in.SSEKMSKeyId)
		}
		if strings.Contains(*in.Key, "-t1-") {
			sawT1 = true
		}
		if strings.Contains(*in.Key, "-t2-") {
			sawT2 = true
		}
	}
	if !sawT1 || !sawT2 {
		t.Errorf("want a per-database key for t1 and t2; keys=%v", keysOf(put))
	}
}

// SE-AC(S3 KMS): a configured KMS key selects SSE-KMS with that key id.
func TestScheduledExportS3_KMSKey(t *testing.T) {
	put := &fakePutAPI{}
	e := NewScheduledExporterS3(fakeS3Builder{ids: []int64{1}}, put, "b", "p", "arn:aws:kms:key", "inst", fixedNow, nil)
	if _, err := e.ExportLatest(context.Background()); err != nil {
		t.Fatalf("ExportLatest: %v", err)
	}
	if len(put.puts) != 1 {
		t.Fatalf("want 1 put, got %d", len(put.puts))
	}
	in := put.puts[0]
	if in.ServerSideEncryption != types.ServerSideEncryptionAwsKms {
		t.Errorf("want SSE aws:kms, got %q", in.ServerSideEncryption)
	}
	if in.SSEKMSKeyId == nil || *in.SSEKMSKeyId != "arn:aws:kms:key" {
		t.Errorf("want SSEKMSKeyId set, got %v", in.SSEKMSKeyId)
	}
}

// SE-AC(S3 no-prefix): an empty prefix puts keys at the bucket root.
func TestScheduledExportS3_NoPrefix(t *testing.T) {
	put := &fakePutAPI{}
	e := NewScheduledExporterS3(fakeS3Builder{ids: []int64{1}}, put, "b", "", "", "inst", fixedNow, nil)
	if _, err := e.ExportLatest(context.Background()); err != nil {
		t.Fatalf("ExportLatest: %v", err)
	}
	if k := *put.puts[0].Key; strings.Contains(k, "/") {
		t.Errorf("no-prefix key should be flat, got %q", k)
	}
}

// SE-AC(S3 failure): a PutObject error is returned to the caller (the collector
// hook logs it and continues) — it must not panic.
func TestScheduledExportS3_PutErrorFailsOpen(t *testing.T) {
	put := &fakePutAPI{err: errors.New("access denied")}
	e := NewScheduledExporterS3(fakeS3Builder{ids: []int64{1}}, put, "b", "p", "", "inst", fixedNow, nil)
	if _, err := e.ExportLatest(context.Background()); err == nil {
		t.Fatal("want an error from a failing PutObject")
	}
}

func keysOf(f *fakePutAPI) []string {
	out := make([]string, 0, len(f.puts))
	for _, in := range f.puts {
		if in.Key != nil {
			out = append(out, *in.Key)
		}
	}
	return out
}
