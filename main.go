package main

import (
	"log"

	"bufflehead/internal/control"
	"bufflehead/internal/db"
	"bufflehead/internal/models"
	"bufflehead/internal/ui"

	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/Engine"
	"graphics.gd/classdb/SceneTree"
	"graphics.gd/classdb/Window"
	"graphics.gd/startup"
	"graphics.gd/variant/Object"
	"graphics.gd/variant/Vector2i"
)

func main() {
	startup.LoadingScene()

	ui.RegisterAll()

	DisplayServer.WindowSetSize(Vector2i.New(1440, 900), 0)
	DisplayServer.WindowSetMinSize(Vector2i.New(800, 500), 0)

	if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
		if root := tree.Root(); root != Window.Nil {
			root.SetTitle("Bufflehead")
		}
	}

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

	ctrlServer := control.New(9900)
	ctrlServer.Start()

	bookmarkStore := models.NewBookmarkStore()

	app := new(ui.App)
	app.Duck = duck
	app.ControlServer = ctrlServer
	app.GatewayConfig = gatewayCfg
	app.BookmarkStore = bookmarkStore
	SceneTree.Add(app.AsNode())

	startup.Scene()
}
