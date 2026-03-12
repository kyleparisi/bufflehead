package main

import (
	"log"

	"bufflehead/internal/control"
	"bufflehead/internal/db"
	"bufflehead/internal/ui"

	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/Engine"
	"graphics.gd/classdb/SceneTree"
	"graphics.gd/classdb/Window"
	"graphics.gd/startup"
	"graphics.gd/variant/Float"
	"graphics.gd/variant/Object"
	"graphics.gd/variant/Vector2i"
)

func main() {
	startup.LoadingScene()

	// Register all custom classes before building the scene.
	ui.RegisterAll()

	// Scale UI to match screen DPI (2.0 on Retina, 1.0 on standard).
	// Window size is set in physical pixels; multiply by scale so logical size stays 1440x900.
	scale := float64(DisplayServer.ScreenGetScale())
	if scale < 1 {
		scale = 1
	}
	winW := int(1440 * scale)
	winH := int(900 * scale)
	minW := int(800 * scale)
	minH := int(500 * scale)
	DisplayServer.WindowSetSize(Vector2i.New(winW, winH), 0)
	DisplayServer.WindowSetMinSize(Vector2i.New(minW, minH), 0)

	if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
		if root := tree.Root(); root != Window.Nil {
			root.SetTitle("Bufflehead")
			root.SetContentScaleFactor(Float.X(scale))
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
	app.ControlServer = ctrlServer
	SceneTree.Add(app.AsNode())

	startup.Scene()
}
