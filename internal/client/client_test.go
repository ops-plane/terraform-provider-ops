package client

import "testing"

func TestNewScheme(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://ops.example.com":      true,
		"http://localhost:8080":        true,
		"http://LOCALHOST":             true,
		"http://ops.localhost":         true,
		"http://127.0.0.1:8080":        true,
		"http://127.5.0.1":             true,
		"http://[::1]:8080":            true,
		"http://ops.example.com":       false,
		"http://10.0.0.1":              false,
		"http://localhost.example.com": false,
		"ftp://localhost":              false,
	} {
		if _, err := New(url, "t"); (err == nil) != ok {
			t.Errorf("New(%q): err = %v, want ok = %v", url, err, ok)
		}
	}
}
