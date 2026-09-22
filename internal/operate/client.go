package operate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
)

type Process struct {
	BPMNProcessID string `json:"bpmnProcessId"`
	Name          string `json:"name"`
	LatestVersion int32  `json:"latestVersion"`
}

type Auth struct {
	Mode     string `json:"mode"`
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ListProcesses queries Operate's v1 API. Keys and pagination tokens are never
// decoded as floating-point numbers. Only the default tenant can be started by this app.
func ListProcesses(ctx context.Context, baseURL string, auth Auth) ([]Process, error) {
	if baseURL == "" {
		return nil, errors.New("set an Operate URL in Connections to load deployed processes")
	}
	if err := domain.ValidateOperateURL(baseURL); err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	endpoint := base
	if !strings.HasSuffix(endpoint, "/v1") {
		endpoint += "/v1"
	}
	endpoint += "/process-definitions/search"
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	switch auth.Mode {
	case "", "none":
	case "token":
		if strings.TrimSpace(auth.Token) == "" {
			return nil, errors.New("enter an Operate bearer token")
		}
	case "password":
		if auth.Username == "" || auth.Password == "" {
			return nil, errors.New("enter an Operate username and password")
		}
		if err := login(ctx, client, base, auth); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unknown Operate authentication method")
	}
	processes := map[string]Process{}
	var cursor json.RawMessage
	seen := map[string]bool{}
	count := 0
	for {
		query := struct {
			Size        int                 `json:"size"`
			Sort        []map[string]string `json:"sort"`
			SearchAfter json.RawMessage     `json:"searchAfter,omitempty"`
		}{100, []map[string]string{{"field": "key", "order": "ASC"}}, cursor}
		body, err := json.Marshal(query)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if auth.Mode == "token" {
			req.Header.Set("Authorization", "Bearer "+auth.Token)
		}
		response, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("could not reach Operate: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		response.Body.Close()
		if response.StatusCode == 401 || response.StatusCode == 403 {
			return nil, errors.New("Operate denied access; check your authentication method, credentials, and permission to read process definitions")
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Operate returned HTTP %d; check the Operate base URL and authentication", response.StatusCode)
		}
		if readErr != nil {
			return nil, readErr
		}
		var page struct {
			Items []struct {
				BPMNProcessID string `json:"bpmnProcessId"`
				Name          string `json:"name"`
				Version       int32  `json:"version"`
				TenantID      string `json:"tenantId"`
			} `json:"items"`
			Total      *int            `json:"total"`
			SortValues json.RawMessage `json:"sortValues"`
		}
		if err := json.Unmarshal(raw, &page); err != nil || page.Items == nil || page.Total == nil {
			return nil, errors.New("unexpected Operate response; check that the URL points to the Operate v1 API, not a login page")
		}
		for _, item := range page.Items {
			if item.TenantID != "" && item.TenantID != "<default>" {
				continue
			}
			if item.BPMNProcessID == "" || item.Version < 1 {
				return nil, errors.New("Operate returned an invalid process definition")
			}
			existing := processes[item.BPMNProcessID]
			if item.Version > existing.LatestVersion {
				processes[item.BPMNProcessID] = Process{item.BPMNProcessID, item.Name, item.Version}
			}
		}
		count += len(page.Items)
		if count >= *page.Total {
			break
		}
		next := string(page.SortValues)
		if len(page.Items) == 0 || next == "" || next == "null" || next == "[]" || seen[next] {
			return nil, errors.New("Operate pagination did not advance; refresh the process list")
		}
		if count >= 10000 {
			return nil, errors.New("Operate has too many process definitions to load; enter the process ID manually")
		}
		seen[next] = true
		cursor = page.SortValues
	}
	result := make([]Process, 0, len(processes))
	for _, p := range processes {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].BPMNProcessID < result[j].BPMNProcessID })
	return result, nil
}

// Each refresh gets an isolated, in-memory session shared by all result pages.
func login(ctx context.Context, client *http.Client, base string, auth Auth) error {
	form := url.Values{"username": {auth.Username}, "password": {auth.Password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/login", strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("could not create Operate login request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(req)
	if err != nil {
		return errors.New("could not reach Operate login; check the URL and connection")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return errors.New("Operate login failed; check your username and password")
	}
	success := response.StatusCode >= 200 && response.StatusCode < 300
	if response.StatusCode == http.StatusFound || response.StatusCode == http.StatusSeeOther {
		location, err := response.Location()
		success = err == nil && location.Scheme == req.URL.Scheme && location.Host == req.URL.Host && !strings.Contains(location.Path, "login") && !strings.Contains(location.RawQuery, "error")
	}
	searchURL, _ := url.Parse(base + "/v1/process-definitions/search")
	if !success || len(client.Jar.Cookies(searchURL)) == 0 {
		return errors.New("Operate did not establish a login session; check credentials and whether this deployment uses Identity/SSO, which requires bearer-token authentication")
	}
	return nil
}
