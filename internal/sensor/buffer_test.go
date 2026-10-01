package sensor

import (
	"strings"
	"testing"
)

func TestDecodeAcceptsValidBatch(t *testing.T) {
	decoder, err := NewBatchDecoder(8)
	if err != nil {
		t.Fatal(err)
	}
	values, err := decoder.Decode([]byte(`[2.0,2.1,2.2]`))
	if err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
	if len(values) != 3 || values[0] != 2.0 || values[2] != 2.2 {
		t.Fatalf("unexpected values: %v", values)
	}
	// Returned slice must be safe for the caller to own.
	values[0] = 99
	again, err := decoder.Decode([]byte(`[2.0]`))
	if err != nil || again[0] != 2.0 {
		t.Fatalf("decoder retained caller mutations: %v %v", again, err)
	}
}

func TestDecodeRejectsEmptyOrMalformed(t *testing.T) {
	decoder, _ := NewBatchDecoder(8)
	for _, payload := range []string{``, `[]`, `not-json`, `{"a":1}`} {
		if _, err := decoder.Decode([]byte(payload)); err == nil {
			t.Errorf("payload %q should be rejected", payload)
		}
	}
}

func TestDecodeRejectsMissingReading(t *testing.T) {
	decoder, _ := NewBatchDecoder(8)
	// A null entry must not be coerced to 0.0 (critically low oxygen).
	_, err := decoder.Decode([]byte(`[2.1,null,2.2]`))
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("null reading should be rejected as missing, got %v", err)
	}
}

func TestDecodeRejectsNonFinite(t *testing.T) {
	decoder, _ := NewBatchDecoder(8)
	for _, payload := range []string{`[NaN]`, `[+Inf]`, `[-Inf]`} {
		if _, err := decoder.Decode([]byte(payload)); err == nil {
			t.Errorf("payload %q should be rejected", payload)
		}
	}
}

func TestDecodeRejectsOutOfRange(t *testing.T) {
	decoder, _ := NewBatchDecoder(8)
	for _, payload := range []string{`[-0.1]`, `[20.1]`, `[2.0,99.0]`} {
		if _, err := decoder.Decode([]byte(payload)); err == nil {
			t.Errorf("payload %q should be rejected as out of range", payload)
		}
	}
}
