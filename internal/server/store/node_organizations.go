package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

const (
	MaxNodeGroupNameRunes = 64
	MaxNodeTagNameRunes   = 32
	MaxNodeTagsPerNode    = 20
	MaxBatchMetadataNodes = 100
)

var (
	ErrNodeOrganizationInvalid  = errors.New("invalid node organization metadata")
	ErrNodeOrganizationConflict = errors.New("node organization metadata conflict")
)

var nodeTagColors = map[string]struct{}{
	"green": {},
	"teal":  {},
	"blue":  {},
	"amber": {},
	"red":   {},
	"gray":  {},
}

type NodeGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	NodeCount int       `json:"node_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NodeTag struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	NodeCount int       `json:"node_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NodeGroupSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type NodeTagSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type NodeOrganization struct {
	Group *NodeGroupSummary `json:"group"`
	Tags  []NodeTagSummary  `json:"tags"`
}

type BatchNodeMetadataUpdate struct {
	NodeIDs      []string
	GroupIDSet   bool
	GroupID      *string
	AddTagIDs    []string
	RemoveTagIDs []string
}

type NodeOrganizationStore struct {
	db      *sql.DB
	dialect serverdb.Dialect
}

func NewNodeOrganizationStore(db *sql.DB) *NodeOrganizationStore {
	return NewNodeOrganizationStoreWithDialect(db, serverdb.DialectSQLite)
}

func NewNodeOrganizationStoreWithDialect(db *sql.DB, dialect serverdb.Dialect) *NodeOrganizationStore {
	return &NodeOrganizationStore{db: db, dialect: dialect}
}

func (s *NodeOrganizationStore) CreateGroup(ctx context.Context, name string) (NodeGroup, error) {
	displayName, normalizedName, err := normalizeOrganizationName(name, MaxNodeGroupNameRunes, "group")
	if err != nil {
		return NodeGroup{}, err
	}
	id, err := organizationID("group")
	if err != nil {
		return NodeGroup{}, err
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO node_groups (id, name, normalized_name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, id, displayName, normalizedName, formatTime(now), formatTime(now)); err != nil {
		return NodeGroup{}, mapOrganizationWriteError(err)
	}
	return NodeGroup{ID: id, Name: displayName, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *NodeOrganizationStore) GetGroup(ctx context.Context, id string) (NodeGroup, error) {
	var group NodeGroup
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT g.id, g.name, COUNT(n.id), g.created_at, g.updated_at
		FROM node_groups g
		LEFT JOIN nodes n ON n.group_id = g.id
		WHERE g.id = ?
		GROUP BY g.id, g.name, g.created_at, g.updated_at
	`, id).Scan(&group.ID, &group.Name, &group.NodeCount, &createdAt, &updatedAt)
	if err != nil {
		return NodeGroup{}, err
	}
	if group.CreatedAt, err = parseTime(createdAt); err != nil {
		return NodeGroup{}, err
	}
	if group.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return NodeGroup{}, err
	}
	return group, nil
}

func (s *NodeOrganizationStore) ListGroups(ctx context.Context) ([]NodeGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.name, COUNT(n.id), g.created_at, g.updated_at
		FROM node_groups g
		LEFT JOIN nodes n ON n.group_id = g.id
		GROUP BY g.id, g.name, g.created_at, g.updated_at
		ORDER BY g.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]NodeGroup, 0)
	for rows.Next() {
		var group NodeGroup
		var createdAt, updatedAt string
		if err := rows.Scan(&group.ID, &group.Name, &group.NodeCount, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if group.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if group.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *NodeOrganizationStore) UpdateGroup(ctx context.Context, id string, name string) (NodeGroup, error) {
	displayName, normalizedName, err := normalizeOrganizationName(name, MaxNodeGroupNameRunes, "group")
	if err != nil {
		return NodeGroup{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE node_groups SET name = ?, normalized_name = ?, updated_at = ? WHERE id = ?`, displayName, normalizedName, formatTime(time.Now().UTC()), id)
	if err != nil {
		return NodeGroup{}, mapOrganizationWriteError(err)
	}
	if err := requireAffectedRow(result); err != nil {
		return NodeGroup{}, err
	}
	return s.GetGroup(ctx, id)
}

func (s *NodeOrganizationStore) DeleteGroup(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET group_id = NULL WHERE group_id = ?`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM node_groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if err := requireAffectedRow(result); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *NodeOrganizationStore) CreateTag(ctx context.Context, name string, color string) (NodeTag, error) {
	displayName, normalizedName, err := normalizeOrganizationName(name, MaxNodeTagNameRunes, "tag")
	if err != nil {
		return NodeTag{}, err
	}
	color, err = normalizeTagColor(color)
	if err != nil {
		return NodeTag{}, err
	}
	id, err := organizationID("tag")
	if err != nil {
		return NodeTag{}, err
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO node_tags (id, name, normalized_name, color, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, id, displayName, normalizedName, color, formatTime(now), formatTime(now)); err != nil {
		return NodeTag{}, mapOrganizationWriteError(err)
	}
	return NodeTag{ID: id, Name: displayName, Color: color, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *NodeOrganizationStore) GetTag(ctx context.Context, id string) (NodeTag, error) {
	var tag NodeTag
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.name, t.color, COUNT(l.node_id), t.created_at, t.updated_at
		FROM node_tags t
		LEFT JOIN node_tag_links l ON l.tag_id = t.id
		WHERE t.id = ?
		GROUP BY t.id, t.name, t.color, t.created_at, t.updated_at
	`, id).Scan(&tag.ID, &tag.Name, &tag.Color, &tag.NodeCount, &createdAt, &updatedAt)
	if err != nil {
		return NodeTag{}, err
	}
	if tag.CreatedAt, err = parseTime(createdAt); err != nil {
		return NodeTag{}, err
	}
	if tag.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return NodeTag{}, err
	}
	return tag, nil
}

func (s *NodeOrganizationStore) ListTags(ctx context.Context) ([]NodeTag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.color, COUNT(l.node_id), t.created_at, t.updated_at
		FROM node_tags t
		LEFT JOIN node_tag_links l ON l.tag_id = t.id
		GROUP BY t.id, t.name, t.color, t.created_at, t.updated_at
		ORDER BY t.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := make([]NodeTag, 0)
	for rows.Next() {
		var tag NodeTag
		var createdAt, updatedAt string
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.NodeCount, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if tag.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if tag.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (s *NodeOrganizationStore) UpdateTag(ctx context.Context, id string, name string, color string) (NodeTag, error) {
	displayName, normalizedName, err := normalizeOrganizationName(name, MaxNodeTagNameRunes, "tag")
	if err != nil {
		return NodeTag{}, err
	}
	color, err = normalizeTagColor(color)
	if err != nil {
		return NodeTag{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE node_tags SET name = ?, normalized_name = ?, color = ?, updated_at = ? WHERE id = ?`, displayName, normalizedName, color, formatTime(time.Now().UTC()), id)
	if err != nil {
		return NodeTag{}, mapOrganizationWriteError(err)
	}
	if err := requireAffectedRow(result); err != nil {
		return NodeTag{}, err
	}
	return s.GetTag(ctx, id)
}

func (s *NodeOrganizationStore) DeleteTag(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_tag_links WHERE tag_id = ?`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM node_tags WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if err := requireAffectedRow(result); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *NodeOrganizationStore) ListNodeOrganizations(ctx context.Context) (map[string]NodeOrganization, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.id, COALESCE(g.id, ''), COALESCE(g.name, ''), COALESCE(t.id, ''), COALESCE(t.name, ''), COALESCE(t.color, '')
		FROM nodes n
		LEFT JOIN node_groups g ON g.id = n.group_id
		LEFT JOIN node_tag_links l ON l.node_id = n.id
		LEFT JOIN node_tags t ON t.id = l.tag_id
		ORDER BY n.id ASC, t.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	organizations := make(map[string]NodeOrganization)
	for rows.Next() {
		var nodeID, groupID, groupName, tagID, tagName, tagColor string
		if err := rows.Scan(&nodeID, &groupID, &groupName, &tagID, &tagName, &tagColor); err != nil {
			return nil, err
		}
		organization, ok := organizations[nodeID]
		if !ok {
			organization = NodeOrganization{Tags: make([]NodeTagSummary, 0)}
		}
		if groupID != "" {
			organization.Group = &NodeGroupSummary{ID: groupID, Name: groupName}
		}
		if tagID != "" {
			organization.Tags = append(organization.Tags, NodeTagSummary{ID: tagID, Name: tagName, Color: tagColor})
		}
		organizations[nodeID] = organization
	}
	return organizations, rows.Err()
}

func (s *NodeOrganizationStore) GetNodeOrganization(ctx context.Context, nodeID string) (NodeOrganization, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE id = ?`, nodeID).Scan(&exists); err != nil {
		return NodeOrganization{}, err
	}
	if exists != 1 {
		return NodeOrganization{}, sql.ErrNoRows
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(g.id, ''), COALESCE(g.name, ''), COALESCE(t.id, ''), COALESCE(t.name, ''), COALESCE(t.color, '')
		FROM nodes n
		LEFT JOIN node_groups g ON g.id = n.group_id
		LEFT JOIN node_tag_links l ON l.node_id = n.id
		LEFT JOIN node_tags t ON t.id = l.tag_id
		WHERE n.id = ?
		ORDER BY t.name ASC
	`, nodeID)
	if err != nil {
		return NodeOrganization{}, err
	}
	defer rows.Close()
	organization := NodeOrganization{Tags: make([]NodeTagSummary, 0)}
	for rows.Next() {
		var groupID, groupName, tagID, tagName, tagColor string
		if err := rows.Scan(&groupID, &groupName, &tagID, &tagName, &tagColor); err != nil {
			return NodeOrganization{}, err
		}
		if groupID != "" {
			organization.Group = &NodeGroupSummary{ID: groupID, Name: groupName}
		}
		if tagID != "" {
			organization.Tags = append(organization.Tags, NodeTagSummary{ID: tagID, Name: tagName, Color: tagColor})
		}
	}
	return organization, rows.Err()
}

func (s *NodeOrganizationStore) BatchUpdateMetadata(ctx context.Context, update BatchNodeMetadataUpdate) (map[string]NodeOrganization, error) {
	nodeIDs, err := validateUniqueIDs(update.NodeIDs, MaxBatchMetadataNodes, "node")
	if err != nil {
		return nil, err
	}
	addTagIDs, err := validateUniqueIDsAllowEmpty(update.AddTagIDs, MaxNodeTagsPerNode, "tag")
	if err != nil {
		return nil, err
	}
	removeTagIDs, err := validateUniqueIDsAllowEmpty(update.RemoveTagIDs, MaxNodeTagsPerNode, "tag")
	if err != nil {
		return nil, err
	}
	if !update.GroupIDSet && len(addTagIDs) == 0 && len(removeTagIDs) == 0 {
		return nil, fmt.Errorf("%w: no metadata changes requested", ErrNodeOrganizationInvalid)
	}
	removeSet := stringSet(removeTagIDs)
	for _, tagID := range addTagIDs {
		if _, exists := removeSet[tagID]; exists {
			return nil, fmt.Errorf("%w: tag cannot be added and removed together", ErrNodeOrganizationInvalid)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if update.GroupIDSet && update.GroupID != nil {
		if err := requireIDInTx(ctx, tx, "node_groups", *update.GroupID); err != nil {
			return nil, err
		}
	}
	if err := requireIDsInTx(ctx, tx, "nodes", nodeIDs); err != nil {
		return nil, err
	}
	allTagIDs := append(append([]string{}, addTagIDs...), removeTagIDs...)
	if err := requireIDsInTx(ctx, tx, "node_tags", uniqueStrings(allTagIDs)); err != nil {
		return nil, err
	}

	currentTags, err := nodeTagSetsInTx(ctx, tx, nodeIDs)
	if err != nil {
		return nil, err
	}
	for _, nodeID := range nodeIDs {
		set := currentTags[nodeID]
		for _, tagID := range removeTagIDs {
			delete(set, tagID)
		}
		for _, tagID := range addTagIDs {
			set[tagID] = struct{}{}
		}
		if len(set) > MaxNodeTagsPerNode {
			return nil, fmt.Errorf("%w: node %s exceeds tag limit", ErrNodeOrganizationInvalid, nodeID)
		}
	}

	if update.GroupIDSet {
		query := `UPDATE nodes SET group_id = ? WHERE id IN (` + placeholders(len(nodeIDs)) + `)`
		args := make([]any, 0, len(nodeIDs)+1)
		if update.GroupID == nil {
			args = append(args, nil)
		} else {
			args = append(args, *update.GroupID)
		}
		args = append(args, stringsToAny(nodeIDs)...)
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return nil, err
		}
	}
	if len(removeTagIDs) > 0 {
		query := `DELETE FROM node_tag_links WHERE node_id IN (` + placeholders(len(nodeIDs)) + `) AND tag_id IN (` + placeholders(len(removeTagIDs)) + `)`
		args := append(stringsToAny(nodeIDs), stringsToAny(removeTagIDs)...)
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return nil, err
		}
	}
	if len(addTagIDs) > 0 {
		insert := `INSERT OR IGNORE INTO node_tag_links (node_id, tag_id, created_at) VALUES (?, ?, ?)`
		if s.dialect == serverdb.DialectMySQL {
			insert = `INSERT IGNORE INTO node_tag_links (node_id, tag_id, created_at) VALUES (?, ?, ?)`
		}
		statement, err := tx.PrepareContext(ctx, insert)
		if err != nil {
			return nil, err
		}
		defer statement.Close()
		now := formatTime(time.Now().UTC())
		for _, nodeID := range nodeIDs {
			for _, tagID := range addTagIDs {
				if _, err := statement.ExecContext(ctx, nodeID, tagID, now); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	organizations := make(map[string]NodeOrganization, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		organization, err := s.GetNodeOrganization(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		organizations[nodeID] = organization
	}
	return organizations, nil
}

func normalizeOrganizationName(name string, maxRunes int, kind string) (string, string, error) {
	displayName := strings.TrimSpace(name)
	if displayName == "" {
		return "", "", fmt.Errorf("%w: %s name is required", ErrNodeOrganizationInvalid, kind)
	}
	if utf8.RuneCountInString(displayName) > maxRunes {
		return "", "", fmt.Errorf("%w: %s name is too long", ErrNodeOrganizationInvalid, kind)
	}
	return displayName, strings.ToLower(displayName), nil
}

func normalizeTagColor(color string) (string, error) {
	color = strings.ToLower(strings.TrimSpace(color))
	if color == "" {
		color = "gray"
	}
	if _, ok := nodeTagColors[color]; !ok {
		return "", fmt.Errorf("%w: unsupported tag color", ErrNodeOrganizationInvalid)
	}
	return color, nil
}

func organizationID(prefix string) (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(bytes[:]), nil
}

func mapOrganizationWriteError(err error) error {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate entry") {
		return fmt.Errorf("%w: name already exists", ErrNodeOrganizationConflict)
	}
	return err
}

func requireAffectedRow(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func validateUniqueIDs(values []string, max int, kind string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: at least one %s id is required", ErrNodeOrganizationInvalid, kind)
	}
	return validateUniqueIDsAllowEmpty(values, max, kind)
}

func validateUniqueIDsAllowEmpty(values []string, max int, kind string) ([]string, error) {
	if len(values) > max {
		return nil, fmt.Errorf("%w: too many %s ids", ErrNodeOrganizationInvalid, kind)
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%w: %s id is required", ErrNodeOrganizationInvalid, kind)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("%w: duplicate %s id", ErrNodeOrganizationInvalid, kind)
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func requireIDInTx(ctx context.Context, tx *sql.Tx, table string, id string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id = ?`, id).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func requireIDsInTx(ctx context.Context, tx *sql.Tx, table string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id IN (`+placeholders(len(ids))+`)`, stringsToAny(ids)...).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return sql.ErrNoRows
	}
	return nil
}

func nodeTagSetsInTx(ctx context.Context, tx *sql.Tx, nodeIDs []string) (map[string]map[string]struct{}, error) {
	sets := make(map[string]map[string]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		sets[nodeID] = make(map[string]struct{})
	}
	rows, err := tx.QueryContext(ctx, `SELECT node_id, tag_id FROM node_tag_links WHERE node_id IN (`+placeholders(len(nodeIDs))+`)`, stringsToAny(nodeIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID, tagID string
		if err := rows.Scan(&nodeID, &tagID); err != nil {
			return nil, err
		}
		sets[nodeID][tagID] = struct{}{}
	}
	return sets, rows.Err()
}

func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func stringsToAny(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
