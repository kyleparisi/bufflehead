package ui

import "bufflehead/internal/sandbox"

// Spike integration: retain folder scopes for app-lifetime SQL/worker reads.
// No API request is allowed to present a native permission dialog.
var fileAccess = sandbox.NewSession(sandbox.New(sandbox.NewBookmarkStore()))

func (a *App) ExitTree() { fileAccess.Close() }
