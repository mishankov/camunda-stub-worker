package operate

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type Task struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	JobType string `json:"jobType"`
	Dynamic bool   `json:"dynamic"`
}

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
	raw, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 8<<20 {
		return nil, errors.New("BPMN exceeds the 8 MB limit")
	}
	return parseTasks(raw, processID)
}

const bpmnNS = "http://www.omg.org/spec/BPMN/20100524/MODEL"
const zeebeNS = "http://camunda.org/schema/zeebe/1.0"

type xmlNode struct {
	XMLName  xml.Name
	ID       string    `xml:"id,attr"`
	Name     string    `xml:"name,attr"`
	Type     string    `xml:"type,attr"`
	Children []xmlNode `xml:",any"`
}

func parseTasks(raw []byte, processID string) ([]Task, error) {
	var root xmlNode
	if err := xml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("invalid BPMN XML: %w", err)
	}
	if root.XMLName.Space != bpmnNS || root.XMLName.Local != "definitions" {
		return nil, errors.New("expected BPMN definitions XML")
	}
	tasks := []Task{}
	var visit func(xmlNode)
	visit = func(n xmlNode) {
		if n.XMLName.Space != bpmnNS {
			return
		}
		for _, extension := range n.Children {
			if extension.XMLName.Space != bpmnNS || extension.XMLName.Local != "extensionElements" {
				continue
			}
			for _, def := range extension.Children {
				if def.XMLName.Space == zeebeNS && def.XMLName.Local == "taskDefinition" && def.Type != "" {
					tasks = append(tasks, Task{ID: n.ID, Name: n.Name, JobType: def.Type, Dynamic: strings.HasPrefix(strings.TrimSpace(def.Type), "=")})
				}
			}
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	for _, process := range root.Children {
		if process.XMLName.Space == bpmnNS && process.XMLName.Local == "process" && process.ID == processID {
			visit(process)
			return tasks, nil
		}
	}
	return nil, errors.New("selected process was not found in the BPMN definition")
}
