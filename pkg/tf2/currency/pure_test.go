package currency

import (
	"reflect"
	"testing"
)

func TestPureStock_Calculations(t *testing.T) {
	tests := []struct {
		name      string
		stock     PureStock
		wantScrap int
		wantRef   float64
	}{
		{
			name:      "Empty stock",
			stock:     PureStock{},
			wantScrap: 0,
			wantRef:   0,
		},
		{
			name:      "Full metal set",
			stock:     PureStock{Refined: 1, Reclaimed: 1, Scrap: 1},
			wantScrap: 13, // 9 + 3 + 1
			wantRef:   13.0 / 9.0,
		},
		{
			name:      "Only reclaimed",
			stock:     PureStock{Reclaimed: 3},
			wantScrap: 9,
			wantRef:   1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stock.TotalScrap(); got != tt.wantScrap {
				t.Errorf("TotalScrap() = %v, want %v", got, tt.wantScrap)
			}
			if got := tt.stock.TotalRefined(); got != tt.wantRef {
				t.Errorf("TotalRefined() = %v, want %v", got, tt.wantRef)
			}
		})
	}
}

func TestPureStock_FormatStock(t *testing.T) {
	tests := []struct {
		name  string
		stock PureStock
		want  []string
	}{
		{
			name:  "No keys, no metal",
			stock: PureStock{},
			want:  nil,
		},
		{
			name:  "One key",
			stock: PureStock{Keys: 1},
			want:  []string{"1 key"},
		},
		{
			name:  "Multiple keys",
			stock: PureStock{Keys: 5},
			want:  []string{"5 keys"},
		},
		{
			name:  "Only metal",
			stock: PureStock{Refined: 2, Reclaimed: 1}, // 18 + 3 = 21 scrap = 2.333... ref
			want:  []string{"2.33 ref"},
		},
		{
			name:  "Keys and metal",
			stock: PureStock{Keys: 10, Refined: 1, Scrap: 1}, // 9 + 1 = 10 scrap = 1.111... ref
			want:  []string{"10 keys", "1.11 ref"},
		},
		{
			name:  "Rounding check (truncation)",
			stock: PureStock{Scrap: 2}, // 2/9 = 0.2222...
			want:  []string{"0.22 ref"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stock.FormatStock()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FormatStock() = %v, want %v", got, tt.want)
			}
		})
	}
}
