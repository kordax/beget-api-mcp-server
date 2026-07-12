package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/kordax/beget-api-mcp-server/internal/beget"
	"github.com/kordax/beget-api-mcp-server/internal/config"
	begetserver "github.com/kordax/beget-api-mcp-server/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	configuration, err := config.FromEnvironment()
	if err != nil {
		log.Fatal(err)
	}
	httpClient := &http.Client{Timeout: configuration.Timeout}
	client, err := beget.NewClient(configuration.BaseURL, configuration.Login, configuration.APIKey, httpClient)
	if err != nil {
		log.Fatal(err)
	}
	if err := begetserver.New(client).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
