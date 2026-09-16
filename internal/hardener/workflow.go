package hardener

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"

	"go.yaml.in/yaml/v3"
)

// ParseWorkflow validates structure without evaluating expressions or scripts.
func ParseWorkflow(name string, data []byte) (Workflow, error) {
	w := Workflow{Path: name, Steps: []Step{}, ReusableJobs: []string{}}
	if len(data) == 0 || len(data) > MaxFileBytes {
		return w, errors.New("workflow is empty or exceeds the size limit")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return w, fmt.Errorf("invalid YAML: %s", bounded(err.Error(), MaxTextBytes))
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return w, errors.New("workflow must contain exactly one YAML document")
	}
	nodes := 0
	if err := validateNode(&document, 0, &nodes); err != nil {
		return w, err
	}
	if len(document.Content) != 1 {
		return w, errors.New("workflow document has no root")
	}
	root, err := mapping(document.Content[0], "workflow")
	if err != nil {
		return w, err
	}
	jobsNode := root["jobs"]
	if jobsNode == nil {
		return w, errors.New("workflow is missing jobs")
	}
	jobs, err := mapping(jobsNode, "jobs")
	if err != nil {
		return w, err
	}
	if len(jobs) == 0 {
		return w, errors.New("jobs must not be empty")
	}
	ids := make([]string, 0, len(jobs))
	for id := range jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if id == "" {
			return w, errors.New("job id must not be empty")
		}
		job, err := mapping(jobs[id], "job")
		if err != nil {
			return w, err
		}
		if uses := job["uses"]; uses != nil {
			if !stringNode(uses) || uses.Value == "" || job["steps"] != nil {
				return w, errors.New("reusable workflow job requires string uses and no steps")
			}
			w.ReusableJobs = append(w.ReusableJobs, id)
			continue
		}
		steps := job["steps"]
		if steps == nil || steps.Kind != yaml.SequenceNode {
			return w, errors.New("job requires a steps sequence or reusable workflow uses")
		}
		for index, node := range steps.Content {
			fields, err := mapping(node, "step")
			if err != nil {
				return w, err
			}
			run, uses, shell := fields["run"], fields["uses"], fields["shell"]
			if shell != nil && !stringNode(shell) {
				return w, errors.New("step shell must be a string")
			}
			if run != nil && uses != nil {
				return w, errors.New("step cannot combine run and uses")
			}
			if run == nil {
				if !stringNode(uses) || uses.Value == "" {
					return w, errors.New("step requires string run or uses")
				}
				w.OutsideSteps++
				continue
			}
			if !stringNode(run) {
				return w, errors.New("step run must be a string")
			}
			step := Step{JobID: id, Index: index, Run: run.Value, ShellExplicit: shell != nil,
				Location: Location{Kind: "run_scalar_start", Line: run.Line, Column: run.Column}}
			if shell != nil {
				step.Shell = shell.Value
			}
			w.Steps = append(w.Steps, step)
		}
	}
	return w, nil
}

func stringNode(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str"
}

func mapping(n *yaml.Node, context string) (map[string]*yaml.Node, error) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s must be a mapping", context)
	}
	result := make(map[string]*yaml.Node, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		key := n.Content[i]
		if !stringNode(key) {
			return nil, fmt.Errorf("%s keys must be strings", context)
		}
		result[key.Value] = n.Content[i+1]
	}
	return result, nil
}

func validateNode(n *yaml.Node, depth int, count *int) error {
	*count = *count + 1
	if depth > 64 || *count > 32768 {
		return errors.New("YAML nesting or node limit exceeded")
	}
	if n.Kind == yaml.AliasNode {
		return errors.New("YAML aliases are not accepted")
	}
	if n.Kind == yaml.MappingNode {
		if len(n.Content)%2 != 0 {
			return errors.New("invalid YAML mapping")
		}
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if !stringNode(key) {
				return errors.New("YAML mapping keys must be strings; merge keys are not accepted")
			}
			if key.Value == "<<" {
				return errors.New("YAML merge keys are not accepted")
			}
			if seen[key.Value] {
				return fmt.Errorf("duplicate YAML key at line %d", key.Line)
			}
			seen[key.Value] = true
		}
	}
	switch n.ShortTag() {
	case "", "!!map", "!!seq", "!!str", "!!null", "!!bool", "!!int", "!!float", "!!timestamp":
	default:
		return errors.New("unsupported YAML tag")
	}
	for _, child := range n.Content {
		if err := validateNode(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}
