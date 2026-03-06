package main

import (
	"log"

	"parquet-viewer/internal/control"
	"parquet-viewer/internal/db"
	"parquet-viewer/internal/models"
	"parquet-viewer/internal/ui"

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

	// Register all custom classes before building the scene.
	ui.RegisterAll()

	// Set window size now that the engine is initialized.
	DisplayServer.WindowSetSize(Vector2i.New(1024, 640), 0)
	DisplayServer.WindowSetMinSize(Vector2i.New(640, 400), 0)

	// Extend content into the native title bar (disabled — causes invisible window on some macOS hardware)
	if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
		if root := tree.Root(); root != Window.Nil {
			root.SetTitle("Parquet Viewer")
			root.SetContentScaleFactor(2.0)
		}
	}

	duck, err := db.New()
	if err != nil {
		log.Fatalf("duckdb init: %v", err)
	}
	defer duck.Close()

	ctrlServer := control.New(9900)
	ctrlServer.Start()

	app := new(ui.App)
	app.Duck = duck
	app.State = models.NewAppState()
	app.ControlServer = ctrlServer
	SceneTree.Add(app.AsNode())

	startup.Scene()
}
