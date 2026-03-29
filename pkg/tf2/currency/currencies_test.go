// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package currency_test

import (
	"testing"

	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
)

func TestToScrap(t *testing.T) {
	tests := []struct {
		refined float64
		want    float64
	}{
		{0.11, 1},
		{0.33, 3},
		{1.00, 9},
		{1.11, 10},
		{2.33, 21},
		{50.00, 450},
	}

	for _, tt := range tests {
		got := currency.ToScrap(tt.refined)
		if got != tt.want {
			t.Errorf("ToScrap(%v) = %v, want %v", tt.refined, got, tt.want)
		}
	}
}

func TestToRefined(t *testing.T) {
	tests := []struct {
		scrap float64
		want  float64
	}{
		{1, 0.11},
		{3, 0.33},
		{9, 1.00},
		{10, 1.11},
		{21, 2.33},
		{450, 50.00},
	}

	for _, tt := range tests {
		got := currency.ToRefined(tt.scrap)
		if got != tt.want {
			t.Errorf("ToRefined(%v) = %v, want %v", tt.scrap, got, tt.want)
		}
	}
}

func TestCurrencies_String(t *testing.T) {
	tests := []struct {
		name string
		c    *currency.Currency
		want string
	}{
		{
			name: "Single key and metal",
			c:    currency.New(1, 20.11),
			want: "1 key, 20.11 ref",
		},
		{
			name: "Multiple keys",
			c:    currency.New(5, 0),
			want: "5 keys",
		},
		{
			name: "Only metal",
			c:    currency.New(0, 15.33),
			want: "15.33 ref",
		},
		{
			name: "Zero value",
			c:    currency.New(0, 0),
			want: "0 keys, 0 ref",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCurrencies_ToValue(t *testing.T) {
	// 1 key = 50 ref = 450 scrap
	const conv = 50.0

	tests := []struct {
		name    string
		keys    float64
		metal   float64
		want    float64 // in scrap
		wantErr bool
	}{
		{"1 key only", 1, 0, 450, false},
		{"1 key 1.11 ref", 1, 1.11, 460, false}, // 450 + 10
		{"0.5 key", 0.5, 0, 225, false},
		{"Error on missing conversion", 1, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := currency.New(tt.keys, tt.metal)
			conversion := conv
			if tt.wantErr {
				conversion = 0
			}

			got, err := c.ToValue(conversion)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ToValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScrapToCurrencies(t *testing.T) {
	// 1 key = 50 ref = 450 scrap
	const conv = 50.0

	tests := []struct {
		name  string
		scrap float64
		wantK float64
		wantM float64
	}{
		{"Exactly 1 key", 450, 1, 0},
		{"1 key and 1 scrap", 451, 1, 0.11},
		{"Less than 1 key", 100, 0, 11.11},
		{"Multiple keys", 1000, 2, 11.11}, // (1000 - 900) / 9 = 11.11
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := currency.ScrapToCurrencies(tt.scrap, conv)
			if got.Keys != tt.wantK || got.Metal != tt.wantM {
				t.Errorf("ScrapToCurrencies() = %v keys, %v metal; want %v keys, %v metal",
					got.Keys, got.Metal, tt.wantK, tt.wantM)
			}
		})
	}
}

func TestAddRefined(t *testing.T) {
	res := currency.AddRefined(1.11, 2.22, 0.11)
	// 10 + 20 + 1 = 31 scrap = 3.44 ref
	want := 3.44
	if res != want {
		t.Errorf("AddRefined() = %v, want %v", res, want)
	}
}
