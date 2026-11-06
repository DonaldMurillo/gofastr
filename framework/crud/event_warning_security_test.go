package crud

import (
	"log"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

func TestDroppedEventWarningIsGeneric(t *testing.T) {
	unconfirmedWarnOnce = sync.Once{}
	t.Cleanup(func() { unconfirmedWarnOnce = sync.Once{} })
	var output syncBuffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	first := NewCrudHandler(&entity.Entity{Config: entity.EntityConfig{Name: "Widget", Table: "widgets"}}, nil)
	second := NewCrudHandler(&entity.Entity{Config: entity.EntityConfig{Name: "Order", Table: "orders"}}, nil)
	first.emitAfterAmbientTx(t.Context(), nil, event.EntityCreated, nil)
	second.emitAfterAmbientTx(t.Context(), nil, event.EntityUpdated, nil)
	diagnostic := output.String()
	if strings.Count(diagnostic, "crud:") != 1 || !strings.Contains(diagnostic, "all entity types and tables") ||
		!strings.Contains(diagnostic, "db.WithTxQueue") {
		t.Fatalf("missing process-wide warning: %q", diagnostic)
	}
	for _, misleading := range []string{"widgets", "orders", event.EntityCreated, event.EntityUpdated} {
		if strings.Contains(diagnostic, misleading) {
			t.Errorf("one-time warning implies a partial blast radius: %q", diagnostic)
		}
	}
}
