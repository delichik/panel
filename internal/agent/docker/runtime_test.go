package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDockerAPIClientPullImagePassesExplicitLatestTag(t *testing.T) {
	latest := "latest"
	v1 := "v1"
	cases := []struct {
		name      string
		image     string
		fromImage string
		tag       *string
	}{
		{name: "single name defaults latest", image: "nginx", fromImage: "nginx", tag: &latest},
		{name: "repository defaults latest", image: "team/web", fromImage: "team/web", tag: &latest},
		{name: "explicit tag", image: "team/web:v1", fromImage: "team/web", tag: &v1},
		{name: "registry port defaults latest", image: "registry.example.com:5000/team/web", fromImage: "registry.example.com:5000/team/web", tag: &latest},
		{name: "registry port explicit tag", image: "registry.example.com:5000/team/web:v1", fromImage: "registry.example.com:5000/team/web", tag: &v1},
		{name: "digest reference preserved", image: "team/web@sha256:abc123", fromImage: "team/web@sha256:abc123"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries := make(chan url.Values, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if r.URL.Path != "/images/create" {
					t.Errorf("path = %s, want /images/create", r.URL.Path)
				}
				queries <- r.URL.Query()
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client := &dockerAPIClient{host: server.URL, pullClient: server.Client()}
			if err := client.pullImage(context.Background(), tc.image); err != nil {
				t.Fatalf("pullImage() error = %v", err)
			}

			query := <-queries
			if got := query.Get("fromImage"); got != tc.fromImage {
				t.Fatalf("fromImage = %q, want %q", got, tc.fromImage)
			}
			gotTags, hasTag := query["tag"]
			if tc.tag == nil {
				if hasTag {
					t.Fatalf("tag query = %#v, want absent", gotTags)
				}
				return
			}
			if !hasTag || len(gotTags) != 1 || gotTags[0] != *tc.tag {
				t.Fatalf("tag query = %#v, want %q", gotTags, *tc.tag)
			}
		})
	}
}
