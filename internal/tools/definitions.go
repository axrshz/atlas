package tools

import (
	"encoding/json"

	"github.com/openai/openai-go/v3/shared"
)

// ToolDefinition describes one function the model may request.
// Function contains the local Go code that actually performs the operation.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema shared.FunctionParameters
	Function    func(input json.RawMessage) (string, error)
}

var ReadFileDefinition = ToolDefinition{
	Name:        "read_file",
	Description: "Read the contents of a given relative file path. Use this when you want to see what's inside a file. Do not use this with directory names.",
	InputSchema: objectSchema(map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "The relative path of a file in the working directory.",
		},
	}, []string{"path"}),
	Function: ReadFile,
}

var ListFilesDefinition = ToolDefinition{
	Name:        "list_files",
	Description: "List files and directories at a given relative path. If no path is provided, list files in the working directory.",
	InputSchema: objectSchema(map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "Optional relative path to list files from. Defaults to the working directory.",
		},
	}, nil),
	Function: ListFiles,
}

var EditFileDefinition = ToolDefinition{
	Name: "edit_file",
	Description: `Make edits to a text file.

Replace old_str with new_str in the given file. old_str and new_str must be different. old_str must match exactly once.

If the file specified with path does not exist, it will be created when old_str is empty.`,
	InputSchema: objectSchema(map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "The relative path to the file.",
		},
		"old_str": map[string]any{
			"type":        "string",
			"description": "Text to search for. It must match exactly once. Leave empty only when creating a new file.",
		},
		"new_str": map[string]any{
			"type":        "string",
			"description": "Text to replace old_str with, or the complete contents for a new file.",
		},
	}, []string{"path", "old_str", "new_str"}),
	Function: EditFile,
}

var DeleteFileDefinition = ToolDefinition{
	Name:        "delete_file",
	Description: "Delete a file at the given relative path. This operation is permanent and cannot be undone.",
	InputSchema: objectSchema(map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "The relative path of the file to delete.",
		},
	}, []string{"path"}),
	Function: DeleteFile,
}

var BashDefinition = ToolDefinition{
	Name: "bash",
	Description: `Execute a bash command in the working directory.

The command inherits the agent process environment and has the same filesystem and system access as the user running the agent.`,
	InputSchema: objectSchema(map[string]any{
		"command": map[string]any{
			"type":        "string",
			"description": "The bash command to execute.",
		},
	}, []string{"command"}),
	Function: Bash,
}

// DefaultTools returns every tool this agent exposes to the model.
func DefaultTools() []ToolDefinition {
	return []ToolDefinition{
		ReadFileDefinition,
		ListFilesDefinition,
		EditFileDefinition,
		DeleteFileDefinition,
		BashDefinition,
	}
}

func objectSchema(properties map[string]any, required []string) shared.FunctionParameters {
	schema := shared.FunctionParameters{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
