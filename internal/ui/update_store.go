//go:build mas

package ui

func (*App) autoCheckForUpdates() {}
func (*App) checkForUpdates(bool) {}
func (*App) downloadUpdate()      {}
func (*App) installUpdate()       {}
func (*App) drainUpdateEvents()   {}
func (*App) renderUpdate()        {}

func (*App) quitForUpdate() {}
