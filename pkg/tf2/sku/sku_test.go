// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sku_test

import (
	"reflect"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
)

func TestFromString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *sku.Item
		wantErr bool
	}{
		{
			name:  "Basic Unique Item",
			input: "363;6",
			want: &sku.Item{
				Defindex: 363, Quality: 6, Craftable: true, Tradable: true,
			},
		},
		{
			name:  "Unusual Professional Killstreak Australium",
			input: "655;5;u14;australium;kt-3;strange",
			want: &sku.Item{
				Defindex:   655,
				Quality:    5,
				Effect:     14,
				Australium: true,
				Killstreak: 3,
				Quality2:   11,
				Craftable:  true,
				Tradable:   true,
			},
		},
		{
			name:  "Uncraftable and Untradable",
			input: "300;6;uncraftable;untradeable",
			want: &sku.Item{
				Defindex: 300, Quality: 6, Craftable: false, Tradable: false,
			},
		},
		{
			name:  "Skins and Wear",
			input: "15000;15;pk1;w3",
			want: &sku.Item{
				Defindex: 15000, Quality: 15, Paintkit: 1, Wear: 3, Craftable: true, Tradable: true,
			},
		},
		{
			name:  "Craft Number and Series",
			input: "444;6;n100;c50",
			want: &sku.Item{
				Defindex: 444, Quality: 6, Craftnumber: 100, Crateseries: 50, Craftable: true, Tradable: true,
			},
		},
		{
			name:    "Invalid SKU - Too Short",
			input:   "363",
			wantErr: true,
		},
		{
			name:    "Invalid Defindex",
			input:   "abc;6",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sku.FromString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FromString() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFromObject(t *testing.T) {
	tests := []struct {
		name string
		item *sku.Item
		want string
	}{
		{
			name: "Basic",
			item: &sku.Item{Defindex: 363, Quality: 6, Craftable: true, Tradable: true},
			want: "363;6",
		},
		{
			name: "Killstreak with dash",
			item: &sku.Item{Defindex: 200, Quality: 6, Killstreak: 2, Craftable: true, Tradable: true},
			want: "200;6;kt-2",
		},
		{
			name: "Full attributes",
			item: &sku.Item{
				Defindex: 100, Quality: 5, Effect: 15, Australium: true, Craftable: false, Tradable: false,
				Wear: 1, Paintkit: 200, Quality2: 11, Killstreak: 3, Target: 400, Festivized: true,
				Craftnumber: 1, Crateseries: 2, Output: 3, OutputQuality: 4, Paint: 5,
			},
			want: "100;5;u15;australium;uncraftable;untradable;w1;pk200;strange;kt-3;td-400;festive;n1;c2;od-3;oq-4;p5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sku.FromObject(tt.item)

			if got != tt.want {
				t.Errorf("FromObject() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	skus := []string{
		"363;6",
		"655;5;u14;australium;strange;kt-3",
		"15000;15;w3;pk1;strange;kt-3;festive",
		"300;6;uncraftable;untradable",
	}

	for _, s := range skus {
		t.Run(s, func(t *testing.T) {
			item, err := sku.FromString(s)
			if err != nil {
				t.Fatalf("Failed to parse: %v", err)
			}

			out := sku.FromObject(item)

			if s != out {
				t.Errorf("Round-trip failed!\nInput:  %s\nOutput: %s", s, out)
			}
		})
	}
}
