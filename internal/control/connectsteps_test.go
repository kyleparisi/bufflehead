package control

import "testing"

func TestConnectStageFromMessage(t *testing.T) {
	cases := map[string]ConnectStage{
		"Connecting to database...":     StageConnectDB,
		"Loading tables...":             StageLoadSchema,
		"Loading schema for 12 tables...": StageLoadSchema,
		"Starting SSM tunnel":           StageTunnel,
		"Refreshing AWS credentials":    StageAuth,
		"Configuring SSO session...":    StageAuth,
		"":                              StageConnectDB, // default once auth+tunnel done
		"something unexpected":          StageConnectDB,
	}
	for msg, want := range cases {
		if got := ConnectStageFromMessage(msg); got != want {
			t.Errorf("ConnectStageFromMessage(%q) = %d, want %d", msg, got, want)
		}
	}
}

func TestConnectStageLabel(t *testing.T) {
	if got := ConnectStageLabel(StageLoadSchema); got != "Load schema" {
		t.Errorf("label(StageLoadSchema) = %q", got)
	}
	if got := ConnectStageLabel(ConnectStage(99)); got != "" {
		t.Errorf("out-of-range label = %q, want empty", got)
	}
	if len(ConnectStageLabels) != int(NumConnectStages) {
		t.Errorf("have %d labels for %d stages", len(ConnectStageLabels), NumConnectStages)
	}
}
