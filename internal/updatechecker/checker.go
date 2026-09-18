package updatechecker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const LatestReleaseAPI = "https://api.github.com/repos/mishankov/camunda-stub-worker/releases/latest"

var versionPattern = regexp.MustCompile(`^[vV]?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

type Info struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseUrl"`
}

type Client struct {
	HTTPClient *http.Client
	Endpoint   string
}

func (c Client) Check(ctx context.Context, currentVersion string) (Info, error) {
	if _, err := parseVersion(currentVersion); err != nil {
		return Info{}, fmt.Errorf("invalid current version: %w", err)
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = LatestReleaseAPI
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Info{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "camunda-stub-worker/"+currentVersion)
	resp, err := client.Do(req)
	if err != nil {
		return Info{}, fmt.Errorf("check for updates: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return Info{}, fmt.Errorf("check for updates: GitHub returned %s", resp.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return Info{}, fmt.Errorf("check for updates: decode response: %w", err)
	}
	if release.TagName == "" || release.HTMLURL == "" {
		return Info{}, errors.New("check for updates: release response is incomplete")
	}
	latest, err := parseVersion(release.TagName)
	if err != nil {
		return Info{}, fmt.Errorf("invalid release version %q: %w", release.TagName, err)
	}
	current, _ := parseVersion(currentVersion)
	return Info{
		CurrentVersion:  normalizeVersion(currentVersion),
		LatestVersion:   normalizeVersion(release.TagName),
		UpdateAvailable: compareVersions(latest, current) > 0,
		ReleaseURL:      release.HTMLURL,
	}, nil
}

type version struct {
	major, minor, patch int
	pre                 []string
}

func parseVersion(value string) (version, error) {
	matches := versionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if matches == nil {
		return version{}, errors.New("must be a semantic version such as 1.2.3")
	}
	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return version{}, errors.New("major version is too large")
	}
	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return version{}, errors.New("minor version is too large")
	}
	patch, err := strconv.Atoi(matches[3])
	if err != nil {
		return version{}, errors.New("patch version is too large")
	}
	var pre []string
	if matches[4] != "" {
		pre = strings.Split(matches[4], ".")
		for _, identifier := range pre {
			if len(identifier) > 1 && identifier[0] == '0' && allDigits(identifier) {
				return version{}, errors.New("numeric prerelease identifiers must not contain leading zeroes")
			}
		}
	}
	return version{major: major, minor: minor, patch: patch, pre: pre}, nil
}

func normalizeVersion(value string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(value), "v"), "V")
}

func compareVersions(a, b version) int {
	for _, pair := range [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(a.pre) == 0 && len(b.pre) == 0 {
		return 0
	}
	if len(a.pre) == 0 {
		return 1
	}
	if len(b.pre) == 0 {
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if result := compareIdentifier(a.pre[i], b.pre[i]); result != 0 {
			return result
		}
	}
	if len(a.pre) < len(b.pre) {
		return -1
	}
	if len(a.pre) > len(b.pre) {
		return 1
	}
	return 0
}

func compareIdentifier(a, b string) int {
	aNumeric, bNumeric := allDigits(a), allDigits(b)
	if aNumeric && bNumeric {
		if len(a) < len(b) {
			return -1
		}
		if len(a) > len(b) {
			return 1
		}
		return strings.Compare(a, b)
	}
	if aNumeric != bNumeric {
		if aNumeric {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
