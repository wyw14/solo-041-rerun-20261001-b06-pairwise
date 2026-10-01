package aeration

import (
	"testing"
)

func TestSingleReadingIsNeverStable(t *testing.T) {
	book, err := NewWindowBook(5)
	if err != nil {
		t.Fatal(err)
	}
	// One identical reading trivially has zero spread; it must still not
	// count as a complete, stable window.
	window, err := book.Store("basin-1", []float64{1.8})
	if err != nil {
		t.Fatal(err)
	}
	if window.Complete || window.Stable {
		t.Fatalf("single reading marked complete=%v stable=%v", window.Complete, window.Stable)
	}
	if window.Count != 1 || window.Limit != 5 {
		t.Fatalf("unexpected window state: %+v", window)
	}
}

func TestSmallBatchesAccumulateUntilComplete(t *testing.T) {
	book, _ := NewWindowBook(5)
	window, _ := book.Store("basin-1", []float64{2.0, 2.1})
	if window.Complete || window.Stable || window.Count != 2 {
		t.Fatalf("two readings should not complete a window: %+v", window)
	}
	// Late arrivals extend the existing window instead of replacing it.
	window, _ = book.Store("basin-1", []float64{2.05, 2.15})
	if window.Complete || window.Stable || window.Count != 4 {
		t.Fatalf("four readings should still be incomplete: %+v", window)
	}
	window, _ = book.Store("basin-1", []float64{2.1})
	if !window.Complete || !window.Stable {
		t.Fatalf("five tightly clustered readings should be complete and stable: %+v", window)
	}
	if window.Average < 2.0 || window.Average > 2.2 {
		t.Fatalf("unexpected average: %f", window.Average)
	}
}

func TestFullWindowWithWideSpreadIsUnstable(t *testing.T) {
	book, _ := NewWindowBook(3)
	window, _ := book.Store("basin-1", []float64{1.5, 2.9, 3.4})
	if !window.Complete {
		t.Fatal("window should be complete")
	}
	if window.Stable {
		t.Fatal("window with wide spread must not be stable")
	}
}

func TestCompletedWindowKeepsMostRecentReadings(t *testing.T) {
	book, _ := NewWindowBook(3)
	book.Store("basin-1", []float64{1.0, 2.0, 3.0})
	// A later batch rolls the window forward.
	window, _ := book.Store("basin-1", []float64{4.0})
	if len(window.Values) != 3 || window.Values[0] != 2.0 || window.Values[2] != 4.0 {
		t.Fatalf("window should retain newest readings, got %v", window.Values)
	}
}

func TestStoreRejectsEmptyInput(t *testing.T) {
	book, _ := NewWindowBook(3)
	if _, err := book.Store("", []float64{1.0}); err == nil {
		t.Error("empty basin id should be rejected")
	}
	if _, err := book.Store("basin-1", nil); err == nil {
		t.Error("empty sample batch should be rejected")
	}
}
