// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

var (
	outputFile = flag.String("out", "", "Output Go file path")
	pkgName    = flag.String("pkg", "protocol", "Go package name")
	apiUrl     = flag.String(
		"url",
		"https://api.github.com/repos/SteamRE/SteamKit/contents/Resources/SteamLanguage",
		"GitHub API URL for SteamLanguage files",
	)
)

// #nosec G101 -- Not credentials
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

func main() {
	flag.Parse()

	if *outputFile == "" {
		fmt.Println("Usage: steamlang -out <path> [-pkg <name>] [-url <url>]")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	files, err := fetchFileList(ctx, *apiUrl)
	if err != nil {
		cancel()
		os.Exit(1)
	}

	var allEnums []*EnumDef

	for _, file := range files {
		if !strings.HasSuffix(file.Name, ".steamd") {
			continue
		}

		fmt.Printf("  > Parsing %s\n", file.Name)

		content, err := downloadFile(ctx, file.DownloadURL)
		if err != nil {
			fmt.Printf("Failed to download %s: %v\n", file.Name, err)
			continue
		}

		allEnums = append(allEnums, parseSteamd(content)...)
	}

	generateEnumsFile(allEnums, *outputFile, *pkgName)
	fmt.Printf("✅ Generated %d enums to %s\n", len(allEnums), *outputFile)
	cancel()
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

func parseSteamd(content string) []*EnumDef {
	var (
		res []*EnumDef
		cur *EnumDef
	)

	reStart := regexp.MustCompile(`^enum (E[a-zA-Z0-9]+)(?:<([a-z]+)>)?`)
	reVal := regexp.MustCompile(`^([A-Za-z0-9_]+) = ([^;]+);(.*)$`)

	lines := strings.SplitSeq(content, "\n")
	for line := range lines {
		line = strings.TrimSpace(line)

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
				rawType := "int"
				if m[2] != "" {
					rawType = m[2]
				}

				cur = &EnumDef{Name: m[1], Type: mapSteamType(rawType)}
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

				if strings.HasPrefix(vVal, "0x") && len(vVal) > 9 {
					cur.Type = "uint32"
				}

				cur.Values = append(cur.Values, EnumValue{
					Name:    vName,
					Value:   vVal,
					Comment: strings.TrimSpace(m[3] + " " + commentInLine),
				})
			}
		}
	}

	return res
}

func generateEnumsFile(enums []*EnumDef, target, pkg string) {
	_ = os.MkdirAll(filepath.Dir(target), 0o755)

	f, _ := os.Create(target)
	defer f.Close()

	sort.Slice(enums, func(i, j int) bool { return enums[i].Name < enums[j].Name })

	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "// Code generated by gen tool. DO NOT EDIT.\npackage %s\n\nimport \"fmt\"\n\n", pkg)

	for _, enum := range enums {
		if len(enum.Values) == 0 {
			continue
		}

		fmt.Fprintf(w, "type %s %s\n\nconst (\n", enum.Name, enum.Type)

		nameToValue := make(map[string]string)
		seenNames := make(map[string]bool)

		for _, val := range enum.Values {
			resolved := val.Value
			if !isNumeric(val.Value) && !strings.Contains(val.Value, "|") {
				if v, ok := nameToValue[val.Value]; ok {
					resolved = v
				}
			}

			nameToValue[val.Name] = resolved
		}

		for _, val := range enum.Values {
			fullName := fmt.Sprintf("%s_%s", enum.Name, val.Name)

			if seenNames[fullName] {
				safeVal := strings.ReplaceAll(val.Value, "-", "Neg")
				fullName = fmt.Sprintf("%s_%s", fullName, safeVal)
			}

			seenNames[fullName] = true

			goValue := formatValue(enum.Name, enum.Type, val.Value)

			comment := ""
			if val.Comment != "" {
				comment = " // " + strings.TrimSpace(val.Comment)
			}

			fmt.Fprintf(w, "\t%s %s = %s%s\n", fullName, enum.Name, goValue, comment)
		}

		fmt.Fprint(w, ")\n\n")

		fmt.Fprintf(w, "var %s_name = map[%s]string{\n", enum.Name, enum.Name)

		seenMapValues := make(map[string]bool)
		seenNamesForMap := make(map[string]bool)

		for _, v := range enum.Values {
			if strings.Contains(v.Value, "|") {
				continue
			}

			actualNumVal := nameToValue[v.Name]

			if !seenMapValues[actualNumVal] {
				fullName := fmt.Sprintf("%s_%s", enum.Name, v.Name)
				if seenNamesForMap[fullName] {
					safeVal := strings.ReplaceAll(v.Value, "-", "Neg")
					fullName = fmt.Sprintf("%s_%s", fullName, safeVal)
				}

				seenNamesForMap[fullName] = true

				fmt.Fprintf(w, "\t%s: \"%s\",\n", fullName, v.Name)

				seenMapValues[actualNumVal] = true
			}
		}

		fmt.Fprint(w, "}\n\n")

		fmt.Fprintf(
			w,
			"func (x %s) String() string {\n\tif name, ok := %s_name[x]; ok {\n\t\treturn name\n\t}\n\treturn fmt.Sprintf(\"%%d\", x)\n}\n\n",
			enum.Name,
			enum.Name,
		)
	}

	_ = w.Flush()
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

func cleanName(eName, vName string) string {
	if prefix, ok := enumsWithDifferentPrefixes[eName]; ok {
		if after, ok0 := strings.CutPrefix(vName, prefix); ok0 {
			return after
		}
	}

	vName = strings.TrimPrefix(vName, "k_"+eName+"_")
	vName = strings.TrimPrefix(vName, "k_")

	return vName
}

func formatValue(enumName, typeName, val string) string {
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

func fetchFileList(ctx context.Context, url string) ([]GitHubFile, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "g-man-generator")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var files []GitHubFile

	err = json.NewDecoder(resp.Body).Decode(&files)

	return files, err
}

func downloadFile(ctx context.Context, url string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "g-man-generator")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)

	return string(b), err
}
