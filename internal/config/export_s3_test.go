// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

package config

import "testing"

// #472 — export_dest overloaded to accept s3://bucket/prefix.
func TestParseExportS3(t *testing.T) {
	cases := []struct {
		in      string
		bucket  string
		prefix  string
		wantErr bool
	}{
		{"s3://b/p", "b", "p", false},
		{"s3://b/p/q", "b", "p/q", false},
		{"s3://b", "b", "", false},
		{"s3://b/", "b", "", false},
		{"s3://", "", "", true},   // empty bucket
		{"s3:///x", "", "", true}, // empty bucket
		{"/local/dir", "", "", true},
	}
	for _, c := range cases {
		b, p, err := ParseExportS3(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: want error, got (%q,%q)", c.in, b, p)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if b != c.bucket || p != c.prefix {
			t.Errorf("%q: got (%q,%q) want (%q,%q)", c.in, b, p, c.bucket, c.prefix)
		}
	}
}

func TestExportDestIsS3(t *testing.T) {
	if !(SignalsConfig{ExportDest: "s3://b/p"}).ExportDestIsS3() {
		t.Error("s3:// dest not detected as S3")
	}
	if (SignalsConfig{ExportDest: "/local/dir"}).ExportDestIsS3() {
		t.Error("local dir misdetected as S3")
	}
}
