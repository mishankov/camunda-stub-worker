package bpmn

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
)

// MaxSize is the largest BPMN document the app accepts.
const MaxSize = 8 << 20

const bpmnNS = "http://www.omg.org/spec/BPMN/20100524/MODEL"
const zeebeNS = "http://camunda.org/schema/zeebe/1.0"

type xmlNode struct {
	XMLName  xml.Name
	ID       string    `xml:"id,attr"`
	Name     string    `xml:"name,attr"`
	Type     string    `xml:"type,attr"`
	Children []xmlNode `xml:",any"`
}

// Process is a top-level process found in a BPMN definitions document,
// together with the worker tasks declared on it.
type Process struct {
	BPMNProcessID string               `json:"bpmnProcessId"`
	Name          string               `json:"name"`
	Tasks         []domain.ProcessTask `json:"tasks"`
}

// Parse validates the document as BPMN XML and returns every top-level
// process with its Zeebe task definitions.
func Parse(raw []byte) ([]Process, error) {
	var root xmlNode
	if err := xml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("invalid BPMN XML: %w", err)
	}
	if root.XMLName.Space != bpmnNS || root.XMLName.Local != "definitions" {
		return nil, errors.New("expected BPMN definitions XML")
	}
	processes := []Process{}
	for _, process := range root.Children {
		if process.XMLName.Space != bpmnNS || process.XMLName.Local != "process" {
			continue
		}
		tasks := []domain.ProcessTask{}
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
						tasks = append(tasks, domain.ProcessTask{ID: n.ID, Name: n.Name, JobType: def.Type, Dynamic: strings.HasPrefix(strings.TrimSpace(def.Type), "=")})
					}
				}
			}
			for _, child := range n.Children {
				visit(child)
			}
		}
		visit(process)
		processes = append(processes, Process{BPMNProcessID: process.ID, Name: process.Name, Tasks: tasks})
	}
	return processes, nil
}

// Tasks returns the worker tasks of the given top-level process.
func Tasks(raw []byte, processID string) ([]domain.ProcessTask, error) {
	processes, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	for _, process := range processes {
		if process.BPMNProcessID == processID {
			return process.Tasks, nil
		}
	}
	return nil, errors.New("selected process was not found in the BPMN definition")
}
