package crafting

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/log"
)

type mockInventory struct {
	counts map[string]int
	pure   PureCounts
}

func (m *mockInventory) GetItemCount(sku string) int { return m.counts[sku] }
func (m *mockInventory) GetPureCounts() PureCounts   { return m.pure }

type mockPrice struct {
	priced map[string]bool
}

func (m *mockPrice) HasPricedItem(sku string) bool { return m.priced[sku] }

type mockGC struct {
	combineClassWeaponsCalls [][2]string
	combineDupWeaponsCalls   []string
	combineMetalCalls        []int
	smeltMetalCalls          []int
	err                      error
}

func (m *mockGC) CombineClassWeapons(ctx context.Context, sku1, sku2 string) error {
	m.combineClassWeaponsCalls = append(m.combineClassWeaponsCalls, [2]string{sku1, sku2})
	return m.err
}
func (m *mockGC) CombineDuplicateWeapon(ctx context.Context, sku string) error {
	m.combineDupWeaponsCalls = append(m.combineDupWeaponsCalls, sku)
	return m.err
}
func (m *mockGC) CombineMetal(ctx context.Context, defindex int) error {
	m.combineMetalCalls = append(m.combineMetalCalls, defindex)
	return m.err
}
func (m *mockGC) SmeltMetal(ctx context.Context, defindex int) error {
	m.smeltMetalCalls = append(m.smeltMetalCalls, defindex)
	return m.err
}

type mockConfig struct {
	weaponsEnabled bool
	metalsEnabled  bool
	minScrap       int
	minRec         int
	threshold      int
	classWeapons   map[string][]string
	allWeapons     []string
}

func (m *mockConfig) IsWeaponsCraftingEnabled() bool { return m.weaponsEnabled }
func (m *mockConfig) IsMetalsCraftingEnabled() bool  { return m.metalsEnabled }
func (m *mockConfig) GetMetalThresholds() (minScrap, minRec, threshold int) {
	return m.minScrap, m.minRec, m.threshold
}
func (m *mockConfig) GetCraftWeaponsByClass() map[string][]string { return m.classWeapons }
func (m *mockConfig) GetAllCraftWeapons() []string                { return m.allWeapons }

func TestService_KeepMetalSupply(t *testing.T) {
	tests := []struct {
		name             string
		metalsEnabled    bool
		pure             PureCounts
		minScrap, minRec int
		threshold        int
		expectedSmelt    []int
		expectedCombine  []int
	}{
		{
			name:          "Crafting disabled",
			metalsEnabled: false,
			pure:          PureCounts{Refined: 10, Reclaimed: 10, Scrap: 10},
		},
		{
			name:          "Low pure counts (early exit)",
			metalsEnabled: true,
			pure:          PureCounts{Refined: 0, Reclaimed: 3, Scrap: 3},
		},
		{
			name:          "Combine Scrap",
			metalsEnabled: true,
			pure:          PureCounts{Refined: 1, Reclaimed: 0, Scrap: 6}, // Scrap(6) > max(2+2=4). Combine = (6-4+2)/3 = 1
			minScrap:      2, minRec: 2, threshold: 2,
			expectedCombine: []int{5000}, // 5000 = Scrap -> Rec
		},
		{
			name:          "Combine Reclaimed",
			metalsEnabled: true,
			pure:          PureCounts{Refined: 1, Reclaimed: 8, Scrap: 2}, // Rec(8) > max(4). Combine = (8-4+2)/3 = 2
			minScrap:      2, minRec: 2, threshold: 2,
			expectedCombine: []int{5001, 5001}, // 2x Rec -> Ref
		},
		{
			name:          "Smelt Refined",
			metalsEnabled: true,
			pure:          PureCounts{Refined: 10, Reclaimed: 1, Scrap: 5}, // Rec(1) < min(5). Smelt = (5-1+2)/3 = 2
			minScrap:      5, minRec: 5, threshold: 2,
			expectedSmelt: []int{5002, 5002}, // 2x Ref -> Rec
		},
		{
			name:          "Smelt Reclaimed",
			metalsEnabled: true,
			pure:          PureCounts{Refined: 10, Reclaimed: 10, Scrap: 0}, // Scrap(0) < min(4). Smelt = (4-0+2)/3 = 2
			minScrap:      4, minRec: 4, threshold: 2,
			expectedSmelt: []int{5001, 5001}, // 2x Rec -> Scrap
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &mockInventory{pure: tt.pure}
			cfg := &mockConfig{
				metalsEnabled: tt.metalsEnabled,
				minScrap:      tt.minScrap,
				minRec:        tt.minRec,
				threshold:     tt.threshold,
			}
			gc := &mockGC{}

			svc := NewService(inv, &mockPrice{}, gc, cfg, log.Discard)
			err := svc.KeepMetalSupply(context.Background())

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(gc.combineMetalCalls) != len(tt.expectedCombine) {
				t.Errorf("CombineMetal calls: got %v, want %v", gc.combineMetalCalls, tt.expectedCombine)
			}
			if len(gc.smeltMetalCalls) != len(tt.expectedSmelt) {
				t.Errorf("SmeltMetal calls: got %v, want %v", gc.smeltMetalCalls, tt.expectedSmelt)
			}
		})
	}
}

func TestService_KeepMetalSupply_ContextCancel(t *testing.T) {
	inv := &mockInventory{pure: PureCounts{Refined: 10, Reclaimed: 10, Scrap: 0}}
	cfg := &mockConfig{metalsEnabled: true, minScrap: 10, threshold: 0}
	gc := &mockGC{}
	svc := NewService(inv, &mockPrice{}, gc, cfg, log.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.KeepMetalSupply(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func TestService_CraftDuplicateWeapons(t *testing.T) {
	tests := []struct {
		name           string
		weaponsEnabled bool
		allWeapons     []string
		counts         map[string]int
		priced         map[string]bool
		expectedCalls  []string
	}{
		{
			name:           "Crafting disabled",
			weaponsEnabled: false,
			allWeapons:     []string{"wep1"},
			counts:         map[string]int{"wep1": 5},
		},
		{
			name:           "Craft duplicate unpriced",
			weaponsEnabled: true,
			allWeapons:     []string{"wep1", "wep2"},
			counts:         map[string]int{"wep1": 5, "wep2": 3}, // 5/2 = 2x, 3/2 = 1x
			expectedCalls:  []string{"wep1", "wep1", "wep2"},
		},
		{
			name:           "Skip priced items",
			weaponsEnabled: true,
			allWeapons:     []string{"wep1", "wep2"},
			counts:         map[string]int{"wep1": 4, "wep2": 4},
			priced:         map[string]bool{"wep1": true},
			expectedCalls:  []string{"wep2", "wep2"},
		},
		{
			name:           "Skip insufficient count",
			weaponsEnabled: true,
			allWeapons:     []string{"wep1"},
			counts:         map[string]int{"wep1": 1},
			expectedCalls:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &mockInventory{counts: tt.counts}
			price := &mockPrice{priced: tt.priced}
			cfg := &mockConfig{weaponsEnabled: tt.weaponsEnabled, allWeapons: tt.allWeapons}
			gc := &mockGC{}

			svc := NewService(inv, price, gc, cfg, log.Discard)
			err := svc.CraftDuplicateWeapons(context.Background())

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(gc.combineDupWeaponsCalls, tt.expectedCalls) {
				t.Errorf("got calls %v, want %v", gc.combineDupWeaponsCalls, tt.expectedCalls)
			}
		})
	}
}

func TestService_CraftClassWeapons(t *testing.T) {
	tests := []struct {
		name           string
		weaponsEnabled bool
		classWeapons   map[string][]string
		counts         map[string]int
		priced         map[string]bool
		expectedCalls  [][2]string
	}{
		{
			name:           "Crafting disabled",
			weaponsEnabled: false,
			classWeapons:   map[string][]string{"scout": {"wep1", "wep2"}},
			counts:         map[string]int{"wep1": 1, "wep2": 1},
		},
		{
			name:           "Valid pair in class",
			weaponsEnabled: true,
			classWeapons:   map[string][]string{"scout": {"wep1", "wep2"}},
			counts:         map[string]int{"wep1": 1, "wep2": 1},
			expectedCalls:  [][2]string{{"wep1", "wep2"}},
		},
		{
			name:           "Skip if count != 1",
			weaponsEnabled: true,
			classWeapons:   map[string][]string{"scout": {"wep1", "wep2"}},
			counts:         map[string]int{"wep1": 2, "wep2": 1},
			expectedCalls:  nil,
		},
		{
			name:           "Skip priced items",
			weaponsEnabled: true,
			classWeapons:   map[string][]string{"scout": {"wep1", "wep2"}},
			counts:         map[string]int{"wep1": 1, "wep2": 1},
			priced:         map[string]bool{"wep1": true},
			expectedCalls:  nil,
		},
		{
			name:           "Stops after first successful craft per class",
			weaponsEnabled: true,
			classWeapons:   map[string][]string{"scout": {"wep1", "wep2", "wep3"}},
			counts:         map[string]int{"wep1": 1, "wep2": 1, "wep3": 1},
			expectedCalls:  [][2]string{{"wep1", "wep2"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &mockInventory{counts: tt.counts}
			price := &mockPrice{priced: tt.priced}
			cfg := &mockConfig{weaponsEnabled: tt.weaponsEnabled, classWeapons: tt.classWeapons}
			gc := &mockGC{}

			svc := NewService(inv, price, gc, cfg, log.Discard)
			err := svc.CraftClassWeapons(context.Background())

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(gc.combineClassWeaponsCalls, tt.expectedCalls) {
				t.Errorf("got calls %v, want %v", gc.combineClassWeaponsCalls, tt.expectedCalls)
			}
		})
	}
}
