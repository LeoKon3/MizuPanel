package audit

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ActorAdmin           = "admin"
	ActorUnauthenticated = "unauthenticated"
	ActorLocalAdmin      = "local_admin"
	ActorSystem          = "system"

	ResultSuccess  = "success"
	ResultFailure  = "failure"
	ResultAccepted = "accepted"

	DefaultPageLimit = 50
	MaxPageLimit     = 100
	MaxSearchLength  = 128
	MaxMetadataItems = 16
)

const (
	maxRequestIDLength = 64
	maxActorNameLength = 128
	maxSourceIPLength  = 64
	maxIdentifierLen   = 64
	maxTargetIDLength  = 1024
	maxTargetNameLen   = 256
	maxNodeIDLength    = 191
	maxSummaryLength   = 64
	maxMetadataKeyLen  = 64
	maxMetadataValue   = 256
)

var (
	ErrInvalid = errors.New("invalid audit value")
	identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
)

type Event struct {
	ID         int64             `json:"id"`
	RequestID  string            `json:"request_id"`
	CreatedAt  time.Time         `json:"created_at"`
	ActorType  string            `json:"actor_type"`
	ActorName  string            `json:"actor_name"`
	SourceIP   string            `json:"source_ip"`
	Module     string            `json:"module"`
	Action     string            `json:"action"`
	TargetType string            `json:"target_type"`
	TargetID   string            `json:"target_id"`
	TargetName string            `json:"target_name"`
	NodeID     string            `json:"node_id"`
	Result     string            `json:"result"`
	DurationMS int64             `json:"duration_ms"`
	Summary    string            `json:"summary"`
	Metadata   map[string]string `json:"metadata"`
}

type Filter struct {
	BeforeID  int64
	Limit     int
	From      *time.Time
	To        *time.Time
	ActorType string
	ActorName string
	Module    string
	Action    string
	NodeID    string
	Result    string
	Query     string
}

type Page struct {
	Events       []Event
	NextBeforeID *int64
}

func validActorType(value string) bool {
	switch value {
	case ActorAdmin, ActorUnauthenticated, ActorLocalAdmin, ActorSystem:
		return true
	default:
		return false
	}
}

func validResult(value string) bool {
	switch value {
	case ResultSuccess, ResultFailure, ResultAccepted:
		return true
	default:
		return false
	}
}

func validateIdentifier(field, value string, required bool) error {
	if value == "" && !required {
		return nil
	}
	if value == "" || len(value) > maxIdentifierLen || !identifier.MatchString(value) {
		return fmt.Errorf("%w: %s", ErrInvalid, field)
	}
	return nil
}

func validateEvent(event *Event) error {
	if event == nil {
		return fmt.Errorf("%w: event", ErrInvalid)
	}
	for field, value := range map[string]string{
		"request_id":  event.RequestID,
		"actor_name":  event.ActorName,
		"source_ip":   event.SourceIP,
		"module":      event.Module,
		"action":      event.Action,
		"target_type": event.TargetType,
		"target_id":   event.TargetID,
		"target_name": event.TargetName,
		"node_id":     event.NodeID,
		"result":      event.Result,
		"summary":     event.Summary,
	} {
		if !utf8.ValidString(value) {
			return fmt.Errorf("%w: %s", ErrInvalid, field)
		}
	}
	if event.RequestID == "" || len(event.RequestID) > maxRequestIDLength {
		return fmt.Errorf("%w: request_id", ErrInvalid)
	}
	if !validActorType(event.ActorType) {
		return fmt.Errorf("%w: actor_type", ErrInvalid)
	}
	if len(event.ActorName) > maxActorNameLength || len(event.SourceIP) > maxSourceIPLength {
		return fmt.Errorf("%w: actor/source", ErrInvalid)
	}
	if err := validateIdentifier("module", event.Module, true); err != nil {
		return err
	}
	if err := validateIdentifier("action", event.Action, true); err != nil {
		return err
	}
	if err := validateIdentifier("target_type", event.TargetType, false); err != nil {
		return err
	}
	if len(event.TargetID) > maxTargetIDLength || len(event.TargetName) > maxTargetNameLen || len(event.NodeID) > maxNodeIDLength {
		return fmt.Errorf("%w: target", ErrInvalid)
	}
	if !validResult(event.Result) || event.DurationMS < 0 || len(event.Summary) > maxSummaryLength {
		return fmt.Errorf("%w: result", ErrInvalid)
	}
	if len(event.Metadata) > MaxMetadataItems {
		return fmt.Errorf("%w: metadata", ErrInvalid)
	}
	for key, value := range event.Metadata {
		if !utf8.ValidString(key) || !utf8.ValidString(value) {
			return fmt.Errorf("%w: metadata", ErrInvalid)
		}
		if err := validateIdentifier("metadata key", key, true); err != nil {
			return err
		}
		if len(key) > maxMetadataKeyLen || len(value) > maxMetadataValue {
			return fmt.Errorf("%w: metadata", ErrInvalid)
		}
	}
	return nil
}

func validateFilter(filter Filter) error {
	if filter.BeforeID < 0 || filter.Limit < 0 || filter.Limit > MaxPageLimit {
		return fmt.Errorf("%w: pagination", ErrInvalid)
	}
	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return fmt.Errorf("%w: time range", ErrInvalid)
	}
	if filter.ActorType != "" && !validActorType(filter.ActorType) {
		return fmt.Errorf("%w: actor_type", ErrInvalid)
	}
	if len(filter.ActorName) > maxActorNameLength || len(filter.NodeID) > maxNodeIDLength || len(filter.Query) > MaxSearchLength {
		return fmt.Errorf("%w: filter", ErrInvalid)
	}
	if !utf8.ValidString(filter.ActorName) || !utf8.ValidString(filter.NodeID) || !utf8.ValidString(filter.Query) {
		return fmt.Errorf("%w: filter", ErrInvalid)
	}
	if err := validateIdentifier("module", filter.Module, false); err != nil {
		return err
	}
	if err := validateIdentifier("action", filter.Action, false); err != nil {
		return err
	}
	if filter.Result != "" && !validResult(filter.Result) {
		return fmt.Errorf("%w: result", ErrInvalid)
	}
	return nil
}

func bounded(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return strings.ToValidUTF8(value, "")
	}
	return strings.ToValidUTF8(value[:max], "")
}
