package tf2schema

import "strings"

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

var retiredKeysNames []string

func init() {
	for _, info := range retiredKeys {
		retiredKeysNames = append(retiredKeysNames, strings.ToLower(info.Name))
	}
}
