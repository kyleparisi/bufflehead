package main

import (
	"fmt"
	"log"
	"strings"

	"bufflehead/internal/aws"
	"bufflehead/internal/control"
	"bufflehead/internal/db"
	"bufflehead/internal/models"
	"bufflehead/internal/ui"

	"graphics.gd/classdb/SceneTree"
	"graphics.gd/startup"
)

func main() {
	startup.LoadingScene()

	// Register all custom classes before building the scene.
	ui.RegisterAll()

	// Root viewport is configured as a hidden 1x1 borderless window at
	// (-32000, -32000) via project.godot. All UI lives in secondary
	// windows created by App. Font sizes self-scale via ui.fontSize().

	duck, err := db.New()
	if err != nil {
		log.Fatalf("duckdb init: %v", err)
	}
	defer duck.Close()

	// Load gateway config (optional — nil if no config file exists)
	gatewayCfg, err := models.LoadGatewayConfig()
	if err != nil {
		log.Printf("gateway config: %v", err)
	}

	// Check prerequisites if gateway config exists
	if gatewayCfg != nil && len(gatewayCfg.Gateways) > 0 {
		missing := aws.CheckPrerequisites()
		if len(missing) > 0 {
			fmt.Printf("Warning: gateway mode requires missing tools: %s\n", strings.Join(missing, ", "))
			fmt.Println("Install with:")
			for _, tool := range missing {
				switch tool {
				case "aws":
					fmt.Println("  brew install awscli")
				case "session-manager-plugin":
					fmt.Println("  brew install session-manager-plugin")
				}
			}
		}
	}

	ctrlServer := control.New(9900)
	ctrlServer.Start()

	app := new(ui.App)
	app.Duck = duck
	app.ControlServer = ctrlServer
	app.GatewayConfig = gatewayCfg
	SceneTree.Add(app.AsNode())

	startup.Scene()
}
