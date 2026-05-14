// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package schema

import (
	"strings"

	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
)

// Item Defindex constants for common items
const (
	DefKey        = 5021
	DefRefined    = 5002
	DefReclaimed  = 5001
	DefScrap      = 5000
	DefPartsProxy = 10000
	DefSpellProxy = 11000
)

// Quality constants for TF2 items
const (
	QualityNormal     = 0
	QualityGenuine    = 1
	QualityVintage    = 3
	QualityUnusual    = 5
	QualityUnique     = 6
	QualityCommunity  = 7
	QualityValve      = 8
	QualitySelfMade   = 9
	QualityCustomized = 10
	QualityStrange    = 11
	QualityCompleted  = 12
	QualityHaunted    = 13
	QualityCollectors = 14
	QualityDecorated  = 15
)

// Attribute ID constants for TF2 items
const (
	AttrUnusualEffect = 134
	AttrStrangeScore  = 214
	AttrPaintColor    = 142
	AttrPaintColor2   = 1031
	AttrKillstreak    = 2025
	AttrAustralium    = 2027
	AttrFestivized    = 2053
	AttrWear          = 725
	AttrPaintkit      = 834
	AttrCrateSeries   = 187
)

// Wear levels for Decorated weapons
const (
	WearFactoryNew    = 1
	WearMinimalWear   = 2
	WearFieldTested   = 3
	WearWellWorn      = 4
	WearBattleScarred = 5
)

// StandardPaints maps hex color values to their display names.
// These are the standard paints available in Team Fortress 2.
var StandardPaints = map[uint32]string{
	0x7D4071: "A Deep Commitment to Purple",
	0x141414: "A Distinctive Lack of Hue",
	0xBCDDB3: "A Mann's Mint",
	0x2D2D24: "After Eight",
	0x7E7E7E: "Aged Moustache Grey",
	0xE6E6E6: "An Extraordinary Abundance of Tinge",
	0xE7B53B: "Australium Gold",
	0xD8BED8: "Color No. 216-190-216",
	0xE9967A: "Dark Salmon Injustice",
	0x808000: "Drably Olive",
	0x729E42: "Indubitably Green",
	0xCF7336: "Mann Co. Orange",
	0xA57545: "Muskelmannbraun",
	0x51384A: "Noble Hatter's Violet",
	0xC5AF91: "Peculiarly Drab Tincture",
	0xFF69B4: "Pink as Hell",
	0x694D3A: "Radigan Conagher Brown",
	0x32CD32: "The Bitter Taste of Defeat and Lime",
	0xF0E68C: "The Color of a Gentlemann's Business Pants",
	0x7C6C57: "Ye Olde Rustic Colour",
	0x424F3B: "Zepheniah's Greed",
	0x2F4F4F: "A Color Similar to Slate",

	// Team paints (Primary/Secondary variations)
	0x654740: "An Air of Debonair",
	0x28394D: "An Air of Debonair",
	0x3B1F23: "Balaclavas Are Forever",
	0x18233D: "Balaclavas Are Forever",
	0xC36C2D: "Cream Spirit",
	0xB88035: "Cream Spirit",
	0x483838: "Operator's Overalls",
	0x384248: "Operator's Overalls",
	0xB8383B: "Team Spirit",
	0x5885A2: "Team Spirit",
	0x803020: "The Value of Teamwork",
	0x256D8D: "The Value of Teamwork",
	0xA89A8C: "Waterlogged Lab Coat",
	0x839FA3: "Waterlogged Lab Coat",
}

// GetPaintName returns the name of the paint associated with the given hex color.
// If the color is not found, it returns a formatted hex string.
func GetPaintName(color uint32) string {
	if name, ok := StandardPaints[color]; ok {
		return name
	}

	return ""
}

var wears = map[string]int{
	"(factory new)":    WearFactoryNew,
	"(minimal wear)":   WearMinimalWear,
	"(field-tested)":   WearFieldTested,
	"(well-worn)":      WearWellWorn,
	"(battle scarred)": WearBattleScarred,
}

// Quality2 constants (elevated qualities)
const (
	Quality2None    = 0
	Quality2Strange = QualityStrange
)

var munitionCrate = map[int]int{
	82: 5734, 83: 5735, 84: 5742, 85: 5752,
	90: 5781, 91: 5802, 92: 5803, 103: 5859,
}

var pistolSkins = map[int]int{
	0: 15013, 18: 15018, 35: 15035, 41: 15041,
	46: 15046, 56: 15056, 61: 15061, 63: 15060,
	69: 15100, 70: 15101, 74: 15102, 78: 15126,
	81: 15148,
}

var rocketLauncherSkins = map[int]int{
	1: 15014, 6: 15006, 28: 15028, 43: 15043,
	52: 15052, 57: 15057, 60: 15081, 69: 15104,
	70: 15105, 76: 15129, 79: 15130, 80: 15150,
}

var medicgunSkins = map[int]int{
	2: 15010, 5: 15008, 25: 15025, 39: 15039,
	50: 15050, 65: 15078, 72: 15097, 76: 15120,
	78: 15121, 79: 15122, 81: 15145, 83: 15146,
}

var revolverSkins = map[int]int{
	3: 15011, 27: 15027, 42: 15042, 51: 15051,
	63: 15064, 64: 15062, 65: 15063, 72: 15103,
	76: 15127, 77: 15128, 81: 15149,
}

var stickybombSkins = map[int]int{
	4: 15012, 8: 15009, 24: 15024, 38: 15038,
	45: 15045, 48: 15048, 60: 15082, 62: 15083,
	63: 15084, 68: 15113, 76: 15137, 78: 15138,
	81: 15155,
}

var sniperRifleSkins = map[int]int{
	7: 15007, 14: 15000, 19: 15019, 23: 15023,
	33: 15033, 59: 15059, 62: 15070, 64: 15071,
	65: 15072, 76: 15135, 66: 15111, 67: 15112,
	78: 15136, 82: 15154,
}

var flameThrowerSkins = map[int]int{
	9: 15005, 17: 15017, 30: 15030, 34: 15034,
	49: 15049, 54: 15054, 60: 15066, 61: 15068,
	62: 15067, 66: 15089, 67: 15090, 76: 15115,
	80: 15141,
}

var minigunSkins = map[int]int{
	10: 15004, 20: 15020, 26: 15026, 31: 15031,
	40: 15040, 55: 15055, 61: 15088, 62: 15087,
	63: 15086, 70: 15098, 73: 15099, 76: 15123,
	77: 15125, 78: 15124, 84: 15147,
}

var scattergunSkins = map[int]int{
	11: 15002, 15: 15015, 21: 15021, 29: 15029,
	36: 15036, 53: 15053, 61: 15069, 63: 15065,
	69: 15106, 72: 15107, 74: 15108, 76: 15131,
	83: 15157, 85: 15151,
}

var shotgunSkins = map[int]int{
	12: 15003, 16: 15016, 44: 15044, 47: 15047,
	60: 15085, 72: 15109, 76: 15132, 78: 15133,
	86: 15152,
}

var smgSkins = map[int]int{
	13: 15001, 22: 15022, 32: 15032, 37: 15037,
	58: 15058, 65: 15076, 69: 15110, 79: 15134,
	81: 15153,
}

var wrenchSkins = map[int]int{
	60: 15074, 61: 15073, 64: 15075, 75: 15114,
	77: 15140, 78: 15139, 82: 15156,
}

var grenadeLauncherSkins = map[int]int{
	60: 15077, 63: 15079, 67: 15091, 68: 15092,
	76: 15116, 77: 15117, 80: 15142, 84: 15158,
}

var knifeSkins = map[int]int{
	64: 15080, 69: 15094, 70: 15095, 71: 15096,
	77: 15119, 78: 15118, 81: 15143, 82: 15144,
}

var exclusiveGenuine = map[int]int{
	810: 831, 811: 832, 812: 833, 813: 834,
	814: 835, 815: 836, 816: 837, 817: 838,
	30720: 30740, 30721: 30741, 30724: 30739,
}

var exclusiveGenuineReversed = map[int]int{
	831: 810, 832: 811, 833: 812, 834: 813,
	835: 814, 836: 815, 837: 816, 838: 817,
	30740: 30720, 30741: 30721, 30739: 30724,
}

var strangifierChemistrySetSeries = map[int]int{
	647: 1, 828: 1, 776: 1, 451: 1, 103: 1,
	446: 1, 541: 1, 733: 1, 387: 1, 486: 1,
	386: 1, 757: 1, 393: 1, 30132: 2, 707: 2,
	30073: 2, 878: 2, 440: 2, 645: 2, 343: 2,
	643: 2, 336: 2, 30377: 3, 30371: 3, 30353: 3,
	30344: 3, 30348: 3, 30361: 3, 30372: 3, 30367: 3,
	30357: 3, 30375: 3, 30350: 3, 30341: 3, 30369: 3,
	30349: 3, 30379: 3, 30343: 3, 30338: 3, 30356: 3,
	30342: 3, 30378: 3, 30359: 3, 30363: 3, 30339: 3,
	30362: 3, 30345: 3, 30352: 3, 30360: 3, 30354: 3,
	30374: 3, 30366: 3, 30347: 3, 30365: 3, 30355: 3,
	30358: 3, 30340: 3, 30351: 3, 30376: 3, 30373: 3,
	30346: 3, 30336: 3, 30337: 3, 30368: 3, 30364: 3,
}

// RetiredKeyInfo represents a retired key.
type RetiredKeyInfo struct {
	Defindex int
	Name     string
}

var retiredKeys = map[int]RetiredKeyInfo{
	5049: {5049, "Festive Winter Crate Key"},
	5067: {5067, "Refreshing Summer Cooler Key"},
	5072: {5072, "Naughty Winter Crate Key"},
	5073: {5073, "Nice Winter Crate Key"},
	5079: {5079, "Scorched Key"},
	5081: {5081, "Fall Key"},
	5628: {5628, "Eerie Key"},
	5631: {5631, "Naughty Winter Crate Key 2012"},
	5632: {5632, "Nice Winter Crate Key 2012"},
	5713: {5713, "Spooky Key"},
	5716: {5716, "Naughty Winter Crate Key 2013"},
	5717: {5717, "Nice Winter Crate Key 2013"},
	5762: {5762, "Limited Late Summer Crate Key"},
	5791: {5791, "Naughty Winter Crate Key 2014"},
	5792: {5792, "Nice Winter Crate Key 2014"},
}

// SpellDefinitions maps spell names to their SKU definitions.
var SpellDefinitions = map[string]sku.Spell{
	// --- Paint Effects (Attribute 1004) ---
	"Halloween: Die Job (paint)":                 {Attribute: 1004, Value: 0},
	"Halloween: Chromatic Corruption (paint)":    {Attribute: 1004, Value: 1},
	"Halloween: Putrescent Pigmentation (paint)": {Attribute: 1004, Value: 2},
	"Halloween: Spectral Spectrum (paint)":       {Attribute: 1004, Value: 3},
	"Halloween: Sinister Staining (paint)":       {Attribute: 1004, Value: 4},

	// --- Footprints (Attribute 1005) ---
	"Halloween: Team Spirit Footprints":    {Attribute: 1005, Value: 1},
	"Halloween: Headless Horseshoes":       {Attribute: 1005, Value: 2},
	"Halloween: Gangreen Footprints":       {Attribute: 1005, Value: 8421376},
	"Halloween: Corpse Gray Footprints":    {Attribute: 1005, Value: 3100495},
	"Halloween: Violent Violet Footprints": {Attribute: 1005, Value: 5322826},
	"Halloween: Rotten Orange Footprints":  {Attribute: 1005, Value: 13595446},
	"Halloween: Bruised Purple Footprints": {Attribute: 1005, Value: 8208497},

	// --- Vocal Effects (Attribute 1006) ---
	"Halloween: Voices from Below": {Attribute: 1006, Value: 1},

	// --- Weapon Effects (Attributes 1007, 1008, 1009) ---
	"Halloween: Pumpkin Bombs":        {Attribute: 1007, Value: 1},
	"Halloween: Gourd Grenades":       {Attribute: 1007, Value: 1}, // Alias
	"Halloween: Halloween Fire":       {Attribute: 1008, Value: 1},
	"Halloween: Spectral Flame":       {Attribute: 1008, Value: 1}, // Alias
	"Halloween: Exorcism":             {Attribute: 1009, Value: 1},
	"Halloween: Squash Rockets":       {Attribute: 1007, Value: 1}, // Alias
	"Halloween: Sentry Quad-Pumpkins": {Attribute: 1007, Value: 1}, // Alias
}

var retiredKeysNames []string

// GlobalNormalizationMap maps retired/legacy defindexes to their canonical counterparts.
var GlobalNormalizationMap = map[int]int{
	// Keys
	5049: 5021, 5067: 5021, 5072: 5021, 5073: 5021, 5079: 5021, 5081: 5021,
	5628: 5021, 5631: 5021, 5632: 5021, 5711: 5021, 5713: 5021, 5714: 5021,
	5715: 5021, 5716: 5021, 5717: 5021, 5762: 5021,
	// Lugermorph
	294: 160,
	// Strangifiers
	6523: 6522, 6526: 6522, 6530: 6522, 6531: 6522, 6532: 6522, 6534: 6522,
	// Killstreak Kits
	6520: 6527, 6521: 6527, 11051: 6527, 11052: 6527,
}

// NormalizeDefindex returns the "canonical" defindex for an item.
func NormalizeDefindex(defindex int) int {
	if norm, ok := GlobalNormalizationMap[defindex]; ok {
		return norm
	}

	return defindex
}

// IsAustraliumDefindex returns true if the defindex can be an Australium weapon.
func IsAustraliumDefindex(defindex int) bool {
	switch defindex {
	case 13, 45, 18, 228, 21, 38, 19, 20, 132, 172, 15, 424, 141, 197, 29, 36, 14, 16, 61:
		return true
	}

	return false
}

// IsNativeFestive returns true if the defindex belongs to a "native" Festive item (from old crates).
func IsNativeFestive(defindex int) bool {
	switch defindex {
	case 654, 658, 659, 660, 661, 662, 663, 664, 665, 669, 1081, 1082, 1085:
		return true
	}

	return false
}

func init() {
	for _, info := range retiredKeys {
		retiredKeysNames = append(retiredKeysNames, strings.ToLower(info.Name))
	}
}
