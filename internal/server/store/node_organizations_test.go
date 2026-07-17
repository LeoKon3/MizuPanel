package store

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestNodeOrganizationStoreCreatesNormalizesAndListsMetadata(t *testing.T) {
	database := openTestDB(t)
	nodes := NewNodeStore(database)
	organizations := NewNodeOrganizationStore(database)
	for _, nodeID := range []string{"node-a", "node-b"} {
		if err := nodes.Upsert(t.Context(), Node{ID: nodeID, Name: nodeID, Status: "online", LastSeenAt: time.Now().UTC()}); err != nil {
			t.Fatalf("upsert %s: %v", nodeID, err)
		}
	}
	group, err := organizations.CreateGroup(t.Context(), "  Production  ")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if group.Name != "Production" {
		t.Fatalf("group name = %q", group.Name)
	}
	if _, err := organizations.CreateGroup(t.Context(), "production"); !errors.Is(err, ErrNodeOrganizationConflict) {
		t.Fatalf("duplicate group err = %v", err)
	}
	tag, err := organizations.CreateTag(t.Context(), " Database ", "BLUE")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if tag.Name != "Database" || tag.Color != "blue" {
		t.Fatalf("tag = %#v", tag)
	}
	if _, err := organizations.CreateTag(t.Context(), "database", "red"); !errors.Is(err, ErrNodeOrganizationConflict) {
		t.Fatalf("duplicate tag err = %v", err)
	}

	updated, err := organizations.BatchUpdateMetadata(t.Context(), BatchNodeMetadataUpdate{
		NodeIDs:    []string{"node-a", "node-b"},
		GroupIDSet: true,
		GroupID:    &group.ID,
		AddTagIDs:  []string{tag.ID},
	})
	if err != nil {
		t.Fatalf("batch update: %v", err)
	}
	for _, nodeID := range []string{"node-a", "node-b"} {
		if updated[nodeID].Group == nil || updated[nodeID].Group.ID != group.ID || len(updated[nodeID].Tags) != 1 || updated[nodeID].Tags[0].ID != tag.ID {
			t.Fatalf("organization for %s = %#v", nodeID, updated[nodeID])
		}
	}
	groups, err := organizations.ListGroups(t.Context())
	if err != nil || len(groups) != 1 || groups[0].NodeCount != 2 {
		t.Fatalf("groups = %#v, err = %v", groups, err)
	}
	tags, err := organizations.ListTags(t.Context())
	if err != nil || len(tags) != 1 || tags[0].NodeCount != 2 {
		t.Fatalf("tags = %#v, err = %v", tags, err)
	}
}

func TestNodeOrganizationStorePreservesMetadataAcrossAgentUpsert(t *testing.T) {
	database := openTestDB(t)
	nodes := NewNodeStore(database)
	organizations := NewNodeOrganizationStore(database)
	now := time.Now().UTC()
	if err := nodes.Upsert(t.Context(), Node{ID: "node-a", Name: "old", Status: "online", LastSeenAt: now}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	group, _ := organizations.CreateGroup(t.Context(), "Production")
	tag, _ := organizations.CreateTag(t.Context(), "Database", "blue")
	if _, err := organizations.BatchUpdateMetadata(t.Context(), BatchNodeMetadataUpdate{NodeIDs: []string{"node-a"}, GroupIDSet: true, GroupID: &group.ID, AddTagIDs: []string{tag.ID}}); err != nil {
		t.Fatalf("organize node: %v", err)
	}
	if err := nodes.Upsert(t.Context(), Node{ID: "node-a", Name: "agent-updated", Status: "online", LastSeenAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("agent upsert: %v", err)
	}
	organization, err := organizations.GetNodeOrganization(t.Context(), "node-a")
	if err != nil {
		t.Fatalf("get organization: %v", err)
	}
	if organization.Group == nil || organization.Group.ID != group.ID || len(organization.Tags) != 1 || organization.Tags[0].ID != tag.ID {
		t.Fatalf("organization after upsert = %#v", organization)
	}
}

func TestNodeOrganizationStoreBatchUpdateRollsBackInvalidTargets(t *testing.T) {
	database := openTestDB(t)
	nodes := NewNodeStore(database)
	organizations := NewNodeOrganizationStore(database)
	if err := nodes.Upsert(t.Context(), Node{ID: "node-a", Name: "a", Status: "online", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	group, _ := organizations.CreateGroup(t.Context(), "Production")
	_, err := organizations.BatchUpdateMetadata(t.Context(), BatchNodeMetadataUpdate{NodeIDs: []string{"node-a", "missing"}, GroupIDSet: true, GroupID: &group.ID})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("batch err = %v, want sql.ErrNoRows", err)
	}
	organization, err := organizations.GetNodeOrganization(t.Context(), "node-a")
	if err != nil {
		t.Fatalf("get organization: %v", err)
	}
	if organization.Group != nil {
		t.Fatalf("group changed after rollback: %#v", organization.Group)
	}
}

func TestNodeOrganizationStoreDeleteGroupTagAndNodeCleansRelationships(t *testing.T) {
	database := openTestDB(t)
	nodes := NewNodeStore(database)
	organizations := NewNodeOrganizationStore(database)
	if err := nodes.Upsert(t.Context(), Node{ID: "node-a", Name: "a", Status: "online", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	group, _ := organizations.CreateGroup(t.Context(), "Production")
	tag, _ := organizations.CreateTag(t.Context(), "Database", "blue")
	if _, err := organizations.BatchUpdateMetadata(t.Context(), BatchNodeMetadataUpdate{NodeIDs: []string{"node-a"}, GroupIDSet: true, GroupID: &group.ID, AddTagIDs: []string{tag.ID}}); err != nil {
		t.Fatalf("organize: %v", err)
	}
	if err := organizations.DeleteGroup(t.Context(), group.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	organization, _ := organizations.GetNodeOrganization(t.Context(), "node-a")
	if organization.Group != nil || len(organization.Tags) != 1 {
		t.Fatalf("organization after group delete = %#v", organization)
	}
	if err := organizations.DeleteTag(t.Context(), tag.ID); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	organization, _ = organizations.GetNodeOrganization(t.Context(), "node-a")
	if len(organization.Tags) != 0 {
		t.Fatalf("tags after delete = %#v", organization.Tags)
	}
	tag, _ = organizations.CreateTag(t.Context(), "Critical", "red")
	if _, err := organizations.BatchUpdateMetadata(t.Context(), BatchNodeMetadataUpdate{NodeIDs: []string{"node-a"}, AddTagIDs: []string{tag.ID}}); err != nil {
		t.Fatalf("re-tag: %v", err)
	}
	if err := nodes.Delete(t.Context(), "node-a"); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	var linkCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM node_tag_links WHERE node_id = 'node-a'`).Scan(&linkCount); err != nil {
		t.Fatalf("count links: %v", err)
	}
	if linkCount != 0 {
		t.Fatalf("link count = %d, want 0", linkCount)
	}
}

func TestNodeOrganizationStoreEnforcesBatchAndTagLimits(t *testing.T) {
	database := openTestDB(t)
	nodes := NewNodeStore(database)
	organizations := NewNodeOrganizationStore(database)
	if err := nodes.Upsert(t.Context(), Node{ID: "node-a", Name: "a", Status: "online", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	tagIDs := make([]string, 0, MaxNodeTagsPerNode+1)
	for index := 0; index <= MaxNodeTagsPerNode; index++ {
		tag, err := organizations.CreateTag(t.Context(), fmt.Sprintf("tag-%02d", index), "blue")
		if err != nil {
			t.Fatalf("create tag %d: %v", index, err)
		}
		tagIDs = append(tagIDs, tag.ID)
	}
	if _, err := organizations.BatchUpdateMetadata(t.Context(), BatchNodeMetadataUpdate{NodeIDs: []string{"node-a"}, AddTagIDs: tagIDs[:MaxNodeTagsPerNode]}); err != nil {
		t.Fatalf("add maximum tags: %v", err)
	}
	if _, err := organizations.BatchUpdateMetadata(t.Context(), BatchNodeMetadataUpdate{NodeIDs: []string{"node-a"}, AddTagIDs: tagIDs[MaxNodeTagsPerNode:]}); !errors.Is(err, ErrNodeOrganizationInvalid) {
		t.Fatalf("tag limit err = %v, want ErrNodeOrganizationInvalid", err)
	}
	organization, err := organizations.GetNodeOrganization(t.Context(), "node-a")
	if err != nil {
		t.Fatalf("get organization: %v", err)
	}
	if len(organization.Tags) != MaxNodeTagsPerNode {
		t.Fatalf("tag count = %d, want %d", len(organization.Tags), MaxNodeTagsPerNode)
	}

	tooManyNodes := make([]string, MaxBatchMetadataNodes+1)
	for index := range tooManyNodes {
		tooManyNodes[index] = fmt.Sprintf("node-%03d", index)
	}
	invalidUpdates := []BatchNodeMetadataUpdate{
		{NodeIDs: []string{"node-a", "node-a"}, GroupIDSet: true},
		{NodeIDs: []string{"node-a"}, AddTagIDs: []string{tagIDs[0]}, RemoveTagIDs: []string{tagIDs[0]}},
		{NodeIDs: []string{"node-a"}},
		{NodeIDs: tooManyNodes, GroupIDSet: true},
	}
	for index, update := range invalidUpdates {
		if _, err := organizations.BatchUpdateMetadata(t.Context(), update); !errors.Is(err, ErrNodeOrganizationInvalid) {
			t.Fatalf("invalid update %d err = %v, want ErrNodeOrganizationInvalid", index, err)
		}
	}
}
