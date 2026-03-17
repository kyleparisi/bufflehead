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

	ctrlServer := control.New(9900)
	ctrlServer.Start()

	app := new(ui.App)
	app.Duck = duck
	app.ControlServer = ctrlServer
	SceneTree.Add(app.AsNode())

	startup.Scene()
}
