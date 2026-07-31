package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const githubURL = "https://github.com/LeoKon3/MizuPanel"

func (s *Server) handleSystemAbout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    readVersion(),
		"github_url": githubURL,
	})
}

func (s *Server) handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.serverLogs == nil {
		writeError(w, http.StatusServiceUnavailable, "server logs unavailable")
		return
	}

	lines, err := serverLogLines(r.URL.Query().Get("lines"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid lines")
		return
	}
	snapshot := s.serverLogs.Snapshot(lines)
	writeJSON(w, http.StatusOK, map[string]any{
		"content":        snapshot.Content,
		"lines":          lines,
		"returned_lines": snapshot.ReturnedLines,
		"collected_at":   time.Now().UTC(),
		"started_at":     snapshot.StartedAt,
		"truncated":      snapshot.Truncated,
	})
}

func serverLogLines(value string) (int, error) {
	if value == "" {
		return 200, nil
	}
	lines, err := strconv.Atoi(value)
	if err != nil || lines <= 0 {
		return 0, errInvalidServerLogLines
	}
	return min(max(lines, 20), 2000), nil
}

var errInvalidServerLogLines = &serverLogQueryError{}

type serverLogQueryError struct{}

func (*serverLogQueryError) Error() string { return "invalid server log lines" }

func readVersion() string {
	dir, err := os.Getwd()
	if err != nil {
		return "dev"
	}
	for {
		content, err := os.ReadFile(filepath.Join(dir, "VERSION"))
		if err == nil {
			version := strings.TrimSpace(string(content))
			if version != "" {
				return version
			}
			return "dev"
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "dev"
		}
		dir = parent
	}
}
