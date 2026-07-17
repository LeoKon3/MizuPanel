package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

func TestNodeOrganizationAPICRUDAndEnrichedNodeList(t *testing.T) {
	mux, nodes, _, _, _ := testRouter(t)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-a", Name: "Alpha", Status: "online", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	groupBody := mutateJSON(t, mux, http.MethodPost, "/api/node-groups", `{"name":"Production"}`, http.StatusCreated)
	var group store.NodeGroup
	if err := json.Unmarshal(groupBody, &group); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	tagBody := mutateJSON(t, mux, http.MethodPost, "/api/node-tags", `{"name":"Database","color":"blue"}`, http.StatusCreated)
	var tag store.NodeTag
	if err := json.Unmarshal(tagBody, &tag); err != nil {
		t.Fatalf("decode tag: %v", err)
	}
	batchBody, _ := json.Marshal(map[string]any{"node_ids": []string{"node-a"}, "group_id": group.ID, "add_tag_ids": []string{tag.ID}})
	mutateJSON(t, mux, http.MethodPatch, "/api/nodes/batch/metadata", string(batchBody), http.StatusOK)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/nodes", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Nodes []NodeResponse `json:"nodes"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode nodes: %v", err)
	}
	if len(response.Nodes) != 1 || response.Nodes[0].Group == nil || response.Nodes[0].Group.ID != group.ID || len(response.Nodes[0].Tags) != 1 || response.Nodes[0].Tags[0].ID != tag.ID {
		t.Fatalf("node organization = %#v", response.Nodes)
	}

	mutateJSON(t, mux, http.MethodPatch, "/api/node-groups/"+group.ID, `{"name":"Primary"}`, http.StatusOK)
	mutateJSON(t, mux, http.MethodPatch, "/api/node-tags/"+tag.ID, `{"name":"Critical DB","color":"red"}`, http.StatusOK)
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/node-groups/"+group.ID, nil)
	deleteRequest.Host = "panel.example"
	deleteRequest.Header.Set("Origin", "http://panel.example")
	deleteRecorder := httptest.NewRecorder()
	mux.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete group status = %d, body = %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func TestNodeOrganizationAPIConflictRollbackAndOriginProtection(t *testing.T) {
	mux, nodes, _, _, _ := testRouter(t)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-a", Name: "Alpha", Status: "online", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	mutateJSON(t, mux, http.MethodPost, "/api/node-groups", `{"name":"Production"}`, http.StatusCreated)
	mutateJSON(t, mux, http.MethodPost, "/api/node-groups", `{"name":"production"}`, http.StatusConflict)
	mutateJSON(t, mux, http.MethodPatch, "/api/nodes/batch/metadata", `{"node_ids":["node-a","missing"],"group_id":null}`, http.StatusNotFound)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/node-tags", bytes.NewBufferString(`{"name":"Unsafe","color":"red"}`))
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://evil.example")
	request.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", recorder.Code)
	}
}

func TestNodeOrganizationAPIRequiresStrictJSONRequests(t *testing.T) {
	mux, _, _, _, _ := testRouter(t)

	request := httptest.NewRequest(http.MethodPost, "/api/node-tags", bytes.NewBufferString(`{"name":"Unsafe","color":"red"}`))
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	request.Header.Set("Content-Type", "application/jsonp")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("JSONP content type status = %d, want 403", recorder.Code)
	}

	mutateJSON(t, mux, http.MethodPost, "/api/node-groups", `{"name":"Production","unknown":true}`, http.StatusBadRequest)
	mutateJSON(t, mux, http.MethodPost, "/api/node-groups", `{"name":"Production"}{"name":"Staging"}`, http.StatusBadRequest)
}

func mutateJSON(t *testing.T, handler http.Handler, method string, path string, body string, wantStatus int) []byte {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status = %d, body = %s, want %d", method, path, recorder.Code, recorder.Body.String(), wantStatus)
	}
	return recorder.Body.Bytes()
}
