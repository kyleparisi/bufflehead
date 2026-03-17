package main

import (
	"log"

	"bufflehead/internal/control"
	"bufflehead/internal/db"
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

	ctrlServer := control.New(9900)
	ctrlServer.Start()

	app := new(ui.App)
	app.Duck = duck
	app.ControlServer = ctrlServer
	SceneTree.Add(app.AsNode())

	startup.Scene()
}
