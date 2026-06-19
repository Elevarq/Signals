package guidedconnect

import (
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// envFunc builds a Getenv from a fixture map.
func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestDetect_Table exercises the detection table from the spec
// (CONNECT-AC002): each documented environment proposes the documented
// auth_method.
func TestDetect_Table(t *testing.T) {
	cases := []struct {
		name string
		in   DetectInput
		want string
	}{
		{
			name: "aws identity + rds host -> aws_rds_iam",
			in: DetectInput{
				Host:   "orders.abc123.us-east-1.rds.amazonaws.com",
				Getenv: envFunc(map[string]string{"AWS_ACCESS_KEY_ID": "AKIA..."}),
			},
			want: config.AuthMethodAWSRDSIAM,
		},
		{
			name: "irsa identity + rds host -> aws_rds_iam",
			in: DetectInput{
				Host:   "orders.abc123.eu-west-1.rds.amazonaws.com",
				Getenv: envFunc(map[string]string{"AWS_ROLE_ARN": "arn:aws:iam::1:role/x", "AWS_WEB_IDENTITY_TOKEN_FILE": "/var/run/token"}),
			},
			want: config.AuthMethodAWSRDSIAM,
		},
		{
			name: "azure identity + azure host -> azure_entra",
			in: DetectInput{
				Host:   "mydb.postgres.database.azure.com",
				Getenv: envFunc(map[string]string{"AZURE_CLIENT_ID": "guid"}),
			},
			want: config.AuthMethodAzureEntra,
		},
		{
			name: "gcp identity + cloudsql host -> gcp_cloudsql_iam",
			in: DetectInput{
				Host:   "inst.cloudsql.goog",
				Getenv: envFunc(map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": "/adc.json"}),
			},
			want: config.AuthMethodGCPCloudSQLIAM,
		},
		{
			name: "gcp identity + cloudsql connection name -> gcp_cloudsql_iam",
			in: DetectInput{
				Host:   "proj:us-central1:inst",
				Getenv: envFunc(map[string]string{"K_SERVICE": "collector"}),
			},
			want: config.AuthMethodGCPCloudSQLIAM,
		},
		{
			name: "secret-ref supplied -> secret_store",
			in: DetectInput{
				Host:      "db.internal",
				SecretRef: "arn:aws:secretsmanager:us-east-1:1:secret:db",
			},
			want: config.AuthMethodSecretStore,
		},
		{
			name: "client cert supplied -> mtls",
			in: DetectInput{
				Host:    "db.internal",
				HasCert: true,
			},
			want: config.AuthMethodMTLS,
		},
		{
			name: "no identity, no cloud host -> password",
			in: DetectInput{
				Host:   "db.internal",
				Getenv: envFunc(nil),
			},
			want: config.AuthMethodPassword,
		},
		{
			name: "aws identity but non-cloud host -> password (identity without host)",
			in: DetectInput{
				Host:   "db.internal",
				Getenv: envFunc(map[string]string{"AWS_PROFILE": "prod"}),
			},
			want: config.AuthMethodPassword,
		},
		{
			name: "rds host but no identity -> password (host without identity)",
			in: DetectInput{
				Host:   "orders.abc123.us-east-1.rds.amazonaws.com",
				Getenv: envFunc(nil),
			},
			want: config.AuthMethodPassword,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.in)
			if got.Ambiguous {
				t.Fatalf("unexpected ambiguous detection: %+v", got)
			}
			if got.Method != tc.want {
				t.Fatalf("method = %q, want %q (notes: %v)", got.Method, tc.want, got.Notes)
			}
		})
	}
}

// TestDetect_HostDisambiguatesMultipleIdentities: when multiple cloud
// identities are present but the host pattern matches exactly one, the
// host disambiguates and that method is proposed — not ambiguous.
func TestDetect_HostDisambiguatesMultipleIdentities(t *testing.T) {
	d := Detect(DetectInput{
		Host: "orders.abc123.us-east-1.rds.amazonaws.com",
		Getenv: envFunc(map[string]string{
			"AWS_ACCESS_KEY_ID": "AKIA...",
			"AZURE_CLIENT_ID":   "guid",
		}),
	})
	if d.Ambiguous {
		t.Fatalf("host should disambiguate; got ambiguous: %+v", d)
	}
	if d.Method != config.AuthMethodAWSRDSIAM {
		t.Fatalf("method = %q, want %q", d.Method, config.AuthMethodAWSRDSIAM)
	}
}

// TestDetect_Ambiguous covers CONNECT-AC003 / FC001: more than one cloud
// identity and a host that matches none is reported ambiguous, not guessed.
func TestDetect_Ambiguous(t *testing.T) {
	d := Detect(DetectInput{
		Host: "db.internal",
		Getenv: envFunc(map[string]string{
			"AWS_ACCESS_KEY_ID": "AKIA...",
			"AZURE_CLIENT_ID":   "guid",
		}),
	})
	if !d.Ambiguous {
		t.Fatalf("want ambiguous detection, got: %+v", d)
	}
	if len(d.Notes) == 0 {
		t.Fatal("ambiguous detection must carry disambiguation notes")
	}
}
