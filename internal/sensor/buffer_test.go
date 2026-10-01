package sensor

import "testing"

func TestBatchDecoderAcceptsFiniteInRangeReadings(t *testing.T) {
	decoder, err := NewBatchDecoder(8)
	if err != nil {
		t.Fatalf("new decoder: %v", err)
	}
	values, err := decoder.Decode([]byte(`[2.0,2.1,2.2]`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(values) != 3 || values[0] != 2.0 || values[2] != 2.2 {
		t.Fatalf("unexpected values %v", values)
	}
}

func TestBatchDecoderRejectsBadQualityReadings(t *testing.T) {
	decoder, _ := NewBatchDecoder(8)
	cases := map[string]string{
		"null reading":      `[2.0,null,2.2]`,
		"empty batch":       `[]`,
		"non-numeric":       `[2.0,"x"]`,
		"negative":          `[-0.5,2.0]`,
		"above probe range": `[2.0,999]`,
		"non-array":         `{"value": 2.0}`,
		"malformed json":    `[2.0,`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if values, err := decoder.Decode([]byte(payload)); err == nil {
				t.Fatalf("expected error for %s, got values %v", name, values)
			}
		})
	}
}

func TestBatchDecoderDoesNotLeakScratchAcrossBatches(t *testing.T) {
	decoder, _ := NewBatchDecoder(8)
	first, err := decoder.Decode([]byte(`[1.0,1.0,1.0]`))
	if err != nil {
		t.Fatalf("first decode: %v", err)
	}
	second, err := decoder.Decode([]byte(`[2.0]`))
	if err != nil {
		t.Fatalf("second decode: %v", err)
	}
	if len(second) != 1 || second[0] != 2.0 {
		t.Fatalf("second batch leaked scratch data: %v (first=%v)", second, first)
	}
	// The returned slice must be independent from later decoder use.
	first[0] = 42
	again, err := decoder.Decode([]byte(`[3.0,3.0]`))
	if err != nil {
		t.Fatalf("third decode: %v", err)
	}
	if again[0] != 3.0 || len(again) != 2 {
		t.Fatalf("decoder state corrupted: %v", again)
	}
}
