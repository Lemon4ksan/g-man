// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var enumsWithDifferentPrefixes = map[string]string{
	"EFrameAccumulatedStat":            "k_EFrameStat",
	"EHIDDeviceDisconnectMethod":       "k_EDeviceDisconnectMethod",
	"EHIDDeviceLocation":               "k_EDeviceLocation",
	"ELogFileType":                     "k_ELogFile",
	"EPublishedFileForSaleStatus":      "k_PFFSS_",
	"ERemoteClientBroadcastMsg":        "k_ERemoteDevice",
	"ERemoteDeviceAuthorizationResult": "k_ERemoteDeviceAuthorization",
	"ERemoteDeviceStreamingResult":     "k_ERemoteDeviceStreaming",
	"EStreamControlMessage":            "k_EStreamControl",
	"EStreamDataMessage":               "k_EStream",
	"EStreamDiscoveryMessage":          "k_EStreamDiscovery",
	"EStreamFrameEvent":                "k_EStream",
	"EStreamFramerateLimiter":          "k_EStreamFramerate",
	"EStreamGamepadInputType":          "k_EStreamGamepadInput",
	"EStreamingDataType":               "k_EStreaming",
	"EStreamMouseWheelDirection":       "k_EStreamMouseWheel",
	"EStreamQualityPreference":         "k_EStreamQuality",
	"EStreamStatsMessage":              "k_EStreamStats",
	"EChatRoomNotificationLevel":       "k_EChatroomNotificationLevel",
	"EPublishedFileQueryType":          "k_PublishedFileQueryType_",
	"EProtoAppType":                    "k_EAppType",
	"EBluetoothDeviceType":             "k_BluetoothDeviceType_",
	"EPlaytestStatus":                  "k_ETesterStatus",
	"ESystemAudioChannel":              "k_SystemAudioChannel_",
	"ESystemAudioDirection":            "k_SystemAudioDirection_",
	"ESystemAudioPortDirection":        "k_SystemAudioPortDirection_",
	"ESystemAudioPortType":             "k_SystemAudioPortType_",
	"ESystemFanControlMode":            "k_SystemFanControlMode_",
	"EAuthTokenRevokeAction":           "k_EAuthTokenRevoke",
	"EMarketingMessageAssociationType": "k_EMarketingMessage",
	"EMarketingMessageLookupType":      "k_EMarketingMessageLookup",
	"EMarketingMessageType":            "k_EMarketingMessage",
	"EMarketingMessageVisibility":      "k_EMarketingMessageVisible",
}

func mapSteamType(steamType string) string {
	switch steamType {
	case "uint":
		return "uint32"
	case "int":
		return "int32"
	case "byte":
		return "byte"
	case "short":
		return "int16"
	case "ushort":
		return "uint16"
	case "long":
		return "int64"
	case "ulong":
		return "uint64"
	default:
		return "uint32"
	}
}

type GitHubFile struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
}

type EnumValue struct {
	Name    string
	Value   string
	Comment string
}

type EnumDef struct {
	Name   string
	Type   string
	Values []EnumValue
}

func fetchFileList(url string) ([]GitHubFile, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Go-Steam-Client-Generator")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %d", resp.StatusCode)
	}

	var files []GitHubFile
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, err
	}
	return files, nil
}

func downloadFile(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func parseSteamd(content string) []*EnumDef {
	var res []*EnumDef
	var cur *EnumDef

	reStart := regexp.MustCompile(`^enum (E[a-zA-Z0-9]+)(?:<([a-z]+)>)?`)
	reVal := regexp.MustCompile(`^([A-Za-z0-9_]+) = ([^;]+);(.*)$`)

	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)

		// Trim comments
		commentInLine := ""
		if idx := strings.Index(line, "//"); idx != -1 {
			commentInLine = strings.TrimSpace(line[idx+2:])
			line = strings.TrimSpace(line[:idx])
		}

		if line == "" {
			continue
		}

		if cur == nil {
			if m := reStart.FindStringSubmatch(line); len(m) > 0 {
				name := m[1]
				rawType := "int"
				if m[2] != "" {
					rawType = m[2]
				}

				// If the type is not specified, but the values ​​are "flag" (usually large),
				// SteamLanguage often assumes uint. But let's start with int32.
				// mapSteamType translates "int" -> "int32", "uint" -> "uint32"
				cur = &EnumDef{
					Name: name,
					Type: mapSteamType(rawType),
				}
			}
		} else {
			if line == "{" {
				continue
			}
			if strings.HasPrefix(line, "}") {
				res = append(res, cur)
				cur = nil
				continue
			}

			if m := reVal.FindStringSubmatch(line); len(m) > 0 {
				vName := cleanName(cur.Name, m[1])
				vVal := strings.TrimSpace(m[2])

				// Heuristic: If we encounter an explicit negative number (not -1),
				// and the type is uint32 (by default or by mistake), change it to int32.
				if strings.HasPrefix(vVal, "-") && vVal != "-1" && strings.HasPrefix(cur.Type, "uint") {
					cur.Type = "int32"
				}
				// The opposite situation: if the type is int32, but the 0x80000000 flag (int32 overflow) is encountered,
				// change it to uint32.
				if strings.HasPrefix(vVal, "0x") && len(vVal) > 9 { // 0x + 8 chars
					cur.Type = "uint32"
				}

				finalComment := strings.TrimSpace(m[3])
				if finalComment == "" {
					finalComment = commentInLine
				}

				cur.Values = append(cur.Values, EnumValue{
					Name:    vName,
					Value:   vVal,
					Comment: finalComment,
				})
			}
		}
	}
	return res
}

func formatValue(enumName string, typeName string, val string) string {
	if (typeName == "uint32" || typeName == "uint64") && val == "-1" {
		if typeName == "uint64" {
			return "0xFFFFFFFFFFFFFFFF"
		}
		return "0xFFFFFFFF"
	}

	if strings.Contains(val, "|") {
		parts := strings.Split(val, "|")
		for i, p := range parts {
			p = strings.TrimSpace(p)
			// If it's a number (e.g. 0xFF), leave it as is
			// If it's text, add the EnumName_ prefix
			if !isNumeric(p) {
				parts[i] = enumName + "_" + p
			}
		}
		return strings.Join(parts, " | ")
	}

	if !isNumeric(val) {
		return enumName + "_" + val
	}

	return val
}

func isNumeric(s string) bool {
	if strings.HasPrefix(s, "0x") {
		return true
	}
	for _, c := range s {
		if (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}
