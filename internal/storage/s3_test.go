package storage

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// Presigning is pure client-side signing once the region is pinned (no
// bucket-location lookup), so these tests need no running object store.

func TestGetURLSignsAgainstPublicEndpoint(t *testing.T) {
	s, err := NewWithPublicEndpoint("internal:9000", "public.example.com:9000", "id", "secret", false, "bkt", "us-east-1")
	if err != nil {
		t.Fatalf("NewWithPublicEndpoint: %v", err)
	}

	raw, err := s.GetURL(context.Background(), "docs/report.pdf")
	if err != nil {
		t.Fatalf("GetURL: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	if u.Host != "public.example.com:9000" {
		t.Errorf("host = %q, want %q", u.Host, "public.example.com:9000")
	}
	if !strings.Contains(u.Path, "bkt") || !strings.Contains(u.Path, "docs/report.pdf") {
		t.Errorf("path = %q, want it to contain bucket and key", u.Path)
	}
	q := u.Query()
	if q.Get("X-Amz-Signature") == "" {
		t.Error("missing X-Amz-Signature query parameter")
	}
	if got := q.Get("response-content-disposition"); got != "attachment" {
		t.Errorf("response-content-disposition = %q, want %q", got, "attachment")
	}
}

func TestNewWithPublicEndpointRejectsInvalidEndpoint(t *testing.T) {
	if _, err := NewWithPublicEndpoint("internal:9000", "http://has-a-scheme:9000", "id", "secret", false, "bkt", "us-east-1"); err == nil {
		t.Error("expected error for public endpoint with scheme, got nil")
	}
}
