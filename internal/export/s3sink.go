// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

// Native S3 destination for scheduled export (#472). When export_dest is an
// s3://bucket/prefix URI, the scheduled exporter uploads each per-database ZIP
// straight to S3 instead of writing a local file — closing the Cloud
// delivery gap (Signals -> S3 -> analyzer inbox) without a workaround.
//
// Security (the whole point of native S3): the sink uses ONLY s3:PutObject —
// it never lists, gets, or deletes — so the delivery identity needs a single
// least-privilege action. Retention is therefore an S3 lifecycle rule, not a
// prune. Credentials come from the default AWS chain (IRSA / instance role);
// no static keys ever appear in config, env, logs, or state. Every object is
// written with server-side encryption (SSE-S3 by default, SSE-KMS when a key
// is configured).
package export

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// s3PutAPI is the narrow slice of the S3 client the sink depends on: a single
// PutObject. Keeping it to one method is both the least-privilege contract in
// code and what makes the sink unit-testable with a tiny fake (no AWS, no other
// verbs to stub). It MUST NOT grow list/get/delete methods.
type s3PutAPI interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// s3Sink uploads scheduled-export ZIPs to an S3 prefix with PutObject only.
type s3Sink struct {
	client   s3PutAPI
	bucket   string
	prefix   string // normalised: no leading or trailing slash, "" = bucket root
	kmsKeyID string // empty = SSE-S3 (AES256); set = SSE-KMS with this key
}

// newS3Sink builds a sink for s3://bucket/prefix. The client is injected so the
// caller (cmd/signals) owns AWS config + region resolution and tests can fake
// it.
func newS3Sink(client s3PutAPI, bucket, prefix, kmsKeyID string) *s3Sink {
	return &s3Sink{
		client:   client,
		bucket:   bucket,
		prefix:   strings.Trim(prefix, "/"),
		kmsKeyID: kmsKeyID,
	}
}

// key joins the prefix and the flat export filename into the object key.
func (s *s3Sink) key(name string) string {
	if s.prefix == "" {
		return name
	}
	return s.prefix + "/" + name
}

// put uploads one export ZIP. A completed PutObject is atomic — a consumer
// (the analyzer S3 source) never observes a partial object — so no temp/rename
// dance is needed. Returns the s3:// location for logging.
func (s *s3Sink) put(ctx context.Context, name string, body []byte) (string, error) {
	key := s.key(name)
	in := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/zip"),
	}
	if s.kmsKeyID != "" {
		in.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		in.SSEKMSKeyId = aws.String(s.kmsKeyID)
	} else {
		in.ServerSideEncryption = types.ServerSideEncryptionAes256
	}
	if _, err := s.client.PutObject(ctx, in); err != nil {
		return "", fmt.Errorf("s3 put %s: %w", key, err)
	}
	return fmt.Sprintf("s3://%s/%s", s.bucket, key), nil
}
