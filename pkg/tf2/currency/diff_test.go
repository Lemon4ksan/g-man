package currency

import (
	"testing"
)

func TestValueDiff(t *testing.T) {
	keyPrice := 540

	tests := []struct {
		name           string
		ourScrap       int
		theirScrap     int
		keyPrice       int
		wantProfitable bool
		wantMissingRef float64
		wantMissingStr string
	}{
		{
			name:           "Profitable trade",
			ourScrap:       100,
			theirScrap:     120,
			keyPrice:       keyPrice,
			wantProfitable: true,
			wantMissingRef: 0,
			wantMissingStr: "0 ref",
		},
		{
			name:           "Equal trade",
			ourScrap:       100,
			theirScrap:     100,
			keyPrice:       keyPrice,
			wantProfitable: true,
			wantMissingRef: 0,
			wantMissingStr: "0 ref",
		},
		{
			name:           "Unprofitable - small metal diff",
			ourScrap:       15, // 1 ref, 2 rec
			theirScrap:     10, // 1 ref, 0 rec, 1 scrap
			keyPrice:       keyPrice,
			wantProfitable: false,
			wantMissingRef: 5.0 / 9.0,
			wantMissingStr: "0.56 ref",
		},
		{
			name:           "Unprofitable - exactly 1 key missing",
			ourScrap:       keyPrice + 10,
			theirScrap:     10,
			keyPrice:       keyPrice,
			wantProfitable: false,
			wantMissingStr: "1 keys",
		},
		{
			name:           "Unprofitable - keys and metal missing",
			ourScrap:       (keyPrice * 2) + 18, // 2 keys + 2 ref
			theirScrap:     0,
			keyPrice:       keyPrice,
			wantProfitable: false,
			wantMissingStr: "2 keys, 2.00 ref",
		},
		{
			name:           "Unprofitable - nearly 1 key missing",
			ourScrap:       keyPrice - 1,
			theirScrap:     0,
			keyPrice:       keyPrice,
			wantProfitable: false,
			wantMissingStr: "59.89 ref",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := NewValueDiff(tt.ourScrap, tt.theirScrap, tt.keyPrice)

			if diff.IsProfitable() != tt.wantProfitable {
				t.Errorf("IsProfitable() = %v, want %v", diff.IsProfitable(), tt.wantProfitable)
			}

			gotMissingRef := diff.MissingRefined()
			if tt.wantMissingRef > 0 && (gotMissingRef < tt.wantMissingRef-0.0001 || gotMissingRef > tt.wantMissingRef+0.0001) {
				t.Errorf("MissingRefined() = %v, want %v", gotMissingRef, tt.wantMissingRef)
			}

			if diff.MissingString() != tt.wantMissingStr {
				t.Errorf("MissingString() = %v, want %v", diff.MissingString(), tt.wantMissingStr)
			}
		})
	}
}
