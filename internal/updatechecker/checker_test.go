package updatechecker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "camunda-stub-worker/1.2.3" {
			t.Errorf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.4.0","html_url":"https://github.com/mishankov/camunda-stub-worker/releases/tag/v1.4.0"}`))
	}))
	defer server.Close()

	info, err := (Client{HTTPClient: server.Client(), Endpoint: server.URL}).Check(context.Background(), "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if !info.UpdateAvailable || info.CurrentVersion != "1.2.3" || info.LatestVersion != "1.4.0" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestCheckDoesNotDowngradeOrReportSameVersion(t *testing.T) {
	for _, tag := range []string{"v1.2.3", "v1.2.2", "v1.2.3-beta.1"} {
		t.Run(tag, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"tag_name":"` + tag + `","html_url":"https://example.test/release"}`))
			}))
			defer server.Close()
			info, err := (Client{HTTPClient: server.Client(), Endpoint: server.URL}).Check(context.Background(), "1.2.3")
			if err != nil {
				t.Fatal(err)
			}
			if info.UpdateAvailable {
				t.Fatalf("unexpected update for %s", tag)
			}
		})
	}
}

func TestCheckRejectsBadResponses(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
	}{
		{name: "server error", status: http.StatusServiceUnavailable},
		{name: "invalid json", status: http.StatusOK, body: `{`},
		{name: "missing fields", status: http.StatusOK, body: `{}`},
		{name: "invalid version", status: http.StatusOK, body: `{"tag_name":"latest","html_url":"https://example.test"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			_, err := (Client{HTTPClient: server.Client(), Endpoint: server.URL}).Check(context.Background(), "1.0.0")
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestSemanticVersionPrecedence(t *testing.T) {
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0"}
	for i := 1; i < len(ordered); i++ {
		previous, err := parseVersion(ordered[i-1])
		if err != nil {
			t.Fatal(err)
		}
		current, err := parseVersion(ordered[i])
		if err != nil {
			t.Fatal(err)
		}
		if compareVersions(previous, current) >= 0 {
			t.Fatalf("expected %s < %s", ordered[i-1], ordered[i])
		}
	}
}
