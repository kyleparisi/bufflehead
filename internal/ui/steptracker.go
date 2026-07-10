package ui

import (
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Vector2"
)

// stepTracker is a vertical connection-status list: one row per stage with a
// colored status dot and label (pending → active → done, or failed). It mirrors
// the "Connection Status" panel from the Pro-Grade Data System design and is
// driven by control.ConnectStage values.
type stepTracker struct {
	root   VBoxContainer.Instance
	dots   []Label.Instance
	labels []Label.Instance
}

// newStepTracker builds a tracker for the given ordered stage labels. All stages
// start pending. The root node is named "StepTracker" so it is identifiable in
// the control-server ui-tree.
func newStepTracker(stageLabels []string) *stepTracker {
	t := &stepTracker{}
	t.root = VBoxContainer.New()
	t.root.AsNode().SetName("StepTracker")
	t.root.AsControl().AddThemeConstantOverride("separation", 10)

	for _, text := range stageLabels {
		row := HBoxContainer.New()
		row.AsControl().AddThemeConstantOverride("separation", 8)

		dot := Label.New()
		dot.SetText("○")
		dot.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
		dot.AsControl().AddThemeColorOverride("font_color", colorStatusGray)
		dot.AsControl().SetCustomMinimumSize(Vector2.New(scaled(14), 0))

		lbl := Label.New()
		lbl.SetText(text)
		lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
		lbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)

		row.AsNode().AddChild(dot.AsNode())
		row.AsNode().AddChild(lbl.AsNode())
		t.root.AsNode().AddChild(row.AsNode())

		t.dots = append(t.dots, dot)
		t.labels = append(t.labels, lbl)
	}
	return t
}

// setActive marks every stage before `active` as done (green), the active stage
// as in-progress (amber) or failed (red) per `failed`, and the rest pending.
func (t *stepTracker) setActive(active int, failed bool) {
	for i := range t.dots {
		switch {
		case i < active:
			t.dots[i].SetText("●")
			t.dots[i].AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
			t.labels[i].AsControl().AddThemeColorOverride("font_color", colorText)
		case i == active && failed:
			t.dots[i].SetText("●")
			t.dots[i].AsControl().AddThemeColorOverride("font_color", colorStatusRed)
			t.labels[i].AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		case i == active:
			t.dots[i].SetText("●")
			t.dots[i].AsControl().AddThemeColorOverride("font_color", colorStatusYellow)
			t.labels[i].AsControl().AddThemeColorOverride("font_color", colorText)
		default:
			t.dots[i].SetText("○")
			t.dots[i].AsControl().AddThemeColorOverride("font_color", colorStatusGray)
			t.labels[i].AsControl().AddThemeColorOverride("font_color", colorTextDim)
		}
	}
}

// markAllDone marks every stage complete.
func (t *stepTracker) markAllDone() {
	t.setActive(len(t.dots), false)
}
