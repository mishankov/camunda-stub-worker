package operate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListProcessesPaginatesAndGroupsVersions(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/operate/v1/process-definitions/search" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if calls == 1 {
			fmt.Fprint(w, `{"items":[{"bpmnProcessId":"order","name":"Old name","version":1},{"bpmnProcessId":"other","version":1,"tenantId":"another-tenant"}],"total":4,"sortValues":[9007199254740993]}`)
		} else {
			if !strings.Contains(string(raw), `"searchAfter":[9007199254740993]`) {
				t.Errorf("cursor lost precision: %s", raw)
			}
			fmt.Fprint(w, `{"items":[{"bpmnProcessId":"order","name":"Order","version":2,"tenantId":"<default>"},{"bpmnProcessId":"alpha","name":"Alpha","version":1}],"total":4,"sortValues":[9007199254740995]}`)
		}
	}))
	defer server.Close()
	for _, suffix := range []string{"/operate", "/operate/v1/"} {
		calls = 0
		result, err := ListProcesses(context.Background(), server.URL+suffix, Auth{Mode: "token", Token: "secret"})
		if err != nil {
			t.Fatal(err)
		}
		if calls != 2 || len(result) != 2 || result[0].BPMNProcessID != "alpha" || result[1].Name != "Order" || result[1].LatestVersion != 2 {
			t.Fatalf("unexpected result: %+v calls=%d", result, calls)
		}
	}
}

func TestListProcessesErrors(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         int
		body, contains string
	}{
		{"auth", 401, "secret server error", "denied access"},
		{"wrong endpoint", 404, "", "HTTP 404"},
		{"login", 200, "<html>Login</html>", "unexpected Operate response"},
		{"wrong schema", 200, `{}`, "unexpected Operate response"},
		{"missing cursor", 200, `{"items":[{"bpmnProcessId":"order","version":1}],"total":2}`, "pagination"},
		{"repeated cursor", 200, `{"items":[{"bpmnProcessId":"order","version":1}],"total":10,"sortValues":[1]}`, "pagination"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			_, err := ListProcesses(context.Background(), server.URL, Auth{})
			if err == nil || !strings.Contains(err.Error(), tc.contains) || strings.Contains(err.Error(), "secret server error") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
func TestListProcessesEmptyAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			select {
			case <-r.Context().Done():
			case <-time.After(100 * time.Millisecond):
			}
			return
		}
		fmt.Fprint(w, `{"items":[],"total":0}`)
	}))
	defer server.Close()
	result, err := ListProcesses(context.Background(), server.URL, Auth{})
	if err != nil || result == nil || len(result) != 0 {
		t.Fatalf("result=%v err=%v", result, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = ListProcesses(ctx, server.URL, Auth{Mode: "token", Token: "token"}); err == nil {
		t.Fatal("expected timeout")
	}
}
func TestListProcessesDoesNotFollowRedirects(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed redirect") }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	if _, err := ListProcesses(context.Background(), source.URL, Auth{Mode: "token", Token: "token"}); err == nil {
		t.Fatal("accepted redirect")
	}
}

func TestPasswordLoginAndPaginatedSession(t *testing.T) {
	for _, suffix := range []string{"", "/operate/v1/"} {
		t.Run(suffix, func(t *testing.T) {
			prefix := strings.TrimSuffix(strings.TrimRight(suffix, "/"), "/v1")
			logins, pages := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" {
					t.Error("credentials sent in URL or authorization header")
				}
				if r.URL.Path == prefix+"/api/login" {
					logins++
					if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
						t.Error("invalid login request")
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.PostForm.Get("username") != "demo+user" || r.PostForm.Get("password") != "p&= +?#" {
						t.Error("credentials not encoded correctly")
					}
					http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "session", Path: prefix + "/", HttpOnly: true})
					w.WriteHeader(http.StatusOK)
					return
				}
				if r.URL.Path != prefix+"/v1/process-definitions/search" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				cookie, err := r.Cookie("JSESSIONID")
				if err != nil || cookie.Value != "session" {
					t.Error("missing session cookie")
				}
				pages++
				fmt.Fprintf(w, `{"items":[{"bpmnProcessId":"order","version":%d}],"total":2,"sortValues":[%d]}`, pages, pages)
			}))
			defer server.Close()
			result, err := ListProcesses(context.Background(), server.URL+suffix, Auth{Mode: "password", Username: "demo+user", Password: "p&= +?#", Token: "unused"})
			if err != nil || logins != 1 || pages != 2 || len(result) != 1 || result[0].LatestVersion != 2 {
				t.Fatalf("result=%v logins=%d pages=%d err=%v", result, logins, pages, err)
			}
		})
	}
}

func TestPasswordLoginFailures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		code     int
		cookie   bool
		location string
	}{
		{"wrong password", 401, false, ""},
		{"forbidden", 403, false, ""},
		{"missing endpoint", 404, false, ""},
		{"no session", 200, false, ""},
		{"login error redirect", 302, true, "/login?error"},
		{"sso redirect", 302, true, "https://identity.example.test/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/login" {
					t.Error("queried processes after failed login")
				}
				if tc.cookie {
					http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "session", Path: "/"})
				}
				if tc.location != "" {
					w.Header().Set("Location", tc.location)
				}
				w.WriteHeader(tc.code)
				fmt.Fprint(w, "sensitive password echoed by server")
			}))
			defer server.Close()
			_, err := ListProcesses(context.Background(), server.URL, Auth{Mode: "password", Username: "user", Password: "sensitive"})
			if err == nil || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
