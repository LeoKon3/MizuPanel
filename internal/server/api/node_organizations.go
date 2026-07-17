package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

const maxNodeOrganizationBodyBytes = 128 * 1024

type nodeGroupWriteRequest struct {
	Name string `json:"name"`
}

type nodeTagWriteRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type optionalNullableString struct {
	Present bool
	Value   *string
}

func (value *optionalNullableString) UnmarshalJSON(data []byte) error {
	value.Present = true
	if string(data) == "null" {
		value.Value = nil
		return nil
	}
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	value.Value = &decoded
	return nil
}

type batchNodeMetadataRequest struct {
	NodeIDs      []string               `json:"node_ids"`
	GroupID      optionalNullableString `json:"group_id"`
	AddTagIDs    []string               `json:"add_tag_ids"`
	RemoveTagIDs []string               `json:"remove_tag_ids"`
}

func (s *Server) handleNodeGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		groups, err := s.organizations.ListGroups(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list node groups")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
	case http.MethodPost:
		if !authorizeOrganizationMutation(r, true) {
			writeError(w, http.StatusForbidden, "origin or content type is not allowed")
			return
		}
		var request nodeGroupWriteRequest
		if !decodeOrganizationRequest(w, r, &request) {
			return
		}
		group, err := s.organizations.CreateGroup(r.Context(), request.Name)
		if err != nil {
			writeOrganizationError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, group)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNodeGroup(w http.ResponseWriter, r *http.Request) {
	id := singleOrganizationPathID(r.URL.Path, "/api/node-groups/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		if !authorizeOrganizationMutation(r, true) {
			writeError(w, http.StatusForbidden, "origin or content type is not allowed")
			return
		}
		var request nodeGroupWriteRequest
		if !decodeOrganizationRequest(w, r, &request) {
			return
		}
		group, err := s.organizations.UpdateGroup(r.Context(), id, request.Name)
		if err != nil {
			writeOrganizationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, group)
	case http.MethodDelete:
		if !authorizeOrganizationMutation(r, false) {
			writeError(w, http.StatusForbidden, "origin is not allowed")
			return
		}
		if err := s.organizations.DeleteGroup(r.Context(), id); err != nil {
			writeOrganizationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "PATCH, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNodeTags(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tags, err := s.organizations.ListTags(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list node tags")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
	case http.MethodPost:
		if !authorizeOrganizationMutation(r, true) {
			writeError(w, http.StatusForbidden, "origin or content type is not allowed")
			return
		}
		var request nodeTagWriteRequest
		if !decodeOrganizationRequest(w, r, &request) {
			return
		}
		tag, err := s.organizations.CreateTag(r.Context(), request.Name, request.Color)
		if err != nil {
			writeOrganizationError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, tag)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNodeTag(w http.ResponseWriter, r *http.Request) {
	id := singleOrganizationPathID(r.URL.Path, "/api/node-tags/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		if !authorizeOrganizationMutation(r, true) {
			writeError(w, http.StatusForbidden, "origin or content type is not allowed")
			return
		}
		var request nodeTagWriteRequest
		if !decodeOrganizationRequest(w, r, &request) {
			return
		}
		tag, err := s.organizations.UpdateTag(r.Context(), id, request.Name, request.Color)
		if err != nil {
			writeOrganizationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, tag)
	case http.MethodDelete:
		if !authorizeOrganizationMutation(r, false) {
			writeError(w, http.StatusForbidden, "origin is not allowed")
			return
		}
		if err := s.organizations.DeleteTag(r.Context(), id); err != nil {
			writeOrganizationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "PATCH, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBatchNodeMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		w.Header().Set("Allow", "PATCH")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrganizationMutation(r, true) {
		writeError(w, http.StatusForbidden, "origin or content type is not allowed")
		return
	}
	var request batchNodeMetadataRequest
	if !decodeOrganizationRequest(w, r, &request) {
		return
	}
	organizations, err := s.organizations.BatchUpdateMetadata(r.Context(), store.BatchNodeMetadataUpdate{
		NodeIDs:      request.NodeIDs,
		GroupIDSet:   request.GroupID.Present,
		GroupID:      request.GroupID.Value,
		AddTagIDs:    request.AddTagIDs,
		RemoveTagIDs: request.RemoveTagIDs,
	})
	if err != nil {
		writeOrganizationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": organizations})
}

func decodeOrganizationRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxNodeOrganizationBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return false
	}
	return true
}

func authorizeOrganizationMutation(r *http.Request, requireJSON bool) bool {
	if !sameOrigin(r) {
		return false
	}
	if !requireJSON {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func singleOrganizationPathID(path string, prefix string) string {
	value := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if value == "" || strings.Contains(value, "/") {
		return ""
	}
	return value
}

func writeOrganizationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNodeOrganizationInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNodeOrganizationConflict):
		writeError(w, http.StatusConflict, "group or tag name already exists")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "node, group, or tag not found")
	default:
		writeError(w, http.StatusInternalServerError, "node organization operation failed")
	}
}
