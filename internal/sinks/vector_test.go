package sinks

import (
	"os"
	"testing"

	"pkm-sync/internal/vectorstore"
)

// TestVectorSinkCloseNilProvider verifies that Close() does not panic when the
// embedding provider is nil (metadata-only mode).
func TestVectorSinkCloseNilProvider(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "vector_test_*.db")
	if err != nil {
		t.Fatal(err)
	}

	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	store, err := vectorstore.NewStore(tmpFile.Name(), 0)
	if err != nil {
		t.Fatal(err)
	}

	sink := &VectorSink{
		store:    store,
		provider: nil, // metadata-only mode
	}

	if err := sink.Close(); err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}
}
