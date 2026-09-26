package ui

import (
	licenses "bufflehead/packaging/licenses/generated"
	"graphics.gd/classdb/TextEdit"
	"graphics.gd/classdb/Window"
	"graphics.gd/variant/Vector2"
	"graphics.gd/variant/Vector2i"
)

func (a *App) showLicenses() {
	win := Window.New()
	win.AsNode().SetName("ThirdPartyLicenses")
	win.SetTitle("Bufflehead — Third-Party Licenses")
	win.SetSize(Vector2i.New(850, 650))
	text := TextEdit.New()
	text.AsNode().SetName("LicenseText")
	text.SetEditable(false)
	text.SetText(licenses.Notices)
	text.AsControl().SetSize(Vector2.New(850, 650))
	win.AsNode().AddChild(text.AsNode())
	win.AsViewport().OnSizeChanged(func() { size := win.Size(); text.AsControl().SetSize(Vector2.New(float64(size.X), float64(size.Y))) })
	win.OnCloseRequested(func() { win.AsNode().QueueFree() })
	a.AsNode().AddChild(win.AsNode())
	win.Show()
}
