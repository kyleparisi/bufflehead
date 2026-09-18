package ui

import (
	"fmt"
	"strconv"
	"strings"

	"bufflehead/internal/models"

	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Vector2"
)

// sshSection is the "connect through an SSH tunnel" block of a connection form.
// Both the Postgres and MySQL forms embed one — the tunnel is orthogonal to the
// engine, so the fields and their projection live here once rather than being
// copied into each form.
//
// Following the screen's state-machine convention, the toggles below mutate
// only the plain fields (enabled, method); render() is the single projection of
// those onto node visibility and button themes.
type sshSection struct {
	// ── Authoritative state ──
	enabled bool
	method  models.SSHAuthMethod

	// ── Nodes ──
	body       VBoxContainer.Instance // fields, hidden when disabled
	enableBtn  Button.Instance
	disableBtn Button.Instance

	host    LineEdit.Instance
	port    LineEdit.Instance
	user    LineEdit.Instance
	keyPath LineEdit.Instance
	secret  LineEdit.Instance

	agentBtn Button.Instance
	keyBtn   Button.Instance
	passBtn  Button.Instance

	keyRow    VBoxContainer.Instance // key-path field, only for key auth
	secretRow VBoxContainer.Instance // passphrase/password, hidden for agent auth
	secretLbl Label.Instance
}

// buildSSHSection appends the SSH tunnel block to a connection form and returns
// the section so the form's Connect handler can read it.
func (g *GatewayScreen) buildSSHSection(parent VBoxContainer.Instance) *sshSection {
	s := &sshSection{enabled: g.initialSSH}

	header := Label.New()
	header.SetText("SSH Tunnel")
	header.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	header.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	parent.AsNode().AddChild(header.AsNode())

	// Off/On toggle, styled like the form's other two-button choices.
	toggleRow := HBoxContainer.New()
	toggleRow.AsNode().SetName("SSHTunnelToggle")
	toggleRow.AsControl().AddThemeConstantOverride("separation", 4)
	s.disableBtn = makeToggleButton("Direct", scaled(100))
	s.enableBtn = makeToggleButton("Via SSH", scaled(100))
	s.disableBtn.AsBaseButton().OnPressed(func() {
		s.enabled = false
		s.render()
	})
	s.enableBtn.AsBaseButton().OnPressed(func() {
		s.enabled = true
		s.render()
	})
	toggleRow.AsNode().AddChild(s.disableBtn.AsNode())
	toggleRow.AsNode().AddChild(s.enableBtn.AsNode())
	parent.AsNode().AddChild(toggleRow.AsNode())

	// ── Body: everything below is visible only when the tunnel is on. ──
	s.body = VBoxContainer.New()
	// Stable name so integration tests can assert the section renders (and, via
	// visibility, that the toggle projects onto it).
	s.body.AsNode().SetName("SSHTunnelFields")
	s.body.AsControl().AddThemeConstantOverride("separation", 8)

	s.host, s.port = g.makeFieldPair(s.body, "SSH Host", "bastion.example.com", 3, "SSH Port", "22", 1)
	s.user = g.makeField(s.body, "SSH User", "defaults to your OS username")

	authLbl := Label.New()
	authLbl.SetText("SSH Authentication")
	authLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	authLbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	s.body.AsNode().AddChild(authLbl.AsNode())

	authRow := HBoxContainer.New()
	authRow.AsControl().AddThemeConstantOverride("separation", 4)
	s.agentBtn = makeToggleButton("SSH Agent", scaled(100))
	s.keyBtn = makeToggleButton("Key File", scaled(100))
	s.passBtn = makeToggleButton("Password", scaled(100))
	s.agentBtn.AsBaseButton().OnPressed(func() {
		s.method = models.SSHAuthAgent
		s.render()
	})
	s.keyBtn.AsBaseButton().OnPressed(func() {
		s.method = models.SSHAuthKey
		s.render()
	})
	s.passBtn.AsBaseButton().OnPressed(func() {
		s.method = models.SSHAuthPassword
		s.render()
	})
	authRow.AsNode().AddChild(s.agentBtn.AsNode())
	authRow.AsNode().AddChild(s.keyBtn.AsNode())
	authRow.AsNode().AddChild(s.passBtn.AsNode())
	s.body.AsNode().AddChild(authRow.AsNode())

	s.keyRow, s.keyPath = g.makeFieldCol("Private Key File", "~/.ssh/id_ed25519")
	s.body.AsNode().AddChild(s.keyRow.AsNode())

	// Built inline rather than via makeFieldCol because the label changes with
	// the auth method ("Key Passphrase" vs "SSH Password").
	s.secretRow = VBoxContainer.New()
	s.secretRow.AsControl().AddThemeConstantOverride("separation", 2)
	s.secretLbl = Label.New()
	s.secretLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	s.secretLbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	s.secret = LineEdit.New()
	s.secret.SetPlaceholderText("stored securely in your OS keychain")
	applyInputTheme(s.secret.AsControl())
	s.secret.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	s.secret.SetSecretCharacter("●")
	s.secret.SetSecret(true)
	s.secretRow.AsNode().AddChild(s.secretLbl.AsNode())
	s.secretRow.AsNode().AddChild(s.secret.AsNode())
	s.body.AsNode().AddChild(s.secretRow.AsNode())

	helper := Label.New()
	helper.SetText("Forwards a local port to the database through the SSH host, like `ssh -L`. " +
		"The host key must already be in your ~/.ssh/known_hosts — connect once with `ssh` first if it isn't.")
	helper.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	helper.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	helper.SetAutowrapMode(3)
	s.body.AsNode().AddChild(helper.AsNode())

	parent.AsNode().AddChild(s.body.AsNode())

	s.render()
	return s
}

// makeToggleButton builds one button of a mutually-exclusive choice row. The
// active/inactive theme is applied by the owning section's render().
func makeToggleButton(text string, minWidth float32) Button.Instance {
	btn := Button.New()
	btn.SetText(text)
	btn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	btn.AsControl().SetCustomMinimumSize(Vector2.New(minWidth, scaled(28)))
	return btn
}

// applyToggleTheme highlights the selected button of a choice row.
func applyToggleTheme(btn Button.Instance, active bool) {
	if btn == (Button.Instance{}) {
		return
	}
	if active {
		applyButtonTheme(btn.AsControl())
	} else {
		applySecondaryButtonTheme(btn.AsControl())
	}
}

// render projects enabled + method onto the nodes. Idempotent, and it must not
// mutate state.
func (s *sshSection) render() {
	applyToggleTheme(s.disableBtn, !s.enabled)
	applyToggleTheme(s.enableBtn, s.enabled)
	s.body.AsCanvasItem().SetVisible(s.enabled)

	applyToggleTheme(s.agentBtn, s.method == models.SSHAuthAgent)
	applyToggleTheme(s.keyBtn, s.method == models.SSHAuthKey)
	applyToggleTheme(s.passBtn, s.method == models.SSHAuthPassword)

	// The agent holds its own keys, so neither a key path nor a secret applies.
	s.keyRow.AsCanvasItem().SetVisible(s.method == models.SSHAuthKey)
	s.secretRow.AsCanvasItem().SetVisible(s.method != models.SSHAuthAgent)
	if s.method == models.SSHAuthPassword {
		s.secretLbl.SetText("SSH Password")
	} else {
		s.secretLbl.SetText("Key Passphrase")
	}
}

// config reads the form fields into an SSHTunnel, or returns nil when the
// tunnel is switched off. The returned tunnel carries the typed secret in
// memory; persistSecret decides where (if anywhere) it is stored.
func (s *sshSection) config() (*models.SSHTunnel, error) {
	if !s.enabled {
		return nil, nil
	}

	host := strings.TrimSpace(s.host.Text())
	if host == "" {
		return nil, fmt.Errorf("SSH host is required when connecting via SSH")
	}

	port := 0
	if txt := strings.TrimSpace(s.port.Text()); txt != "" {
		p, err := strconv.Atoi(txt)
		if err != nil {
			return nil, fmt.Errorf("SSH port %q is not a number", txt)
		}
		port = p
	}

	tunnel := &models.SSHTunnel{
		Host:       host,
		Port:       port,
		User:       strings.TrimSpace(s.user.Text()),
		AuthMethod: s.method,
		KeyPath:    strings.TrimSpace(s.keyPath.Text()),
	}
	if s.method != models.SSHAuthAgent {
		tunnel.Secret = s.secret.Text()
	}
	if err := tunnel.Validate(); err != nil {
		return nil, err
	}
	if s.method == models.SSHAuthPassword && tunnel.Secret == "" {
		return nil, fmt.Errorf("SSH password is required for password authentication")
	}
	return tunnel, nil
}

// persistSecret stores the SSH passphrase/password in the OS keychain so the
// saved bookmark can reconnect without retyping it, and records on the tunnel
// where it went. It reports a non-fatal warning when the keychain is
// unavailable (headless Linux, say) — the connection still works this session.
func persistSSHSecret(tunnel *models.SSHTunnel, label string) string {
	if tunnel == nil || tunnel.Secret == "" {
		return ""
	}
	if err := models.SetSecret(models.SSHSecretLabel(label), tunnel.Secret); err != nil {
		return "Note: OS keychain unavailable — the SSH passphrase won't be saved for next time."
	}
	tunnel.SecretKind = models.SecretKeychain
	return ""
}

// describe renders the configured tunnel for the form's "Test" line.
func (s *sshSection) describe() string {
	if !s.enabled {
		return ""
	}
	tunnel, err := s.config()
	if err != nil || tunnel == nil {
		return ""
	}
	return " via ssh " + tunnel.Describe()
}
