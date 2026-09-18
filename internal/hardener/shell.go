package hardener

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The runner may fall back from Bash to sh. Both are supported by the rules.
// This marker is internal; it is never accepted as a literal shell setting.
const defaultPOSIXShell = "bash-or-sh"

// defaultShell preserves presence: an empty or unsupported shell blocks fallback.
func defaultShell(node *yaml.Node, context string) (*yaml.Node, error) {
	if node == nil {
		return nil, nil
	}
	defaults, err := mapping(node, context+" defaults")
	if err != nil {
		return nil, err
	}
	if defaults["run"] == nil {
		return nil, nil
	}
	run, err := mapping(defaults["run"], context+" defaults.run")
	if err != nil {
		return nil, err
	}
	shell := run["shell"]
	if shell != nil && !stringNode(shell) {
		return nil, fmt.Errorf("%s defaults.run.shell must be a string", context)
	}
	return shell, nil
}

// resolveShell returns only a supported shell or an empty, unresolved result.
// Explicit settings win even when the runner or container cannot be inferred.
func resolveShell(step, job, workflow, runner, container *yaml.Node) string {
	for _, shell := range []*yaml.Node{step, job, workflow} {
		if shell != nil {
			if stringNode(shell) && (shell.Value == "bash" || shell.Value == "sh") {
				return shell.Value
			}
			return ""
		}
	}
	if !stringNode(runner) {
		return ""
	}
	ubuntu := false
	switch runner.Value {
	case "ubuntu-latest", "ubuntu-22.04", "ubuntu-24.04", "ubuntu-26.04":
		ubuntu = true
	case "macos-latest", "macos-14", "macos-15", "macos-26":
	default:
		return ""
	}
	if container == nil {
		return defaultPOSIXShell
	}
	if !ubuntu {
		return ""
	}
	image := container
	if container.Kind == yaml.MappingNode {
		fields, err := mapping(container, "job container")
		if err != nil {
			return ""
		}
		image = fields["image"]
	}
	if !stringNode(image) || strings.TrimSpace(image.Value) == "" || strings.Contains(image.Value, "${{") {
		return ""
	}
	return "sh"
}
