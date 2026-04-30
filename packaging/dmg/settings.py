import os
from pathlib import Path

root = Path(os.environ.get("DMGBUILD_ROOT", Path.cwd()))
app_path = root / "releases/darwin/universal/Bufflehead.app"
bg_path = root / "packaging/dmg/background.jpg"

format = "UDBZ"
size = None
files = [str(app_path)]
symlinks = {"Applications": "/Applications"}
icon = str(root / "graphics/Bufflehead.icns")

background = str(bg_path)
show_status_bar = False
show_tab_view = False
show_toolbar = False
show_pathbar = False
show_sidebar = False
sidebar_width = 0

window_rect = ((200, 200), (660, 400))
default_view = "icon-view"

arrange_by = None
grid_offset = (0, 0)
grid_spacing = 100
scroll_position = (0, 0)
label_pos = "bottom"
text_size = 14
icon_size = 128
text_color = "#ffffff"

icon_locations = {
    "Bufflehead.app": (180, 200),
    "Applications": (480, 200),
}
