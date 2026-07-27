package audit

import (
	"crypto/rand"
	"strings"
	"time"
)

// RecordSystem writes one bounded background-operation event. Callers must pass
// only allowlisted identifiers and metadata; command text and output never
// belong in an audit event.
func RecordSystem(recorder Recorder, options RecordOptions) {
	if recorder == nil {
		return
	}
	now := time.Now().UTC()
	result := options.Result
	if !validResult(result) {
		result = ResultSuccess
	}
	summary := bounded(strings.ToLower(options.Summary), maxSummaryLength)
	if summary == "" {
		if result == ResultFailure {
			summary = "operation_failed"
		} else {
			summary = "completed"
		}
	}
	event := Event{
		RequestID:  newRequestID(rand.Reader, now),
		CreatedAt:  now,
		ActorType:  ActorSystem,
		ActorName:  "system",
		Module:     bounded(strings.ToLower(options.Module), maxIdentifierLen),
		Action:     bounded(strings.ToLower(options.Action), maxIdentifierLen),
		TargetType: bounded(strings.ToLower(options.TargetType), maxIdentifierLen),
		TargetID:   bounded(options.TargetID, maxTargetIDLength),
		TargetName: bounded(options.TargetName, maxTargetNameLen),
		NodeID:     bounded(options.NodeID, maxNodeIDLength),
		Result:     result,
		DurationMS: max(options.Duration.Milliseconds(), 0),
		Summary:    summary,
		Metadata:   boundedMetadata(options.Metadata),
	}
	persist(recorder, &event)
}
