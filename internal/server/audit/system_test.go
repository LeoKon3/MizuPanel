package audit

import (
	"context"
	"testing"
	"time"
)

type systemRecordingRecorder struct {
	events []Event
}

func (r *systemRecordingRecorder) Create(_ context.Context, event *Event) error {
	r.events = append(r.events, *event)
	return nil
}

func TestRecordSystemCreatesBoundedSystemEvent(t *testing.T) {
	recorder := &systemRecordingRecorder{}
	RecordSystem(recorder, RecordOptions{
		Module:     "TASK",
		Action:     "RUN_COMPLETE",
		TargetType: "TASK_RUN",
		TargetID:   "42",
		TargetName: "Nightly cleanup",
		Result:     ResultFailure,
		Summary:    "TASK_FAILED",
		Duration:   1500 * time.Millisecond,
		Metadata:   map[string]string{"script_id": "7", "node_count": "3"},
	})

	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.RequestID == "" || event.ActorType != ActorSystem || event.ActorName != "system" {
		t.Fatalf("system identity = %+v", event)
	}
	if event.Module != "task" || event.Action != "run_complete" || event.TargetType != "task_run" {
		t.Fatalf("operation = %+v", event)
	}
	if event.Result != ResultFailure || event.Summary != "task_failed" || event.DurationMS != 1500 {
		t.Fatalf("result = %+v", event)
	}
	if event.Metadata["script_id"] != "7" || event.Metadata["node_count"] != "3" {
		t.Fatalf("metadata = %#v", event.Metadata)
	}
}

func TestRecordSystemDefaultsResultAndIgnoresNilRecorder(t *testing.T) {
	RecordSystem(nil, RecordOptions{Module: "task", Action: "run_complete"})

	recorder := &systemRecordingRecorder{}
	RecordSystem(recorder, RecordOptions{Module: "task", Action: "run_complete"})
	if len(recorder.events) != 1 || recorder.events[0].Result != ResultSuccess || recorder.events[0].Summary != "completed" {
		t.Fatalf("event = %+v", recorder.events)
	}
}
