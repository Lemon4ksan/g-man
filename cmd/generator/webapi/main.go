// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/ast"
	"github.com/lemon4ksan/foundation/codec/json"
)

// MaxPositionalParams determines the threshold for using positional parameters.
// Methods with more parameters than this will use a dedicated Request DTO struct to avoid argument order confusion.
const MaxPositionalParams = 3

// APIListResp models the root response structure of Valve's GetSupportedAPIList endpoint.
type APIListResp struct {
	APIList struct {
		Interfaces []Interface `json:"interfaces"`
	} `json:"apilist"`
}

// Interface models a single Steam WebAPI service interface.
type Interface struct {
	RawName         string   `json:"name"`
	GoInterfaceName string   `json:"-"`
	Methods         []Method `json:"methods"`
}

// Method models a single Steam WebAPI remote procedure call.
type Method struct {
	Name        string      `json:"name"`
	Version     int         `json:"version"`
	HTTPMethod  string      `json:"httpmethod"`
	Description string      `json:"description,omitempty"`
	Parameters  []Parameter `json:"parameters,omitempty"`

	GoMethodName string `json:"-"`
	UseStruct    bool   `json:"-"`
	ReqStruct    string `json:"-"`
}

// Parameter models an individual request parameter in a Steam WebAPI call.
type Parameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Optional    bool   `json:"optional"`
	Description string `json:"description,omitempty"`

	GoFieldName string `json:"-"`
	GoParamName string `json:"-"`
	GoFieldType string `json:"-"`
}

func main() {
	inFlag := flag.String("in", "cmd/generator/api.steampowered.com.json", "Path to input Steam WebAPI JSON schema")
	outFlag := flag.String("out", "pkg/steam/webapi/api.go", "Path to output declarative aoni Go file")
	fetchFlag := flag.Bool("fetch", false, "Fetch latest schema from Valve Steam WebAPI")
	runVortex := flag.Bool("run-vortex", true, "Automatically run vortex after creating api.go")

	flag.Parse()

	var (
		data []byte
		err  error
	)

	if *fetchFlag {
		log.Println("Fetching latest Steam WebAPI schema from Valve...")

		url := "https://api.steampowered.com/ISteamWebAPIUtil/GetSupportedAPIList/v1/"

		data, err = fetchWebAPISchema(url)
		if err != nil {
			log.Fatalf("Failed to fetch WebAPI schema: %v", err)
		}
	} else {
		inputPath := filepath.Clean(*inFlag)

		data, err = os.ReadFile(inputPath)
		if err != nil {
			log.Fatalf("Failed to read schema file %s: %v", inputPath, err)
		}
	}

	var apiResp APIListResp
	if err := json.Unmarshal(data, &apiResp); err != nil {
		log.Fatalf("Failed to unmarshal JSON schema: %v", err)
	}

	prepareInterfaces(apiResp.APIList.Interfaces)

	file := buildAST(apiResp.APIList.Interfaces)

	outPath := filepath.Clean(*outFlag)
	if err := ast.WriteFile(outPath, file); err != nil {
		log.Fatalf("Failed to write AST contract file %s: %v", outPath, err)
	}

	log.Printf("Successfully synthesized declarative Aoni AST contract for %d interfaces to %s\n",
		len(apiResp.APIList.Interfaces), outPath)

	if *runVortex {
		log.Printf("⚡ Running 'vortex gen %s'...\n", outPath)
		cmd := exec.CommandContext(
			context.Background(),
			"vortex",
			"gen",
			"-file="+filepath.Base(outPath),
		) //nolint:gosec
		cmd.Dir = filepath.Dir(outPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			log.Fatalf("vortex compilation failed: %v", err)
		}

		log.Println("✔ WebAPI compilation completed successfully!")
	}
}

func fetchWebAPISchema(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func prepareInterfaces(ifaces []Interface) {
	// 1. Initial name resolution and parameter preparation
	for i := range ifaces {
		iface := &ifaces[i]
		iface.GoInterfaceName = formatGoInterfaceName(iface.RawName)

		methodCounts := make(map[string]int)
		for _, m := range iface.Methods {
			methodCounts[m.Name]++
		}

		for j := range iface.Methods {
			m := &iface.Methods[j]

			baseName := formatGoName(m.Name)
			if methodCounts[m.Name] > 1 {
				m.GoMethodName = fmt.Sprintf("%sV%d", baseName, m.Version)
			} else {
				m.GoMethodName = baseName
			}

			if len(m.Parameters) > MaxPositionalParams {
				m.UseStruct = true
			}

			for k := range m.Parameters {
				p := &m.Parameters[k]
				p.GoFieldName = formatGoName(p.Name)
				p.GoParamName = formatGoParamName(p.Name)
				p.GoFieldType = formatGoType(p.Type)
			}
		}
	}

	// 2. Count collisions of DTO struct names across all interfaces
	reqCounts := make(map[string]int)
	for i := range ifaces {
		iface := &ifaces[i]
		for j := range iface.Methods {
			m := &iface.Methods[j]
			if m.UseStruct {
				shortName := m.GoMethodName + "Request"
				reqCounts[shortName]++
			}
		}
	}

	// 3. Assign concise, non-colliding DTO struct names
	for i := range ifaces {
		iface := &ifaces[i]
		for j := range iface.Methods {
			m := &iface.Methods[j]
			if m.UseStruct {
				shortName := m.GoMethodName + "Request"
				if reqCounts[shortName] == 1 {
					m.ReqStruct = shortName
				} else {
					m.ReqStruct = fmt.Sprintf("%s%sRequest", iface.GoInterfaceName, m.GoMethodName)
				}
			}
		}
	}
}

func buildAST(ifaces []Interface) *ast.File {
	file := ast.NewFile("webapi")
	file.AddImport("encoding/json", "")

	for _, iface := range ifaces {
		svc := file.NewService(iface.GoInterfaceName).
			WithBaseURL("https://api.steampowered.com/" + iface.RawName).
			WithEngine(ast.EngineFast).
			WithCasing(ast.CasingSnake).
			WithDoc(fmt.Sprintf("%s provides methods for the Steam %s WebAPI service.", iface.GoInterfaceName, iface.RawName))

		for _, m := range iface.Methods {
			httpMethod := strings.ToUpper(m.HTTPMethod)
			if httpMethod == "" {
				httpMethod = "GET"
			}

			path := fmt.Sprintf("/%s/v%d/", m.Name, m.Version)
			methodNode := svc.NewMethod(m.GoMethodName, httpMethod, path).
				WithResponse("*json.RawMessage")

			if m.Description != "" {
				methodNode.WithDoc(fmt.Sprintf("%s: %s", m.GoMethodName, m.Description))
			} else {
				methodNode.WithDoc(fmt.Sprintf("%s calls %s.", m.GoMethodName, path))
			}

			if strings.EqualFold(m.HTTPMethod, "POST") {
				methodNode.WithForm()
			}

			if m.UseStruct {
				methodNode.WithRequest(m.ReqStruct)

				reqStruct := file.NewStruct(m.ReqStruct).
					WithDoc(fmt.Sprintf("%s represents request parameters for %s.", m.ReqStruct, m.GoMethodName))

				for _, p := range m.Parameters {
					fld := reqStruct.NewField(p.GoFieldName, p.GoFieldType).
						WithQuery(p.Name).
						SetRequired(!p.Optional)

					if p.Description != "" {
						fld.WithDoc(p.Description)
					}
				}
			} else {
				for _, p := range m.Parameters {
					methodNode.AddParam(p.GoParamName, p.GoFieldType)
				}
			}
		}
	}

	return file
}

func formatGoInterfaceName(name string) string {
	name = strings.TrimPrefix(name, "I")
	name = strings.ReplaceAll(name, "_", "")
	return formatGoName(name)
}

func formatGoParamName(name string) string {
	pascal := formatGoName(name)
	if len(pascal) == 0 {
		return "param"
	}

	runes := []rune(pascal)
	res := strings.ToLower(string(runes[0])) + string(runes[1:])

	switch res {
	case "type":
		return "typ"
	case "range":
		return "rng"
	case "default":
		return "defVal"
	case "select":
		return "sel"
	case "interface":
		return "iface"
	case "map":
		return "m"
	case "case":
		return "c"
	case "var":
		return "v"
	case "func":
		return "fn"
	case "struct":
		return "s"
	case "import":
		return "imp"
	case "package":
		return "pkg"
	default:
		return res
	}
}

func formatGoName(name string) string {
	name = strings.ReplaceAll(name, "[0]", "")
	name = strings.ReplaceAll(name, "[", "")
	name = strings.ReplaceAll(name, "]", "")

	switch strings.ToLower(name) {
	case "steamid":
		return "SteamID"
	case "steamidkey":
		return "SteamIDKey"
	case "appid":
		return "AppID"
	case "cellid":
		return "CellID"
	case "maxcount":
		return "MaxCount"
	case "cmtype":
		return "CMType"
	case "accountid":
		return "AccountID"
	case "gameid":
		return "GameID"
	case "matchid":
		return "MatchID"
	case "leagueid":
		return "LeagueID"
	case "serverid":
		return "ServerID"
	case "itemid":
		return "ItemID"
	case "classid":
		return "ClassID"
	case "instanceid":
		return "InstanceID"
	case "groupid":
		return "GroupID"
	case "eventid":
		return "EventID"
	case "badgeid":
		return "BadgeID"
	case "sessionid":
		return "SessionID"
	case "clientid":
		return "ClientID"
	case "ugcid":
		return "UGCID"
	case "pickid":
		return "PickID"
	}

	parts := strings.Split(name, "_")
	for i, part := range parts {
		lower := strings.ToLower(part)
		switch lower {
		case "id":
			parts[i] = "ID"
		case "ip":
			parts[i] = "IP"
		case "steamid":
			parts[i] = "SteamID"
		case "appid":
			parts[i] = "AppID"
		case "cellid":
			parts[i] = "CellID"
		case "maxcount":
			parts[i] = "MaxCount"
		case "accountid":
			parts[i] = "AccountID"
		case "url":
			parts[i] = "URL"
		case "rsa":
			parts[i] = "RSA"
		case "rtmp":
			parts[i] = "RTMP"
		case "ugc":
			parts[i] = "UGC"
		case "cidr":
			parts[i] = "CIDR"
		case "asn":
			parts[i] = "ASN"
		case "cdn":
			parts[i] = "CDN"
		case "sdr":
			parts[i] = "SDR"
		case "cm":
			parts[i] = "CM"
		case "dpc":
			parts[i] = "DPC"
		case "csgo":
			parts[i] = "CSGO"
		case "dota2":
			parts[i] = "DOTA2"
		case "tf2":
			parts[i] = "TF2"
		default:
			if len(part) > 0 {
				parts[i] = strings.ToUpper(part[:1]) + part[1:]
			}
		}
	}

	res := strings.Join(parts, "")
	if len(res) == 0 {
		return "Field"
	}

	if res[0] >= '0' && res[0] <= '9' {
		res = "V" + res
	}

	return res
}

func formatGoType(t string) string {
	switch t {
	case "uint64", "uint32", "int32", "bool", "string":
		return t
	case "{message}":
		return "string"
	case "{enum}":
		return "int32"
	case "rawbyte":
		return "[]byte"
	default:
		return "string"
	}
}
