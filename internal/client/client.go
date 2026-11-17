// Package client talks to OPS's JSON API (/api/v1) with a token: objects by id, PATCH of the fields sent,
// errors as {"error": message, "fields": {field: message}}.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Client is an OPS server and the token it's called with.
type Client struct {
	BaseURL *url.URL // e.g. https://ops.example.com
	Token   string
	HTTP    *http.Client
}

// New is a client for baseURL (the server's address, without /api/v1).
func New(baseURL, token string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("url %q: an absolute URL such as https://ops.example.com", baseURL)
	}
	return &Client{BaseURL: u, Token: token, HTTP: &http.Client{Timeout: 2 * time.Minute}}, nil
}

// Error is an error answer of the API.
type Error struct {
	Status  int
	Message string
	Fields  map[string]string
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("OPS answered %d: %s", e.Status, e.Message)
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		msg += fmt.Sprintf("\n  %s: %s", k, e.Fields[k])
	}
	return msg
}

// IsNotFound: the object doesn't exist (anymore), or the token may not see it.
func IsNotFound(err error) bool {
	e, ok := err.(*Error)
	return ok && e.Status == http.StatusNotFound
}

// Do sends method path (under /api/v1; with a query if any) with body in as JSON (nil: none) and decodes the answer
// into out (nil: ignored).
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL.String()+"/api/v1"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		e := &Error{Status: res.StatusCode}
		if json.Unmarshal(b, &struct {
			Error  *string            `json:"error"`
			Fields *map[string]string `json:"fields"`
		}{&e.Message, &e.Fields}) != nil || e.Message == "" {
			e.Message = strings.TrimSpace(string(b))
		}
		return e
	}
	if out == nil || len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, out)
}

// Path joins segments into an escaped path ("/projects", id, …).
func Path(segments ...string) string {
	var b strings.Builder
	for _, s := range segments {
		b.WriteString("/")
		b.WriteString(url.PathEscape(strings.TrimPrefix(s, "/")))
	}
	return b.String()
}

// Objects, as the API returns them.

type Project struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RunnerPool  string `json:"runner_pool"`
	Default     bool   `json:"default"`
}

type EnvironmentValue struct {
	TemplateID string `json:"template_id"`
	Input      string `json:"input"`
	Value      string `json:"value"`
	KeepOwn    bool   `json:"keep_own"`
}

type Workspace struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ProjectID      string `json:"project_id"`
	AutoApply      bool   `json:"auto_apply"`
	ExecutionMode  string `json:"execution_mode"`
	DriftDetection bool   `json:"drift_detection"`
}

type Variable struct {
	ID        string  `json:"id"`
	Category  string  `json:"category"`
	Key       string  `json:"key"`
	Value     *string `json:"value"` // nil: sensitive
	Sensitive bool    `json:"sensitive"`
	HCL       bool    `json:"hcl"`
}

type VariableSet struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Global       bool     `json:"global"`
	Priority     bool     `json:"priority"`
	ProjectIDs   []string `json:"project_ids"`
	WorkspaceIDs []string `json:"workspace_ids"`
}

type VCSConnection struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Provider     string   `json:"provider"`
	BaseURL      string   `json:"base_url"`
	Scope        string   `json:"scope"`
	ProjectIDs   []string `json:"project_ids"`
	WorkspaceIDs []string `json:"workspace_ids"`
}

type Runner struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Pool  string `json:"pool"`
	Token string `json:"token"` // only when created with a new one
}

type VCSModule struct {
	Namespace    string   `json:"namespace"`
	Name         string   `json:"name"`
	System       string   `json:"system"`
	ConnectionID string   `json:"connection_id"`
	Repo         string   `json:"repo"`
	Dir          string   `json:"dir"`
	TagPrefix    string   `json:"tag_prefix"`
	SyncError    string   `json:"sync_error"`
	Versions     []string `json:"versions"`
}

type TemplateCatalog struct {
	ConnectionID string `json:"connection_id"`
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	Dir          string `json:"dir"`
	Mode         string `json:"mode"`
	Commit       string `json:"commit"`
	SyncError    string `json:"sync_error"`
}
