package control

import "strings"

// ConnectStage identifies a phase of establishing a remote gateway connection.
// The stages are ordered; a UI step tracker renders them top to bottom.
type ConnectStage int

const (
	StageAuth ConnectStage = iota // authenticate (SSO / IAM)
	StageTunnel                   // establish the SSM/SSH tunnel
	StageConnectDB                // connect to the database over the tunnel
	StageLoadSchema               // load tables + schema
	NumConnectStages
)

// ConnectStageLabels are the human-readable labels for each ConnectStage,
// indexed by stage.
var ConnectStageLabels = []string{
	"Authenticate (SSO)",
	"Establish tunnel",
	"Connect to database",
	"Load schema",
}

// ConnectStageLabel returns the label for a stage, or "" if out of range.
func ConnectStageLabel(s ConnectStage) string {
	if s < 0 || int(s) >= len(ConnectStageLabels) {
		return ""
	}
	return ConnectStageLabels[s]
}

// ConnectStageFromMessage maps a progress message emitted during a gateway
// connection to the stage it represents. By the time these messages appear,
// auth and tunnel are already established, so an unrecognized message defaults
// to StageConnectDB. Matching is case-insensitive; order matters because
// "loading schema" also contains no other stage keywords but "connecting to
// database" must not be mistaken for the tunnel stage.
func ConnectStageFromMessage(msg string) ConnectStage {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "schema") || strings.Contains(m, "loading table"):
		return StageLoadSchema
	case strings.Contains(m, "connect"):
		return StageConnectDB
	case strings.Contains(m, "tunnel"):
		return StageTunnel
	case strings.Contains(m, "auth") || strings.Contains(m, "credential") || strings.Contains(m, "sso"):
		return StageAuth
	default:
		return StageConnectDB
	}
}
