package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	bfaws "bufflehead/internal/aws"
	"bufflehead/internal/models"

	"graphics.gd/classdb/BoxContainer"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/OptionButton"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/ScrollContainer"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Color"
	"graphics.gd/variant/Float"
	"graphics.gd/variant/Vector2"
)

// Gateway status indicator colors.
var (
	colorStatusGreen  = Color.RGBA{R: 0.30, G: 0.80, B: 0.40, A: 1}
	colorStatusYellow = Color.RGBA{R: 0.90, G: 0.75, B: 0.20, A: 1}
	colorStatusRed    = Color.RGBA{R: 0.85, G: 0.30, B: 0.30, A: 1}
	colorStatusGray   = Color.RGBA{R: 0.50, G: 0.50, B: 0.50, A: 1}
)

// GatewayScreen is the connection screen for remote databases.
type GatewayScreen struct {
	VBoxContainer.Extension[GatewayScreen] `gd:"GatewayScreen"`

	gateways []models.GatewayEntry
	cards    []*gatewayCard

	OnConnect    func(entry models.GatewayEntry, auth *bfaws.AuthManager, tunnel *bfaws.TunnelManager)
	OnOpenLocal  func()
	OnEditConfig func()

	// SSO login section
	ssoStartURL  LineEdit.Instance
	ssoRegion    LineEdit.Instance
	ssoLoginBtn  Button.Instance
	ssoStatus    Label.Instance
	ssoLog       Label.Instance
	ssoPickerBox VBoxContainer.Instance // container for account/role buttons
	ssoUpdate    bool
	ssoLoginLog  string
	ssoLoginErr  string
	ssoDone      bool

	// Account/role picker state (set by background goroutines, read by Process)
	ssoAccounts     []bfaws.SSOAccount
	ssoAccountsErr  string
	ssoAccountsReady bool
	ssoRoles        []bfaws.SSORole
	ssoRolesErr     string
	ssoRolesReady   bool
	ssoPickedAcct   bfaws.SSOAccount
	ssoProfileDone  bool
	ssoProfileErr   string
	ssoProfileName  string

	// Connection form fields
	formProfile      LineEdit.Instance
	formRegion       LineEdit.Instance
	formInstance     OptionButton.Instance
	formInstanceBtn  Button.Instance
	formInstanceIDs  []string // instance IDs corresponding to dropdown items
	instancesLoading bool
	instancesReady   bool
	instancesErr     string
	instancesResult  []bfaws.SSMInstance
	formRDS          OptionButton.Instance
	formRDSBtn       Button.Instance
	formRDSData      []bfaws.RDSInstance // RDS instances for dropdown
	rdsLoading       bool
	rdsReady         bool
	rdsErr           string
	rdsResult        []bfaws.RDSInstance
	formRDSHost      LineEdit.Instance
	formRDSPort   LineEdit.Instance
	formLocalPort LineEdit.Instance
	formDBName    LineEdit.Instance
	formDBUser    LineEdit.Instance
	formDBPass    LineEdit.Instance
	formPassVBox  VBoxContainer.Instance // container for password field, hidden in IAM mode
	formStatus    Label.Instance
	formIAMAuth   bool // true = IAM auth, false = password
	formPassBtn   Button.Instance
	formIAMBtn    Button.Instance
}

type gatewayCard struct {
	entry       models.GatewayEntry
	auth        *bfaws.AuthManager
	tunnel      *bfaws.TunnelManager
	statusLbl   Label.Instance
	logLbl      Label.Instance
	actionBtn   Button.Instance
	statusDot   Label.Instance
	credStatus  bfaws.CredentialStatus
	needsUpdate bool
	loginErr    string
	loginLog    string
	connected   bool
	fromForm    bool // true if created from inline form (no statusDot/logLbl/actionBtn)
}

func (g *GatewayScreen) SetGateways(entries []models.GatewayEntry) {
	g.gateways = entries
}

func (g *GatewayScreen) Ready() {
	g.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	g.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	g.AsBoxContainer().SetAlignment(BoxContainer.AlignmentCenter)

	scroll := ScrollContainer.New()
	scroll.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	scroll.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)

	center := VBoxContainer.New()
	center.AsControl().SetSizeFlagsHorizontal(Control.SizeShrinkCenter)
	center.AsControl().AddThemeConstantOverride("separation", 16)
	center.AsControl().SetCustomMinimumSize(Vector2.New(scaled(480), 0))

	// Title
	title := Label.New()
	title.SetText("Connect to Gateway")
	title.AsControl().AddThemeFontSizeOverride("font_size", fontSize(20))
	title.AsControl().AddThemeColorOverride("font_color", colorText)
	title.SetHorizontalAlignment(1)
	center.AsNode().AddChild(title.AsNode())

	// ── SSO Login section ──
	ssoPanel := g.buildSSOPanel()
	center.AsNode().AddChild(ssoPanel.AsNode())

	// ── Saved gateway cards ──
	for i, entry := range g.gateways {
		cardPanel := g.buildCardPanel(entry, i)
		center.AsNode().AddChild(cardPanel.AsNode())
	}

	// ── New connection form ──
	formPanel := g.buildForm()
	center.AsNode().AddChild(formPanel.AsNode())

	// Spacer
	spacer := Control.New()
	spacer.SetCustomMinimumSize(Vector2.New(0, scaled(4)))
	center.AsNode().AddChild(spacer.AsNode())

	// Open Local File button
	localBtn := Button.New()
	localBtn.SetText("Open Local File...")
	localBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	applySecondaryButtonTheme(localBtn.AsControl())
	localBtn.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(36)))
	localBtn.AsBaseButton().OnPressed(func() {
		if g.OnOpenLocal != nil {
			g.OnOpenLocal()
		}
	})
	center.AsNode().AddChild(localBtn.AsNode())

	// Edit config link
	editBtn := Button.New()
	editBtn.SetText("Edit Gateway Config")
	editBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	editBtn.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	editBtn.AsControl().AddThemeColorOverride("font_hover_color", colorAccent)
	transparent := Color.RGBA{R: 0, G: 0, B: 0, A: 0}
	flatBg := makeStyleBox(transparent, 0, 0, transparent)
	editBtn.AsControl().AddThemeStyleboxOverride("normal", flatBg.AsStyleBox())
	editBtn.AsControl().AddThemeStyleboxOverride("hover", flatBg.AsStyleBox())
	editBtn.AsControl().AddThemeStyleboxOverride("pressed", flatBg.AsStyleBox())
	editBtn.AsBaseButton().OnPressed(func() {
		if g.OnEditConfig != nil {
			g.OnEditConfig()
		}
	})
	center.AsNode().AddChild(editBtn.AsNode())

	scroll.AsNode().AddChild(center.AsNode())
	g.AsNode().AddChild(scroll.AsNode())
}

// buildSSOPanel creates the SSO login section — start URL + region + button.
func (g *GatewayScreen) buildSSOPanel() PanelContainer.Instance {
	panel := PanelContainer.New()
	border := makeStyleBox(colorBgPanel, 6, 1, colorBorderDim)
	border.AsStyleBox().SetContentMarginAll(scaled(16))
	panel.AsControl().AddThemeStyleboxOverride("panel", border.AsStyleBox())

	vbox := VBoxContainer.New()
	vbox.AsControl().AddThemeConstantOverride("separation", 8)

	// Header
	header := Label.New()
	header.SetText("AWS SSO Login")
	header.AsControl().AddThemeFontSizeOverride("font_size", fontSize(14))
	header.AsControl().AddThemeColorOverride("font_color", colorText)
	vbox.AsNode().AddChild(header.AsNode())

	// Start URL field
	urlVBox := VBoxContainer.New()
	urlVBox.AsControl().AddThemeConstantOverride("separation", 2)
	urlLbl := Label.New()
	urlLbl.SetText("SSO Start URL")
	urlLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	urlLbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	g.ssoStartURL = LineEdit.New()
	g.ssoStartURL.SetPlaceholderText("https://your-org.awsapps.com/start")
	applyInputTheme(g.ssoStartURL.AsControl())
	g.ssoStartURL.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	urlVBox.AsNode().AddChild(urlLbl.AsNode())
	urlVBox.AsNode().AddChild(g.ssoStartURL.AsNode())
	vbox.AsNode().AddChild(urlVBox.AsNode())

	// Region + button row
	row := HBoxContainer.New()
	row.AsControl().AddThemeConstantOverride("separation", 8)

	regionVBox := VBoxContainer.New()
	regionVBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	regionVBox.AsControl().AddThemeConstantOverride("separation", 2)
	regionLbl := Label.New()
	regionLbl.SetText("SSO Region")
	regionLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	regionLbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	g.ssoRegion = LineEdit.New()
	g.ssoRegion.SetPlaceholderText("us-east-1")
	g.ssoRegion.SetText("us-east-1")
	applyInputTheme(g.ssoRegion.AsControl())
	g.ssoRegion.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	regionVBox.AsNode().AddChild(regionLbl.AsNode())
	regionVBox.AsNode().AddChild(g.ssoRegion.AsNode())
	row.AsNode().AddChild(regionVBox.AsNode())

	g.ssoLoginBtn = Button.New()
	g.ssoLoginBtn.SetText("Login")
	g.ssoLoginBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	applyButtonTheme(g.ssoLoginBtn.AsControl())
	g.ssoLoginBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(80), 0))
	g.ssoLoginBtn.AsControl().SetSizeFlagsVertical(Control.SizeShrinkEnd)
	g.ssoLoginBtn.AsBaseButton().OnPressed(func() {
		g.onSSOLogin()
	})
	row.AsNode().AddChild(g.ssoLoginBtn.AsNode())

	vbox.AsNode().AddChild(row.AsNode())

	// Status label
	g.ssoStatus = Label.New()
	g.ssoStatus.SetText("")
	g.ssoStatus.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	vbox.AsNode().AddChild(g.ssoStatus.AsNode())

	// Log output (hidden until login starts)
	g.ssoLog = Label.New()
	g.ssoLog.SetText("")
	g.ssoLog.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	g.ssoLog.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	g.ssoLog.SetAutowrapMode(3)
	g.ssoLog.AsCanvasItem().SetVisible(false)
	vbox.AsNode().AddChild(g.ssoLog.AsNode())

	// Account/role picker container (populated after login)
	g.ssoPickerBox = VBoxContainer.New()
	g.ssoPickerBox.AsControl().AddThemeConstantOverride("separation", 4)
	g.ssoPickerBox.AsCanvasItem().SetVisible(false)
	vbox.AsNode().AddChild(g.ssoPickerBox.AsNode())

	panel.AsNode().AddChild(vbox.AsNode())
	return panel
}

func (g *GatewayScreen) onSSOLogin() {
	startURL := g.ssoStartURL.Text()
	region := g.ssoRegion.Text()
	if startURL == "" {
		g.ssoStatus.SetText("Enter your SSO start URL")
		g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}
	if region == "" {
		region = "us-east-1"
	}

	g.ssoLoginBtn.AsBaseButton().SetDisabled(true)
	g.ssoStatus.SetText("Configuring SSO session...")
	g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)
	g.ssoLog.SetText("")
	g.ssoLog.AsCanvasItem().SetVisible(false)
	g.ssoLoginLog = ""
	g.ssoLoginErr = ""
	g.ssoDone = false

	go func() {
		// Ensure sso-session block exists in ~/.aws/config
		sessionName, err := bfaws.EnsureSSOSession(startURL, region)
		if err != nil {
			g.ssoLoginErr = err.Error()
			g.ssoUpdate = true
			return
		}

		g.ssoLoginLog = "Opening browser for SSO login..."
		g.ssoUpdate = true

		ch := bfaws.SSOSessionLogin(sessionName)
		for result := range ch {
			if result.Err != nil {
				g.ssoLoginErr = result.Err.Error()
				g.ssoUpdate = true
				return
			}
			if result.Line != "" {
				if g.ssoLoginLog != "" {
					g.ssoLoginLog += "\n"
				}
				g.ssoLoginLog += result.Line
				g.ssoUpdate = true
			}
		}

		// Login succeeded — now fetch accounts
		g.ssoDone = true
		g.ssoUpdate = true

		token, tokenRegion, err := bfaws.ReadCachedAccessToken(startURL)
		if err != nil {
			g.ssoAccountsErr = err.Error()
			g.ssoAccountsReady = true
			g.ssoUpdate = true
			return
		}

		accounts, err := bfaws.ListSSOAccounts(token, tokenRegion)
		if err != nil {
			g.ssoAccountsErr = err.Error()
			g.ssoAccountsReady = true
			g.ssoUpdate = true
			return
		}

		g.ssoAccounts = accounts
		g.ssoAccountsReady = true
		g.ssoUpdate = true
	}()
}

func (g *GatewayScreen) clearPickerBox() {
	for g.ssoPickerBox.AsNode().GetChildCount() > 0 {
		child := g.ssoPickerBox.AsNode().GetChild(0)
		g.ssoPickerBox.AsNode().RemoveChild(child)
		child.QueueFree()
	}
}

func (g *GatewayScreen) showAccountPicker() {
	g.clearPickerBox()

	header := Label.New()
	header.SetText("Select an account:")
	header.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	header.AsControl().AddThemeColorOverride("font_color", colorText)
	g.ssoPickerBox.AsNode().AddChild(header.AsNode())

	for _, acct := range g.ssoAccounts {
		acct := acct // capture
		btn := Button.New()
		label := acct.AccountName
		if label == "" {
			label = acct.AccountID
		} else {
			label = fmt.Sprintf("%s (%s)", acct.AccountName, acct.AccountID)
		}
		btn.SetText(label)
		btn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
		applySecondaryButtonTheme(btn.AsControl())
		btn.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(30)))
		btn.AsBaseButton().OnPressed(func() {
			g.onPickAccount(acct)
		})
		g.ssoPickerBox.AsNode().AddChild(btn.AsNode())
	}

	g.ssoPickerBox.AsCanvasItem().SetVisible(true)
}

func (g *GatewayScreen) onPickAccount(acct bfaws.SSOAccount) {
	g.ssoPickedAcct = acct
	g.ssoStatus.SetText("Loading roles for " + acct.AccountName + "...")
	g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)
	g.clearPickerBox()

	startURL := g.ssoStartURL.Text() // capture on main thread
	go func() {
		token, tokenRegion, err := bfaws.ReadCachedAccessToken(startURL)
		if err != nil {
			g.ssoRolesErr = err.Error()
			g.ssoRolesReady = true
			g.ssoUpdate = true
			return
		}

		roles, err := bfaws.ListSSORoles(token, tokenRegion, acct.AccountID)
		if err != nil {
			g.ssoRolesErr = err.Error()
			g.ssoRolesReady = true
			g.ssoUpdate = true
			return
		}

		g.ssoRoles = roles
		g.ssoRolesReady = true
		g.ssoUpdate = true
	}()
}

func (g *GatewayScreen) showRolePicker() {
	g.clearPickerBox()

	header := Label.New()
	header.SetText(fmt.Sprintf("Select a role for %s:", g.ssoPickedAcct.AccountName))
	header.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	header.AsControl().AddThemeColorOverride("font_color", colorText)
	g.ssoPickerBox.AsNode().AddChild(header.AsNode())

	for _, role := range g.ssoRoles {
		role := role // capture
		btn := Button.New()
		btn.SetText(role.RoleName)
		btn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
		applySecondaryButtonTheme(btn.AsControl())
		btn.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(30)))
		btn.AsBaseButton().OnPressed(func() {
			g.onPickRole(role)
		})
		g.ssoPickerBox.AsNode().AddChild(btn.AsNode())
	}

	// Back button
	backBtn := Button.New()
	backBtn.SetText("← Back to accounts")
	backBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	backBtn.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	transparent := Color.RGBA{R: 0, G: 0, B: 0, A: 0}
	flatBg := makeStyleBox(transparent, 0, 0, transparent)
	backBtn.AsControl().AddThemeStyleboxOverride("normal", flatBg.AsStyleBox())
	backBtn.AsControl().AddThemeStyleboxOverride("hover", flatBg.AsStyleBox())
	backBtn.AsControl().AddThemeStyleboxOverride("pressed", flatBg.AsStyleBox())
	backBtn.AsBaseButton().OnPressed(func() {
		g.showAccountPicker()
		g.ssoStatus.SetText("Logged in — select an account")
		g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
	})
	g.ssoPickerBox.AsNode().AddChild(backBtn.AsNode())

	g.ssoPickerBox.AsCanvasItem().SetVisible(true)
}

func (g *GatewayScreen) onPickRole(role bfaws.SSORole) {
	acct := g.ssoPickedAcct
	region := g.ssoRegion.Text()
	if region == "" {
		region = "us-east-1"
	}

	// Build a profile name: accountName-roleName (sanitized)
	sanitize := func(s string) string {
		s = strings.ToLower(s)
		s = strings.ReplaceAll(s, " ", "-")
		return s
	}
	profileName := fmt.Sprintf("bf-%s-%s", sanitize(acct.AccountName), sanitize(role.RoleName))

	g.ssoStatus.SetText("Saving profile...")
	g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)
	g.clearPickerBox()
	g.ssoPickerBox.AsCanvasItem().SetVisible(false)

	go func() {
		err := bfaws.WriteProfile(profileName, acct.AccountID, role.RoleName, region)
		if err != nil {
			g.ssoProfileErr = err.Error()
		} else {
			g.ssoProfileName = profileName
			g.ssoProfileDone = true
		}
		g.ssoUpdate = true
	}()
}

func (g *GatewayScreen) buildForm() PanelContainer.Instance {
	panel := PanelContainer.New()
	border := makeStyleBox(colorBgPanel, 6, 1, colorBorderDim)
	border.AsStyleBox().SetContentMarginAll(scaled(16))
	panel.AsControl().AddThemeStyleboxOverride("panel", border.AsStyleBox())

	vbox := VBoxContainer.New()
	vbox.AsControl().AddThemeConstantOverride("separation", 8)

	// Section header
	header := Label.New()
	header.SetText("Connect to Database")
	header.AsControl().AddThemeFontSizeOverride("font_size", fontSize(14))
	header.AsControl().AddThemeColorOverride("font_color", colorText)
	vbox.AsNode().AddChild(header.AsNode())

	// AWS section
	awsLabel := Label.New()
	awsLabel.SetText("AWS")
	awsLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	awsLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	vbox.AsNode().AddChild(awsLabel.AsNode())

	g.formProfile = g.makeField(vbox, "AWS Profile", "e.g. my-sso-profile")
	g.formRegion = g.makeField(vbox, "AWS Region", "e.g. us-east-1")

	// Bastion instance dropdown + refresh button
	instanceVBox := VBoxContainer.New()
	instanceVBox.AsControl().AddThemeConstantOverride("separation", 2)
	instanceLbl := Label.New()
	instanceLbl.SetText("Bastion Instance")
	instanceLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	instanceLbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	instanceVBox.AsNode().AddChild(instanceLbl.AsNode())

	instanceRow := HBoxContainer.New()
	instanceRow.AsControl().AddThemeConstantOverride("separation", 4)

	g.formInstance = OptionButton.New()
	g.formInstance.AddItem("Select an instance...")
	g.formInstance.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applyInputTheme(g.formInstance.AsControl())
	g.formInstance.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	instanceRow.AsNode().AddChild(g.formInstance.AsNode())

	g.formInstanceBtn = Button.New()
	g.formInstanceBtn.SetText("Load")
	g.formInstanceBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	applySecondaryButtonTheme(g.formInstanceBtn.AsControl())
	g.formInstanceBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(60), 0))
	g.formInstanceBtn.AsBaseButton().OnPressed(func() {
		g.onLoadInstances()
	})
	instanceRow.AsNode().AddChild(g.formInstanceBtn.AsNode())

	instanceVBox.AsNode().AddChild(instanceRow.AsNode())
	vbox.AsNode().AddChild(instanceVBox.AsNode())

	// RDS section
	rdsLabel := Label.New()
	rdsLabel.SetText("RDS / Database")
	rdsLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	rdsLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	vbox.AsNode().AddChild(rdsLabel.AsNode())

	// RDS instance dropdown + load button
	rdsPickerVBox := VBoxContainer.New()
	rdsPickerVBox.AsControl().AddThemeConstantOverride("separation", 2)
	rdsPickerLbl := Label.New()
	rdsPickerLbl.SetText("RDS Instance")
	rdsPickerLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	rdsPickerLbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	rdsPickerVBox.AsNode().AddChild(rdsPickerLbl.AsNode())

	rdsPickerRow := HBoxContainer.New()
	rdsPickerRow.AsControl().AddThemeConstantOverride("separation", 4)

	g.formRDS = OptionButton.New()
	g.formRDS.AddItem("Select an RDS instance...")
	g.formRDS.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applyInputTheme(g.formRDS.AsControl())
	g.formRDS.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	g.formRDS.OnItemSelected(func(index int) {
		g.onRDSSelected(index)
	})
	rdsPickerRow.AsNode().AddChild(g.formRDS.AsNode())

	g.formRDSBtn = Button.New()
	g.formRDSBtn.SetText("Load")
	g.formRDSBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	applySecondaryButtonTheme(g.formRDSBtn.AsControl())
	g.formRDSBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(60), 0))
	g.formRDSBtn.AsBaseButton().OnPressed(func() {
		g.onLoadRDS()
	})
	rdsPickerRow.AsNode().AddChild(g.formRDSBtn.AsNode())

	rdsPickerVBox.AsNode().AddChild(rdsPickerRow.AsNode())
	vbox.AsNode().AddChild(rdsPickerVBox.AsNode())

	g.formRDSHost = g.makeField(vbox, "RDS Host", "e.g. my-db.cluster-xyz.rds.amazonaws.com")

	// Port row (two fields side by side)
	portRow := HBoxContainer.New()
	portRow.AsControl().AddThemeConstantOverride("separation", 8)

	rdsPortVBox := VBoxContainer.New()
	rdsPortVBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	rdsPortVBox.AsControl().AddThemeConstantOverride("separation", 2)
	rdsPortLabel := Label.New()
	rdsPortLabel.SetText("RDS Port")
	rdsPortLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	rdsPortLabel.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	g.formRDSPort = LineEdit.New()
	g.formRDSPort.SetPlaceholderText("5432")
	g.formRDSPort.SetText("5432")
	applyInputTheme(g.formRDSPort.AsControl())
	g.formRDSPort.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	rdsPortVBox.AsNode().AddChild(rdsPortLabel.AsNode())
	rdsPortVBox.AsNode().AddChild(g.formRDSPort.AsNode())

	localPortVBox := VBoxContainer.New()
	localPortVBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	localPortVBox.AsControl().AddThemeConstantOverride("separation", 2)
	localPortLabel := Label.New()
	localPortLabel.SetText("Local Port")
	localPortLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	localPortLabel.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	g.formLocalPort = LineEdit.New()
	g.formLocalPort.SetPlaceholderText("5433")
	g.formLocalPort.SetText("5433")
	applyInputTheme(g.formLocalPort.AsControl())
	g.formLocalPort.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	localPortVBox.AsNode().AddChild(localPortLabel.AsNode())
	localPortVBox.AsNode().AddChild(g.formLocalPort.AsNode())

	portRow.AsNode().AddChild(rdsPortVBox.AsNode())
	portRow.AsNode().AddChild(localPortVBox.AsNode())
	vbox.AsNode().AddChild(portRow.AsNode())

	g.formDBName = g.makeField(vbox, "Database Name", "e.g. mydb")
	g.formDBUser = g.makeField(vbox, "Username", "e.g. readonly_user")

	// Auth mode toggle
	authLabel := Label.New()
	authLabel.SetText("Authentication")
	authLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	authLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	vbox.AsNode().AddChild(authLabel.AsNode())

	authRow := HBoxContainer.New()
	authRow.AsControl().AddThemeConstantOverride("separation", 4)

	g.formPassBtn = Button.New()
	g.formPassBtn.SetText("Password")
	g.formPassBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	applyButtonTheme(g.formPassBtn.AsControl())
	g.formPassBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(100), scaled(28)))

	g.formIAMBtn = Button.New()
	g.formIAMBtn.SetText("IAM Auth")
	g.formIAMBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	applySecondaryButtonTheme(g.formIAMBtn.AsControl())
	g.formIAMBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(100), scaled(28)))

	g.formPassBtn.AsBaseButton().OnPressed(func() {
		g.formIAMAuth = false
		applyButtonTheme(g.formPassBtn.AsControl())
		applySecondaryButtonTheme(g.formIAMBtn.AsControl())
		g.formPassVBox.AsCanvasItem().SetVisible(true)
	})
	g.formIAMBtn.AsBaseButton().OnPressed(func() {
		g.formIAMAuth = true
		applyButtonTheme(g.formIAMBtn.AsControl())
		applySecondaryButtonTheme(g.formPassBtn.AsControl())
		g.formPassVBox.AsCanvasItem().SetVisible(false)
	})

	authRow.AsNode().AddChild(g.formPassBtn.AsNode())
	authRow.AsNode().AddChild(g.formIAMBtn.AsNode())
	vbox.AsNode().AddChild(authRow.AsNode())

	// Password field (hidden when IAM auth is selected)
	g.formPassVBox = VBoxContainer.New()
	g.formPassVBox.AsControl().AddThemeConstantOverride("separation", 2)
	passLbl := Label.New()
	passLbl.SetText("Password")
	passLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	passLbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	g.formDBPass = LineEdit.New()
	g.formDBPass.SetPlaceholderText("leave empty if using env var")
	applyInputTheme(g.formDBPass.AsControl())
	g.formDBPass.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	g.formDBPass.SetSecretCharacter("●")
	g.formDBPass.SetSecret(true)
	g.formPassVBox.AsNode().AddChild(passLbl.AsNode())
	g.formPassVBox.AsNode().AddChild(g.formDBPass.AsNode())
	vbox.AsNode().AddChild(g.formPassVBox.AsNode())

	// Status label
	g.formStatus = Label.New()
	g.formStatus.SetText("")
	g.formStatus.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	g.formStatus.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	g.formStatus.SetAutowrapMode(3)
	vbox.AsNode().AddChild(g.formStatus.AsNode())

	// Connect button
	connectBtn := Button.New()
	connectBtn.SetText("Connect")
	connectBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	applyButtonTheme(connectBtn.AsControl())
	connectBtn.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(34)))
	connectBtn.AsBaseButton().OnPressed(func() {
		g.onFormConnect()
	})
	vbox.AsNode().AddChild(connectBtn.AsNode())

	panel.AsNode().AddChild(vbox.AsNode())
	return panel
}

func (g *GatewayScreen) makeField(parent VBoxContainer.Instance, label, placeholder string) LineEdit.Instance {
	fieldVBox := VBoxContainer.New()
	fieldVBox.AsControl().AddThemeConstantOverride("separation", 2)

	lbl := Label.New()
	lbl.SetText(label)
	lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	lbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)

	input := LineEdit.New()
	input.SetPlaceholderText(placeholder)
	applyInputTheme(input.AsControl())
	input.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))

	fieldVBox.AsNode().AddChild(lbl.AsNode())
	fieldVBox.AsNode().AddChild(input.AsNode())
	parent.AsNode().AddChild(fieldVBox.AsNode())
	return input
}

func (g *GatewayScreen) onLoadInstances() {
	profile := g.formProfile.Text()
	region := g.formRegion.Text()
	if profile == "" {
		g.formStatus.SetText("Enter AWS Profile first")
		g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}
	if region == "" {
		g.formStatus.SetText("Enter AWS Region first")
		g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}

	g.instancesLoading = true
	g.formInstanceBtn.AsBaseButton().SetDisabled(true)
	g.formInstanceBtn.SetText("...")
	g.formStatus.SetText("Loading instances...")
	g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)

	auth := bfaws.NewAuthManager(profile, region)
	go func() {
		instances, err := auth.ListSSMInstances()
		if err != nil {
			g.instancesErr = err.Error()
		} else {
			g.instancesResult = instances
		}
		g.instancesReady = true
	}()
}

func (g *GatewayScreen) onLoadRDS() {
	profile := g.formProfile.Text()
	region := g.formRegion.Text()
	if profile == "" {
		g.formStatus.SetText("Enter AWS Profile first")
		g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}
	if region == "" {
		g.formStatus.SetText("Enter AWS Region first")
		g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}

	g.rdsLoading = true
	g.formRDSBtn.AsBaseButton().SetDisabled(true)
	g.formRDSBtn.SetText("...")
	g.formStatus.SetText("Loading RDS instances...")
	g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)

	auth := bfaws.NewAuthManager(profile, region)
	go func() {
		instances, err := auth.ListRDSInstances()
		if err != nil {
			g.rdsErr = err.Error()
		} else {
			g.rdsResult = instances
		}
		g.rdsReady = true
	}()
}

func (g *GatewayScreen) onRDSSelected(index int) {
	if index <= 0 || index > len(g.formRDSData) {
		return
	}
	rdsInst := g.formRDSData[index-1] // offset by 1 for placeholder
	g.formRDSHost.SetText(rdsInst.Endpoint)
	g.formRDSPort.SetText(strconv.Itoa(rdsInst.Port))
}

func (g *GatewayScreen) onFormConnect() {
	profile := g.formProfile.Text()
	region := g.formRegion.Text()
	// Get selected instance ID from the dropdown
	selectedIdx := g.formInstance.Selected()
	var instanceID string
	if selectedIdx > 0 && selectedIdx <= len(g.formInstanceIDs) {
		instanceID = g.formInstanceIDs[selectedIdx-1] // offset by 1 for placeholder item
	}
	rdsHost := g.formRDSHost.Text()
	rdsPortStr := g.formRDSPort.Text()
	localPortStr := g.formLocalPort.Text()
	dbName := g.formDBName.Text()
	dbUser := g.formDBUser.Text()
	dbPass := g.formDBPass.Text()

	// Validate required fields
	if profile == "" {
		g.formStatus.SetText("AWS Profile is required")
		g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}
	if region == "" {
		g.formStatus.SetText("AWS Region is required")
		g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}
	if instanceID == "" {
		g.formStatus.SetText("Bastion Instance ID is required")
		g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}
	if rdsHost == "" {
		g.formStatus.SetText("RDS Host is required")
		g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}
	if dbName == "" {
		g.formStatus.SetText("Database Name is required")
		g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}
	if dbUser == "" {
		g.formStatus.SetText("Username is required")
		g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
		return
	}

	rdsPort, _ := strconv.Atoi(rdsPortStr)
	if rdsPort == 0 {
		rdsPort = 5432
	}
	localPort, _ := strconv.Atoi(localPortStr)
	if localPort == 0 {
		localPort = 5433
	}

	authMode := "password"
	if g.formIAMAuth {
		authMode = "iam"
	}

	entry := models.GatewayEntry{
		Name:       fmt.Sprintf("%s/%s", dbName, dbUser),
		AWSProfile: profile,
		AWSRegion:  region,
		InstanceID: instanceID,
		RDSHost:    rdsHost,
		RDSPort:    rdsPort,
		LocalPort:  localPort,
		DBName:     dbName,
		DBUser:     dbUser,
		DBPassword: dbPass,
		AuthMode:   authMode,
	}

	card := &gatewayCard{
		entry:    entry,
		auth:     bfaws.NewAuthManager(profile, region),
		fromForm: true,
	}
	card.statusLbl = g.formStatus
	g.cards = append(g.cards, card)

	g.formStatus.SetText("Checking credentials...")
	g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)

	go func() {
		status := card.auth.Status()
		card.credStatus = status

		if status == bfaws.CredsValid {
			card.needsUpdate = true
			g.startGatewayConnection(card)
		} else {
			card.loginLog = ""
			card.needsUpdate = true

			ch := card.auth.SSOLogin()
			for result := range ch {
				if result.Err != nil {
					card.loginErr = result.Err.Error()
					card.needsUpdate = true
					return
				}
				if result.Line != "" {
					if card.loginLog != "" {
						card.loginLog += "\n"
					}
					card.loginLog += result.Line
					card.needsUpdate = true
				}
			}
			card.credStatus = card.auth.Status()
			if card.credStatus == bfaws.CredsValid {
				card.needsUpdate = true
				g.startGatewayConnection(card)
			} else {
				card.loginErr = "SSO login did not produce valid credentials"
				card.needsUpdate = true
			}
		}
	}()
}

func (g *GatewayScreen) buildCardPanel(entry models.GatewayEntry, idx int) PanelContainer.Instance {
	panel := PanelContainer.New()
	border := makeStyleBox(colorBgPanel, 6, 1, colorBorderDim)
	border.AsStyleBox().SetContentMarginAll(scaled(16))
	panel.AsControl().AddThemeStyleboxOverride("panel", border.AsStyleBox())

	vbox := VBoxContainer.New()
	vbox.AsControl().AddThemeConstantOverride("separation", 6)

	// Row 1: Status dot + Name
	row1 := HBoxContainer.New()
	row1.AsControl().AddThemeConstantOverride("separation", 8)

	statusDot := Label.New()
	statusDot.SetText("○")
	statusDot.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	statusDot.AsControl().AddThemeColorOverride("font_color", colorStatusGray)

	nameLabel := Label.New()
	nameLabel.SetText(entry.Name)
	nameLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(15))
	nameLabel.AsControl().AddThemeColorOverride("font_color", colorText)
	nameLabel.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)

	row1.AsNode().AddChild(statusDot.AsNode())
	row1.AsNode().AddChild(nameLabel.AsNode())

	// RDS host
	hostLabel := Label.New()
	host := entry.RDSHost
	if len(host) > 48 {
		host = host[:48] + "..."
	}
	hostLabel.SetText(host)
	hostLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	hostLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)

	// AWS profile
	profileLabel := Label.New()
	profileLabel.SetText("AWS Profile: " + entry.AWSProfile)
	profileLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	profileLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)

	// Status + action button row
	row4 := HBoxContainer.New()
	row4.AsControl().AddThemeConstantOverride("separation", 8)

	statusLabel := Label.New()
	statusLabel.SetText("Checking credentials...")
	statusLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	statusLabel.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	statusLabel.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)

	actionBtn := Button.New()
	actionBtn.SetText("Connect")
	actionBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	applyButtonTheme(actionBtn.AsControl())

	row4.AsNode().AddChild(statusLabel.AsNode())
	row4.AsNode().AddChild(actionBtn.AsNode())

	// Log area for SSO login output (hidden initially)
	logLabel := Label.New()
	logLabel.SetText("")
	logLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	logLabel.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	logLabel.SetAutowrapMode(3)
	logLabel.AsCanvasItem().SetVisible(false)

	vbox.AsNode().AddChild(row1.AsNode())
	vbox.AsNode().AddChild(hostLabel.AsNode())
	vbox.AsNode().AddChild(profileLabel.AsNode())
	vbox.AsNode().AddChild(row4.AsNode())
	vbox.AsNode().AddChild(logLabel.AsNode())
	panel.AsNode().AddChild(vbox.AsNode())

	card := &gatewayCard{
		entry:     entry,
		auth:      bfaws.NewAuthManager(entry.AWSProfile, entry.AWSRegion),
		statusLbl: statusLabel,
		logLbl:    logLabel,
		actionBtn: actionBtn,
		statusDot: statusDot,
	}
	g.cards = append(g.cards, card)

	// Check credentials async
	go func() {
		status := card.auth.Status()
		card.credStatus = status
		card.needsUpdate = true
	}()

	// Wire action button
	cardIdx := idx
	actionBtn.AsBaseButton().OnPressed(func() {
		g.onCardAction(cardIdx)
	})

	return panel
}

func (g *GatewayScreen) onCardAction(idx int) {
	if idx >= len(g.cards) {
		return
	}
	card := g.cards[idx]

	switch card.credStatus {
	case bfaws.CredsExpired, bfaws.CredsNoCredentials:
		card.statusLbl.SetText("Opening browser for SSO login...")
		card.statusDot.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)
		card.actionBtn.AsBaseButton().SetDisabled(true)
		card.loginLog = ""
		card.logLbl.SetText("")
		card.logLbl.AsCanvasItem().SetVisible(true)

		ch := card.auth.SSOLogin()
		go func() {
			for result := range ch {
				if result.Err != nil {
					card.loginErr = result.Err.Error()
					card.needsUpdate = true
					return
				}
				if result.Line != "" {
					if card.loginLog != "" {
						card.loginLog += "\n"
					}
					card.loginLog += result.Line
					card.needsUpdate = true
				}
			}
			// Channel closed — login finished successfully
			card.credStatus = card.auth.Status()
			card.needsUpdate = true
		}()

	case bfaws.CredsValid:
		card.statusLbl.SetText("Connecting...")
		card.statusDot.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)
		card.actionBtn.AsBaseButton().SetDisabled(true)
		g.startGatewayConnection(card)
	}
}

func (g *GatewayScreen) startGatewayConnection(card *gatewayCard) {
	// Check for required CLI tools before attempting connection.
	if missing := bfaws.CheckPrerequisites(); len(missing) > 0 {
		msg := "Missing required tools: " + strings.Join(missing, ", ")
		if slices.Contains(missing, "session-manager-plugin") {
			msg += "\nInstall with: brew install --cask session-manager-plugin"
		}
		if slices.Contains(missing, "aws") {
			msg += "\nInstall with: brew install awscli"
		}
		card.loginErr = msg
		card.needsUpdate = true
		return
	}

	entry := card.entry
	auth := card.auth

	go func() {
		// Resolve instance ID
		instanceID, err := auth.ResolveInstanceID(entry.InstanceID, entry.InstanceTags)
		if err != nil {
			card.loginErr = "Instance resolution: " + err.Error()
			card.needsUpdate = true
			return
		}

		// Start SSM tunnel
		tunnel := bfaws.NewTunnelManager(func(status bfaws.TunnelStatus) {
			card.needsUpdate = true
		})

		err = tunnel.Start(bfaws.TunnelConfig{
			InstanceID: instanceID,
			RDSHost:    entry.RDSHost,
			RDSPort:    entry.RDSPort,
			LocalPort:  entry.LocalPort,
			AWSProfile: entry.AWSProfile,
			AWSRegion:  entry.AWSRegion,
		})
		if err != nil {
			card.loginErr = "Tunnel: " + err.Error()
			card.needsUpdate = true
			return
		}

		// Wait for tunnel readiness
		if err := tunnel.WaitReady(30 * time.Second); err != nil {
			card.loginErr = "Tunnel not ready: " + err.Error()
			card.needsUpdate = true
			tunnel.Stop()
			return
		}

		card.tunnel = tunnel
		card.connected = true
		card.needsUpdate = true
	}()
}

// Process is called each frame to update UI from background goroutines.
func (g *GatewayScreen) Process(delta Float.X) {
	// ── SSO login updates ──
	if g.ssoUpdate {
		g.ssoUpdate = false

		if g.ssoLoginErr != "" {
			g.ssoStatus.SetText("Error: " + g.ssoLoginErr)
			g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
			g.ssoLoginBtn.AsBaseButton().SetDisabled(false)
			g.ssoLoginErr = ""
		} else if g.ssoProfileDone {
			g.ssoStatus.SetText("Profile saved: " + g.ssoProfileName)
			g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
			g.ssoLoginBtn.AsBaseButton().SetDisabled(false)
			g.ssoLoginBtn.SetText("Login")
			// Pre-fill the connection form with this profile
			g.formProfile.SetText(g.ssoProfileName)
			g.formRegion.SetText(g.ssoRegion.Text())
			g.ssoProfileDone = false
		} else if g.ssoProfileErr != "" {
			g.ssoStatus.SetText("Error saving profile: " + g.ssoProfileErr)
			g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
			g.ssoLoginBtn.AsBaseButton().SetDisabled(false)
			g.ssoProfileErr = ""
		} else if g.ssoRolesReady {
			g.ssoRolesReady = false
			if g.ssoRolesErr != "" {
				g.ssoStatus.SetText("Error loading roles: " + g.ssoRolesErr)
				g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
				g.ssoRolesErr = ""
			} else {
				g.ssoStatus.SetText("Select a role:")
				g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
				g.showRolePicker()
			}
		} else if g.ssoAccountsReady {
			g.ssoAccountsReady = false
			if g.ssoAccountsErr != "" {
				g.ssoStatus.SetText("Error loading accounts: " + g.ssoAccountsErr)
				g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
				g.ssoLoginBtn.AsBaseButton().SetDisabled(false)
				g.ssoAccountsErr = ""
			} else if len(g.ssoAccounts) == 0 {
				g.ssoStatus.SetText("No accounts found for this SSO session")
				g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
				g.ssoLoginBtn.AsBaseButton().SetDisabled(false)
			} else {
				g.ssoStatus.SetText("Logged in — select an account")
				g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
				g.ssoLog.AsCanvasItem().SetVisible(false)
				g.showAccountPicker()
			}
		} else if g.ssoDone {
			g.ssoStatus.SetText("Loading accounts...")
			g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)
			g.ssoLog.AsCanvasItem().SetVisible(false)
			g.ssoDone = false
		} else if g.ssoLoginLog != "" {
			g.ssoLog.SetText(g.ssoLoginLog)
			g.ssoLog.AsCanvasItem().SetVisible(true)
			g.ssoStatus.SetText("Waiting for browser authorization...")
			g.ssoStatus.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)
		}
	}

	// ── Instance list updates ──
	if g.instancesReady {
		g.instancesReady = false
		g.instancesLoading = false
		g.formInstanceBtn.AsBaseButton().SetDisabled(false)
		g.formInstanceBtn.SetText("Load")

		if g.instancesErr != "" {
			g.formStatus.SetText("Error loading instances: " + g.instancesErr)
			g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
			g.instancesErr = ""
		} else {
			// Clear existing items and repopulate
			g.formInstance.Clear()
			g.formInstanceIDs = nil

			if len(g.instancesResult) == 0 {
				g.formInstance.AddItem("No online instances found")
				g.formStatus.SetText("No SSM-managed instances are online")
				g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)
			} else {
				g.formInstance.AddItem(fmt.Sprintf("Select an instance (%d found)...", len(g.instancesResult)))
				for _, inst := range g.instancesResult {
					label := inst.InstanceID
					if inst.Name != "" {
						label = fmt.Sprintf("%s (%s)", inst.Name, inst.InstanceID)
					}
					if inst.IPAddress != "" {
						label += " — " + inst.IPAddress
					}
					g.formInstance.AddItem(label)
					g.formInstanceIDs = append(g.formInstanceIDs, inst.InstanceID)
				}
				g.formStatus.SetText(fmt.Sprintf("Found %d instance(s)", len(g.instancesResult)))
				g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
			}
			g.instancesResult = nil
		}
	}

	// ── RDS list updates ──
	if g.rdsReady {
		g.rdsReady = false
		g.rdsLoading = false
		g.formRDSBtn.AsBaseButton().SetDisabled(false)
		g.formRDSBtn.SetText("Load")

		if g.rdsErr != "" {
			g.formStatus.SetText("Error loading RDS: " + g.rdsErr)
			g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
			g.rdsErr = ""
		} else {
			g.formRDS.Clear()
			g.formRDSData = nil

			if len(g.rdsResult) == 0 {
				g.formRDS.AddItem("No RDS instances found")
				g.formStatus.SetText("No RDS instances found")
				g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusYellow)
			} else {
				g.formRDS.AddItem(fmt.Sprintf("Select an RDS instance (%d found)...", len(g.rdsResult)))
				for _, inst := range g.rdsResult {
					label := fmt.Sprintf("%s — %s:%d", inst.Identifier, inst.Endpoint, inst.Port)
					if inst.Engine != "" {
						label += " [" + inst.Engine + "]"
					}
					g.formRDS.AddItem(label)
				}
				g.formRDSData = g.rdsResult
				g.formStatus.SetText(fmt.Sprintf("Found %d RDS instance(s)", len(g.rdsResult)))
				g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
			}
			g.rdsResult = nil
		}
	}

	// ── Gateway card updates ──
	for _, card := range g.cards {
		if !card.needsUpdate {
			continue
		}
		card.needsUpdate = false

		// Update log output
		if card.loginLog != "" {
			if card.fromForm {
				g.formStatus.SetText(card.loginLog)
			} else {
				card.logLbl.SetText(card.loginLog)
				card.logLbl.AsCanvasItem().SetVisible(true)
			}
		}

		if card.loginErr != "" {
			errMsg := "Error: " + card.loginErr
			if card.fromForm {
				g.formStatus.SetText(errMsg)
				g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
			} else {
				card.statusLbl.SetText(errMsg)
				card.statusDot.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
				card.actionBtn.AsBaseButton().SetDisabled(false)
				card.actionBtn.SetText("Retry")
			}
			card.loginErr = ""
			continue
		}

		if card.connected {
			if card.fromForm {
				g.formStatus.SetText("Connected!")
				g.formStatus.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
			} else {
				card.statusLbl.SetText("Connected")
				card.statusDot.SetText("●")
				card.statusDot.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
				card.actionBtn.AsBaseButton().SetDisabled(true)
				card.logLbl.AsCanvasItem().SetVisible(false)
			}

			if g.OnConnect != nil {
				g.OnConnect(card.entry, card.auth, card.tunnel)
			}
			card.connected = false
			continue
		}

		// Only update saved-gateway cards (form cards don't have these nodes)
		if card.fromForm {
			continue
		}

		switch card.credStatus {
		case bfaws.CredsValid:
			card.statusLbl.SetText("Credentials valid")
			card.statusDot.SetText("●")
			card.statusDot.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
			card.actionBtn.SetText("Connect")
			card.actionBtn.AsBaseButton().SetDisabled(false)
			card.logLbl.AsCanvasItem().SetVisible(false)
		case bfaws.CredsExpired:
			card.statusLbl.SetText("Credentials expired")
			card.statusDot.SetText("○")
			card.statusDot.AsControl().AddThemeColorOverride("font_color", colorStatusRed)
			card.actionBtn.SetText("SSO Login")
			card.actionBtn.AsBaseButton().SetDisabled(false)
		case bfaws.CredsNoCredentials:
			card.statusLbl.SetText("No credentials")
			card.statusDot.SetText("○")
			card.statusDot.AsControl().AddThemeColorOverride("font_color", colorStatusGray)
			card.actionBtn.SetText("SSO Login")
			card.actionBtn.AsBaseButton().SetDisabled(false)
		}
	}
}
