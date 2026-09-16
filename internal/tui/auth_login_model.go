package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/auth"
	"spark/internal/auth/oauth"
	"spark/internal/config"
)

type authAuthURLMsg struct {
	Provider string
	URL      string
}

type authChannelEventMsg struct {
	ch  <-chan tea.Msg
	msg tea.Msg
}

type authLoginFinishedMsg struct {
	Provider string
	Auth     *auth.Auth
	Err      error
}

type authDevicePromptMsg struct {
	Provider        string
	VerificationURI string
	UserCode        string
	Interval        time.Duration
}

type authStatusRefreshMsg struct {
	Records map[string]*auth.Auth
}

type authManagerModel struct {
	store    *auth.Store
	specs    []auth.ProviderSpec
	records  map[string]*auth.Auth
	selected int
	width    int
	height   int
	status   string

	loggingIn       bool
	loginProvider   string
	loginMode       string // "browser" or "device"
	authURL         string
	deviceCode      string
	verificationURI string
	cancelLogin     context.CancelFunc
	manualCh        chan string
	manualInput     string

	confirmLogout bool
}

func ManageAuthDashboard() error {
	store, err := auth.DefaultStore()
	if err != nil {
		return fmt.Errorf("open auth store: %w", err)
	}
	m := newAuthManagerModel(store)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func newAuthManagerModel(store *auth.Store) *authManagerModel {
	m := &authManagerModel{
		store:   store,
		specs:   auth.Specs,
		records: make(map[string]*auth.Auth),
		status:  "Ready. Press 'b' or Enter for Browser OAuth, 'd' for Device Flow.",
	}
	m.reloadRecords()
	return m
}

func (m *authManagerModel) reloadRecords() {
	if m.store == nil {
		return
	}
	recs, err := m.store.List()
	if err != nil {
		return
	}
	m.records = make(map[string]*auth.Auth, len(recs))
	for _, r := range recs {
		m.records[r.Provider] = r
	}
}

func (m *authManagerModel) Init() tea.Cmd {
	return nil
}

func waitForAuthMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return authChannelEventMsg{ch: ch, msg: msg}
	}
}

func (m *authManagerModel) handleLoginFinished(msg authLoginFinishedMsg) (tea.Model, tea.Cmd) {
	m.loggingIn = false
	m.cancelLogin = nil
	m.authURL = ""
	m.deviceCode = ""
	m.verificationURI = ""
	m.manualInput = ""
	m.manualCh = nil
	if msg.Err != nil {
		if m.loginMode == "device" {
			m.status = errorStatus(fmt.Sprintf("Device login failed: %v (Tip: Press 'b' for Browser OAuth)", msg.Err))
		} else {
			m.status = errorStatus(fmt.Sprintf("Browser login failed: %v (Tip: Press 'd' for Device Flow)", msg.Err))
		}
		return m, nil
	}
	if msg.Auth != nil && m.store != nil {
		_ = m.store.Save(msg.Auth)
		m.reloadRecords()
		createdProf := ""
		if cfg, err := config.Load(); err == nil && cfg != nil {
			profName, created := cfg.EnsureProfileForAuthProvider(msg.Provider)
			if created {
				_ = config.Save(cfg)
				createdProf = profName
			}
		}
		if createdProf != "" {
			m.status = successStatus(fmt.Sprintf("Successfully logged in to %s! Configured profile '%s'.", msg.Provider, createdProf))
		} else {
			m.status = successStatus(fmt.Sprintf("Successfully logged in to %s!", msg.Provider))
		}
	}
	return m, nil
}

func (m *authManagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case authChannelEventMsg:
		switch inner := msg.msg.(type) {
		case authAuthURLMsg:
			m.authURL = inner.URL
			m.status = fmt.Sprintf("Please open this URL in your browser:\n%s", inner.URL)
			return m, waitForAuthMsg(msg.ch)
		case authDevicePromptMsg:
			m.verificationURI = inner.VerificationURI
			m.deviceCode = inner.UserCode
			m.status = fmt.Sprintf("Visit %s and enter code: %s", inner.VerificationURI, inner.UserCode)
			return m, waitForAuthMsg(msg.ch)
		case authLoginFinishedMsg:
			return m.handleLoginFinished(inner)
		}
		return m, nil

	case authDevicePromptMsg:
		m.verificationURI = msg.VerificationURI
		m.deviceCode = msg.UserCode
		m.status = fmt.Sprintf("Visit %s and enter code: %s", msg.VerificationURI, msg.UserCode)
		return m, nil

	case authLoginFinishedMsg:
		return m.handleLoginFinished(msg)

	case authStatusRefreshMsg:
		m.records = msg.Records
		return m, nil

	case tea.KeyMsg:
		if m.loggingIn {
			switch msg.String() {
			case "esc", "ctrl+c":
				if m.cancelLogin != nil {
					m.cancelLogin()
					m.cancelLogin = nil
				}
				m.loggingIn = false
				m.manualInput = ""
				m.manualCh = nil
				m.status = "Login canceled."
				return m, nil
			case "enter":
				val := strings.TrimSpace(m.manualInput)
				if val != "" && m.manualCh != nil {
					select {
					case m.manualCh <- val:
						m.status = "Verifying entered API key / callback..."
					default:
					}
					m.manualInput = ""
				}
				return m, nil
			case "backspace":
				if len(m.manualInput) > 0 {
					r := []rune(m.manualInput)
					m.manualInput = string(r[:len(r)-1])
				}
				return m, nil
			case "ctrl+u":
				m.manualInput = ""
				return m, nil
			default:
				if len(msg.Runes) > 0 {
					m.manualInput += string(msg.Runes)
					return m, nil
				}
			}
			return m, nil
		}

		if m.confirmLogout {
			key := strings.ToLower(msg.String())
			if key == "y" {
				m.confirmLogout = false
				return m, m.performLogout()
			}
			m.confirmLogout = false
			m.status = "Logout canceled."
			return m, nil
		}

		key := strings.ToLower(msg.String())
		if cmd, ok := quitOnScreenBack(key); ok {
			return m, cmd
		}

		switch key {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.specs)-1 {
				m.selected++
			}
		case "enter":
			spec := m.currentSpec()
			if spec.Provider == auth.ProviderKimi || (spec.SupportsDeviceFlow && spec.AuthEndpoint == "") {
				return m, m.startDeviceLogin()
			}
			return m, m.startBrowserLogin()
		case "b":
			spec := m.currentSpec()
			if spec.AuthEndpoint == "" {
				m.status = fmt.Sprintf("%s does not support Browser OAuth. Press 'd' for Device Flow.", spec.Label)
				return m, nil
			}
			return m, m.startBrowserLogin()
		case "d":
			spec := m.currentSpec()
			if !spec.SupportsDeviceFlow {
				m.status = fmt.Sprintf("%s does not support Device Flow. Press 'b' or Enter for Browser OAuth.", spec.Label)
				return m, nil
			}
			return m, m.startDeviceLogin()
		case "x", "delete":
			spec := m.currentSpec()
			if _, ok := m.records[spec.Provider]; ok {
				m.confirmLogout = true
				m.status = fmt.Sprintf("Log out from %s (%s)? Press Y to confirm or N to cancel.", spec.Label, spec.Provider)
			} else {
				m.status = fmt.Sprintf("Not logged in to %s.", spec.Label)
			}
		}
	}
	return m, nil
}

func (m *authManagerModel) currentSpec() auth.ProviderSpec {
	if m.selected >= 0 && m.selected < len(m.specs) {
		return m.specs[m.selected]
	}
	return m.specs[0]
}

func (m *authManagerModel) startBrowserLogin() tea.Cmd {
	spec := m.currentSpec()
	if spec.AuthEndpoint == "" {
		m.status = fmt.Sprintf("%s does not support Browser OAuth. Press 'd' for Device Flow.", spec.Label)
		return nil
	}
	m.loggingIn = true
	m.loginProvider = spec.Provider
	m.loginMode = "browser"
	m.authURL = ""
	m.manualInput = ""
	m.manualCh = make(chan string, 1)
	m.status = fmt.Sprintf("Starting browser login for %s...", spec.Label)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	m.cancelLogin = cancel

	msgCh := make(chan tea.Msg, 2)
	manualCh := m.manualCh
	go func() {
		a, err := oauth.BrowserFlow(ctx, spec, oauth.BrowserOptions{
			ManualCallbackURL: manualCh,
			OnAuthURL: func(rawURL string) {
				msgCh <- authAuthURLMsg{
					Provider: spec.Provider,
					URL:      rawURL,
				}
			},
		})
		msgCh <- authLoginFinishedMsg{
			Provider: spec.Provider,
			Auth:     a,
			Err:      err,
		}
	}()

	return waitForAuthMsg(msgCh)
}

func (m *authManagerModel) startDeviceLogin() tea.Cmd {
	spec := m.currentSpec()
	if !spec.SupportsDeviceFlow {
		m.status = fmt.Sprintf("%s does not support Device Flow.", spec.Label)
		return nil
	}
	m.loggingIn = true
	m.loginProvider = spec.Provider
	m.loginMode = "device"
	m.deviceCode = ""
	m.verificationURI = ""
	m.status = fmt.Sprintf("Initiating Device Flow for %s...", spec.Label)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	m.cancelLogin = cancel

	msgCh := make(chan tea.Msg, 2)
	go func() {
		a, err := oauth.DeviceFlow(ctx, spec, func(res oauth.DeviceResult) error {
			_ = oauth.OpenBrowser(res.VerificationURI)
			msgCh <- authDevicePromptMsg{
				Provider:        spec.Provider,
				VerificationURI: res.VerificationURI,
				UserCode:        res.UserCode,
				Interval:        res.Interval,
			}
			return nil
		})
		msgCh <- authLoginFinishedMsg{
			Provider: spec.Provider,
			Auth:     a,
			Err:      err,
		}
	}()

	return waitForAuthMsg(msgCh)
}

func (m *authManagerModel) performLogout() tea.Cmd {
	spec := m.currentSpec()
	if m.store != nil {
		_ = m.store.Delete(spec.Provider)
		m.reloadRecords()
	}
	m.status = successStatus(fmt.Sprintf("Logged out from %s.", spec.Label))
	return nil
}

func (m *authManagerModel) View() string {
	var sb strings.Builder

	title := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("Spark Authentication & Upstream Logins")
	sb.WriteString(title + "\n\n")

	sb.WriteString(lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Available Providers:") + "\n")

	for i, spec := range m.specs {
		isSelected := i == m.selected
		rec, isLoggedIn := m.records[spec.Provider]

		statusStr := lipgloss.NewStyle().Foreground(colorMuted).Render("[Not Logged In]")
		if isLoggedIn && rec != nil {
			if rec.Expired(0) {
				statusStr = lipgloss.NewStyle().Foreground(colorWarning).Render("[Expired]")
			} else {
				statusStr = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("[Active]")
			}
			if rec.Account != "" {
				statusStr += " " + lipgloss.NewStyle().Foreground(colorTextSoft).Render(rec.Account)
			}
		}

		cursor := "  "
		itemStyle := lipgloss.NewStyle().Foreground(colorText)
		if isSelected {
			cursor = "❯ "
			itemStyle = lipgloss.NewStyle().Foreground(colorFocus).Bold(true)
		}

		line := fmt.Sprintf("%s%-18s %s", cursor, spec.Label, statusStr)
		sb.WriteString(itemStyle.Render(line) + "\n")
	}

	sb.WriteString("\n")

	// Detail view of currently selected provider
	spec := m.currentSpec()
	rec := m.records[spec.Provider]

	boxWidth := 72
	if m.width > 78 {
		boxWidth = min(m.width-4, 98)
	}

	detailBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Width(boxWidth)

	var details strings.Builder
	details.WriteString(lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render(spec.Label) + "\n")
	details.WriteString(fmt.Sprintf("Provider ID:    %s\n", spec.Provider))
	if rec != nil {
		details.WriteString(fmt.Sprintf("Login Type:     %s\n", rec.Kind))
		if rec.Account != "" {
			details.WriteString(fmt.Sprintf("Account:        %s\n", rec.Account))
		}
		if !rec.ExpiresAt.IsZero() {
			if rec.Expired(0) {
				details.WriteString(fmt.Sprintf("Expires:        Expired (%s)\n", rec.ExpiresAt.Format(time.RFC3339)))
			} else {
				details.WriteString(fmt.Sprintf("Expires:        %s (in %s)\n", rec.ExpiresAt.Format(time.RFC3339), time.Until(rec.ExpiresAt).Round(time.Minute)))
			}
		}
	} else {
		details.WriteString("Status:         Not logged in (BYOK or unconfigured)\n")
	}

	supportsBrowser := spec.AuthEndpoint != ""
	supportsDevice := spec.SupportsDeviceFlow

	if supportsBrowser && supportsDevice {
		details.WriteString("Auth Methods:   [B] Browser OAuth (PKCE)  |  [D] Device Code Flow\n")
		details.WriteString(fmt.Sprintf("OAuth Callback: http://localhost:%d%s\n", spec.DefaultCallbackPort, spec.EffectiveCallbackPath()))
		if spec.DeviceVerificationEndpoint != "" {
			details.WriteString(fmt.Sprintf("Device Verify:  %s\n", spec.DeviceVerificationEndpoint))
		}
	} else if supportsDevice {
		details.WriteString("Auth Methods:   [D] Device Code Flow (RFC 8628)\n")
		if spec.DeviceVerificationEndpoint != "" {
			details.WriteString(fmt.Sprintf("Device Verify:  %s\n", spec.DeviceVerificationEndpoint))
		}
	} else if supportsBrowser {
		details.WriteString("Auth Methods:   [B] Browser OAuth (PKCE)\n")
		details.WriteString(fmt.Sprintf("OAuth Callback: http://localhost:%d%s\n", spec.DefaultCallbackPort, spec.EffectiveCallbackPath()))
	}

	if m.loggingIn {
		details.WriteString("\n" + lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("⏳ Logging in... Press Esc to cancel") + "\n")
		if m.deviceCode != "" {
			details.WriteString("\n" + lipgloss.NewStyle().Foreground(colorFocus).Bold(true).Render("👉 Authorization Steps:") + "\n")
			details.WriteString(fmt.Sprintf("  1. Open in browser:  %s\n", lipgloss.NewStyle().Foreground(colorAccent).Underline(true).Render(m.verificationURI)))
			details.WriteString(fmt.Sprintf("  2. Enter code:       %s\n\n", lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render(m.deviceCode)))
			details.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("Waiting for authorization approval in browser...") + "\n")
		} else if m.authURL != "" {
			details.WriteString(fmt.Sprintf("Auth URL (copy to browser):\n%s\n", m.authURL))
		}

		if m.loginMode == "browser" {
			details.WriteString("\n" + lipgloss.NewStyle().Foreground(colorFocus).Bold(true).Render("Paste API Key / Callback URL below:") + "\n")
			details.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("(If browser auto-transfer failed, paste the key or URL here and press Enter)") + "\n")

			inputText := m.manualInput
			cursor := lipgloss.NewStyle().Foreground(colorFocus).Render("█")
			if inputText == "" {
				placeholder := lipgloss.NewStyle().Foreground(colorMuted).Render("paste API key (e.g. user_...) here")
				details.WriteString(fmt.Sprintf("❯ %s%s\n", cursor, placeholder))
			} else {
				displayVal := inputText
				if len(displayVal) > 50 {
					displayVal = displayVal[:22] + "..." + displayVal[len(displayVal)-18:]
				}
				valRendered := lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render(displayVal)
				details.WriteString(fmt.Sprintf("❯ %s%s\n", valRendered, cursor))
			}
		}
	}

	sb.WriteString(detailBox.Render(details.String()) + "\n\n")

	// Status and help
	if m.status != "" {
		sb.WriteString(m.status + "\n\n")
	}

	var helpText string
	if m.loggingIn {
		if m.loginMode == "browser" {
			helpText = lipgloss.NewStyle().Foreground(colorMuted).Render("Paste or type API Key / URL • Enter: Submit key • Esc: Cancel")
		} else {
			helpText = lipgloss.NewStyle().Foreground(colorMuted).Render("Esc: Cancel")
		}
	} else {
		spec := m.currentSpec()
		supportsBrowser := spec.AuthEndpoint != ""
		supportsDevice := spec.SupportsDeviceFlow
		if supportsBrowser && supportsDevice {
			helpText = lipgloss.NewStyle().Foreground(colorMuted).Render("Enter/B: Browser Login • D: Device Code Login • X: Logout • Esc/Q: Back")
		} else if supportsDevice {
			helpText = lipgloss.NewStyle().Foreground(colorMuted).Render("Enter/D: Device Login • X: Logout • Esc/Q: Back")
		} else {
			helpText = lipgloss.NewStyle().Foreground(colorMuted).Render("Enter/B: Browser Login • X: Logout • Esc/Q: Back")
		}
	}
	sb.WriteString(helpText)

	return sb.String()
}
