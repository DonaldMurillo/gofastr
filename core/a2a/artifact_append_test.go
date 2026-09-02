package a2a

import (
	"context"
	"testing"
	"time"
)

// An append event carries only the new parts: spec §7.2 tells a receiver
// to append the event's parts to the artifact it already holds, so an
// event carrying the merged artifact with append=true would double every
// part already delivered. The stored task holds the merged result.
func TestArtifactAppendEventCarriesDelta(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Artifact(Artifact{ArtifactID: "a1", Parts: []Part{TextPart("one")}}, false); err != nil {
			return err
		}
		if err := tc.Artifact(Artifact{ArtifactID: "a1", Parts: []Part{TextPart("two")}}, true); err != nil {
			return err
		}
		return tc.Complete()
	})
	r := h.openStream("alice", MethodSendStreamingMessage, streamSendParams(""))
	_, sr := r.nextResult(2 * time.Second) // task snapshot
	taskID := sr.Task.ID
	_, first := r.nextResult(2 * time.Second)
	if first.ArtifactUpdate == nil || first.ArtifactUpdate.Append || len(first.ArtifactUpdate.Artifact.Parts) != 1 {
		t.Fatalf("first artifact event = %+v, want one part, append=false", first)
	}
	_, second := r.nextResult(2 * time.Second)
	if second.ArtifactUpdate == nil || !second.ArtifactUpdate.Append {
		t.Fatalf("second artifact event = %+v, want append=true", second)
	}
	if parts := second.ArtifactUpdate.Artifact.Parts; len(parts) != 1 || *parts[0].Text != "two" {
		t.Fatalf("append event must carry only the new part, got %+v", parts)
	}
	got := h.waitTask("alice", taskID, TaskStateCompleted, 2*time.Second)
	if len(got.Artifacts) != 1 || len(got.Artifacts[0].Parts) != 2 {
		t.Fatalf("stored artifact = %+v, want the merged two parts", got.Artifacts)
	}
}
