package servicecenter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

type Store struct {
	db      *sql.DB
	dialect serverdb.Dialect
}

func NewStore(db *sql.DB, dialect serverdb.Dialect) *Store {
	return &Store{db: db, dialect: dialect}
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Dialect() serverdb.Dialect {
	return s.dialect
}

func (s *Store) Create(ctx context.Context, input ServiceInput) (Service, error) {
	input, normalizedName, err := normalizeInput(input)
	if err != nil {
		return Service{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	service := Service{
		ID:          uuid.NewString(),
		Name:        input.Name,
		Description: input.Description,
		Resources:   input.Resources,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Service{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO application_services (id, name, normalized_name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, service.ID, service.Name, normalizedName, service.Description, now, now); err != nil {
		return Service{}, normalizeStoreError(err)
	}
	resources, err := insertResources(ctx, tx, service.ID, input.Resources, now)
	if err != nil {
		return Service{}, err
	}
	if err := tx.Commit(); err != nil {
		return Service{}, normalizeStoreError(err)
	}
	service.Resources = resources
	return service, nil
}

func (s *Store) Update(ctx context.Context, id string, input ServiceInput) (Service, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Service{}, fmt.Errorf("%w: 服务 ID 不能为空", ErrInvalid)
	}
	input, normalizedName, err := normalizeInput(input)
	if err != nil {
		return Service{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Service{}, err
	}
	defer tx.Rollback()
	var createdAt string
	if err := tx.QueryRowContext(ctx, `SELECT created_at FROM application_services WHERE id = ?`, id).Scan(&createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Service{}, ErrNotFound
		}
		return Service{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE application_services SET name = ?, normalized_name = ?, description = ?, updated_at = ? WHERE id = ?`, input.Name, normalizedName, input.Description, now, id); err != nil {
		return Service{}, normalizeStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM application_service_resources WHERE service_id = ?`, id); err != nil {
		return Service{}, err
	}
	resources, err := insertResources(ctx, tx, id, input.Resources, now)
	if err != nil {
		return Service{}, err
	}
	if err := tx.Commit(); err != nil {
		return Service{}, normalizeStoreError(err)
	}
	return Service{ID: id, Name: input.Name, Description: input.Description, Resources: resources, CreatedAt: createdAt, UpdatedAt: now}, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM application_services WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (Service, error) {
	services, err := s.queryServices(ctx, `WHERE s.id = ?`, strings.TrimSpace(id))
	if err != nil {
		return Service{}, err
	}
	if len(services) == 0 {
		return Service{}, ErrNotFound
	}
	return services[0], nil
}

func (s *Store) List(ctx context.Context) ([]Service, error) {
	return s.queryServices(ctx, "")
}

func (s *Store) queryServices(ctx context.Context, condition string, args ...any) ([]Service, error) {
	query := `SELECT s.id, s.name, s.description, s.created_at, s.updated_at,
		r.id, r.resource_type, r.scope_id, r.resource_kind, r.namespace, r.resource_key, r.display_name, r.created_at
		FROM application_services s
		LEFT JOIN application_service_resources r ON r.service_id = s.id ` + condition + `
		ORDER BY LOWER(s.name), s.id, r.resource_type, r.display_name, r.id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	services := make([]Service, 0)
	indexes := make(map[string]int)
	for rows.Next() {
		var serviceID, name, description, createdAt, updatedAt string
		var resourceID, resourceType, scopeID, resourceKind, namespace, resourceKey, displayName, resourceCreated sql.NullString
		if err := rows.Scan(&serviceID, &name, &description, &createdAt, &updatedAt, &resourceID, &resourceType, &scopeID, &resourceKind, &namespace, &resourceKey, &displayName, &resourceCreated); err != nil {
			return nil, err
		}
		index, exists := indexes[serviceID]
		if !exists {
			index = len(services)
			indexes[serviceID] = index
			services = append(services, Service{ID: serviceID, Name: name, Description: description, Resources: []Resource{}, CreatedAt: createdAt, UpdatedAt: updatedAt})
		}
		if resourceID.Valid {
			services[index].Resources = append(services[index].Resources, Resource{
				ID:           resourceID.String,
				ServiceID:    serviceID,
				ResourceType: ResourceType(resourceType.String),
				ScopeID:      scopeID.String,
				ResourceKind: resourceKind.String,
				Namespace:    namespace.String,
				ResourceKey:  resourceKey.String,
				DisplayName:  displayName.String,
				CreatedAt:    resourceCreated.String,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return services, nil
}

func insertResources(ctx context.Context, tx *sql.Tx, serviceID string, resources []Resource, now string) ([]Resource, error) {
	result := make([]Resource, 0, len(resources))
	for _, resource := range resources {
		resource.ID = uuid.NewString()
		resource.ServiceID = serviceID
		resource.CreatedAt = now
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_service_resources (id, service_id, resource_type, scope_id, resource_kind, namespace, resource_key, display_name, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, resource.ID, serviceID, resource.ResourceType, resource.ScopeID, resource.ResourceKind, resource.Namespace, resource.ResourceKey, resource.DisplayName, now); err != nil {
			return nil, normalizeStoreError(err)
		}
		result = append(result, resource)
	}
	return result, nil
}

func normalizeStoreError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique") || strings.Contains(message, "duplicate") {
		return fmt.Errorf("%w: 服务名称或资源关联已存在", ErrConflict)
	}
	return err
}
