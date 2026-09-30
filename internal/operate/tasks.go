package operate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/mishankov/camunda-stub-worker/internal/bpmn"
	"github.com/mishankov/camunda-stub-worker/internal/domain"
)

// Task is a worker task discovered in a BPMN definition.
type Task = domain.ProcessTask

func GetProcessTasks(ctx context.Context, baseURL string, auth Auth, key, processID string) ([]Task, error) {
	if n, err := strconv.ParseInt(key, 10, 64); err != nil || n <= 0 {
		return nil, errors.New("select a deployed process version with a valid definition key")
	}
	client, base, err := newClient(ctx, baseURL, auth)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/process-definitions/"+key+"/xml", nil)
	if err != nil {
		return nil, err
	}
	// Accept the endpoint's media type; the body is validated as BPMN XML below.
	// Restricting this to application/xml can cause HTTP 406 for raw text XML.
	req.Header.Set("Accept", "*/*")
	if auth.Mode == "token" {
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Operate: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not load BPMN: Operate returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, bpmn.MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > bpmn.MaxSize {
		return nil, errors.New("BPMN exceeds the 8 MB limit")
	}
	return bpmn.Tasks(raw, processID)
}
