// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	capabilitiesResourceURI      = "beget://capabilities"
	capabilitiesResourceMIMEType = "application/json"
)

type capabilityCatalog struct {
	Version    int                  `json:"version"`
	Usage      string               `json:"usage"`
	Categories []capabilityCategory `json:"categories"`
}

type capabilityCategory struct {
	Name          string   `json:"name"`
	Scenario      string   `json:"scenario"`
	Documentation string   `json:"documentation,omitempty"`
	Inspect       []string `json:"inspect"`
	Change        []string `json:"change"`
}

type capabilitySection struct {
	name, scenario, documentation string
}

var capabilitySections = map[string]capabilitySection{
	"local": {
		name:     "local diagnostics",
		scenario: "Check whether server-side credentials are ready, inspect server capabilities, or validate a candidate mailbox password without a Beget request.",
	},
	"user": {
		name:          "account",
		scenario:      "Inspect plan limits and account usage, or change SSH availability.",
		documentation: "https://beget.com/ru/kb/api/funkczii-upravleniya-akkauntom",
	},
	"backup": {
		name:          "backups",
		scenario:      "Discover backup IDs and contents before queuing restore or download work, then inspect the backup log.",
		documentation: "https://beget.com/ru/kb/api/funkczii-upravleniya-bekapami",
	},
	"cron": {
		name:          "cron",
		scenario:      "List jobs to obtain row numbers before adding, editing, hiding, or deleting a task.",
		documentation: "https://beget.com/ru/kb/api/funkczii-upravleniya-cron",
	},
	"dns": {
		name:          "dns",
		scenario:      "Read the complete active record group before replacing it, then read it again to verify propagation state.",
		documentation: "https://beget.com/ru/kb/api/funkczii-upravleniya-dns",
	},
	"ftp": {
		name:          "ftp",
		scenario:      "List FTP accounts to obtain the exact suffix before changing a password or deleting an account.",
		documentation: "https://beget.com/ru/kb/api/funkczii-upravleniya-ftp",
	},
	"mysql": {
		name:          "mysql",
		scenario:      "List databases and access sources before creating, changing, or deleting either one.",
		documentation: "https://beget.com/ru/kb/api/funkczii-upravleniya-mysql",
	},
	"site": {
		name:          "sites",
		scenario:      "List sites and linked domains to obtain exact IDs before link, delete, freeze, or unfreeze operations.",
		documentation: "https://beget.com/ru/kb/api/funkczii-upravleniya-sajtami",
	},
	"domain": {
		name:          "domains",
		scenario:      "List domains, subdomains, zones, PHP versions, or directives before changing the matching resource.",
		documentation: "https://beget.com/ru/kb/api/funkczii-dlya-raboty-s-domenami",
	},
	"mail": {
		name:          "mail",
		scenario:      "List mailboxes or forwarding destinations before changing mailbox settings, forwarding, or catch-all mail.",
		documentation: "https://beget.com/ru/kb/api/funkczii-dlya-raboty-s-pochtoj",
	},
	"stat": {
		name:          "load statistics",
		scenario:      "List site or database load first, then request details only for the selected resource.",
		documentation: "https://beget.com/ru/kb/api/funkczii-dlya-sbora-statistiki",
	},
}

var encodedCapabilityCatalog = mustCapabilityCatalogJSON()

func addCapabilitiesResource(server *mcp.Server) {
	content := encodedCapabilityCatalog
	server.AddResource(&mcp.Resource{
		URI:         capabilitiesResourceURI,
		Name:        "beget_capabilities",
		Title:       "Beget tool routing catalog",
		Description: "Optional local routing aid. Read it only when tools/list does not make the correct category clear; reading it never calls Beget.",
		MIMEType:    capabilitiesResourceMIMEType,
		Size:        int64(len(content)),
	}, func(_ context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		if request.Params.URI != capabilitiesResourceURI {
			return nil, mcp.ResourceNotFoundError(request.Params.URI)
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: capabilitiesResourceURI, MIMEType: capabilitiesResourceMIMEType, Text: content,
		}}}, nil
	})
}

func mustCapabilityCatalogJSON() string {
	catalog := buildCapabilityCatalog()
	encoded, err := json.Marshal(catalog)
	if err != nil {
		panic(fmt.Errorf("encode capability catalog: %w", err))
	}
	return string(encoded)
}

func buildCapabilityCatalog() capabilityCatalog {
	catalog := capabilityCatalog{
		Version: 1,
		Usage:   "Optional routing aid. Read only when tool selection remains unclear after tools/list. It makes no Beget request; initialize instructions and tool schemas remain authoritative.",
	}
	categoryBySection := make(map[string]int, len(capabilitySections))
	for _, operation := range operationCatalog {
		index, exists := categoryBySection[operation.section]
		if !exists {
			section, ok := capabilitySections[operation.section]
			if !ok {
				panic(fmt.Errorf("operation %s has undocumented section %q", operation.name, operation.section))
			}
			index = len(catalog.Categories)
			categoryBySection[operation.section] = index
			catalog.Categories = append(catalog.Categories, capabilityCategory{
				Name: section.name, Scenario: section.scenario, Documentation: section.documentation,
				Inspect: []string{}, Change: []string{},
			})
		}
		if operation.mutating {
			catalog.Categories[index].Change = append(catalog.Categories[index].Change, operation.name)
		} else {
			catalog.Categories[index].Inspect = append(catalog.Categories[index].Inspect, operation.name)
		}
	}
	return catalog
}
