//go:build mas

package updater

// MAS contains no downloader, installer, helper launcher or GitHub client.
type Phase int

const (
	Idle Phase = iota
	Restarting
)

func (Phase) String() string { return "store-managed" }

type State struct {
	Phase Phase
	Open  bool
}
