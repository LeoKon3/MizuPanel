package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
