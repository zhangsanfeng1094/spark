package tui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
	"spark/internal/probe"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func fakeResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestParseOpenAIModelsResponse(t *testing.T) {
	body := []byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4o-mini"},{"id":"  "},{"id":"o3-mini"}]}`)
	got, err := parseOpenAIModelsResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 models, got %d (%v)", len(got), got)
	}
	if got[0] != "gpt-4o-mini" || got[1] != "o3-mini" {
		t.Fatalf("unexpected models: %v", got)
	}
}

func TestFetchOpenAIModelsWithClient(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/models" {
				return fakeResponse(req, http.StatusOK, `{"data":[{"id":"z-model"},{"id":"a-model"}]}`), nil
			}
			return fakeResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
		}),
	}

	got, err := fetchOpenAIModelsWithClient("https://example.com", "k", "org", "proj", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 models, got %d (%v)", len(got), got)
	}
	if got[0] != "a-model" || got[1] != "z-model" {
		t.Fatalf("expected sorted models, got %v", got)
	}
}

func TestFetchOpenAIModelsWithClientHTTPError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return fakeResponse(req, http.StatusUnauthorized, `{"error":"bad key"}`), nil
		}),
	}

	_, err := fetchOpenAIModelsWithClient("https://example.com", "", "", "", client)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchAnthropicModelsWithClient(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/models" {
				t.Fatalf("path mismatch: %s", req.URL.Path)
			}
			if req.Method != http.MethodGet {
				t.Fatalf("method mismatch: %s", req.Method)
			}
			if req.Header.Get("x-api-key") != "anthropic-key" {
				t.Fatalf("missing anthropic api key header: %#v", req.Header)
			}
			if req.Header.Get("anthropic-version") == "" {
				t.Fatalf("missing anthropic version header: %#v", req.Header)
			}
			if req.Header.Get("Authorization") != "" {
				t.Fatalf("unexpected bearer auth for anthropic model fetch: %#v", req.Header)
			}
			return fakeResponse(req, http.StatusOK, `{"data":[{"id":"claude-b"},{"id":"claude-a"}]}`), nil
		}),
	}

	got, err := fetchModelsWithClient(&config.Profile{
		OpenAIBaseURL: "https://api.anthropic.com",
		OpenAIAPIKey:  "anthropic-key",
		OpenAIAPIType: config.OpenAIAPITypeAnthropicMessages,
	}, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "claude-a" || got[1] != "claude-b" {
		t.Fatalf("expected sorted anthropic models, got %v", got)
	}
}

func TestFetchAnthropicModelsWithClientBaseURLWithV1Suffix(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/models" {
				t.Fatalf("path mismatch: %s", req.URL.Path)
			}
			return fakeResponse(req, http.StatusOK, `{"data":[{"id":"claude-a"}]}`), nil
		}),
	}

	got, err := fetchModelsWithClient(&config.Profile{
		OpenAIBaseURL: "https://api.anthropic.com/v1",
		OpenAIAPIKey:  "anthropic-key",
		OpenAIAPIType: config.OpenAIAPITypeAnthropicMessages,
	}, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "claude-a" {
		t.Fatalf("models mismatch: %v", got)
	}
}

func TestFetchAnthropicModelsWithClientUnsupportedEndpointErrorIsActionable(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return fakeResponse(req, http.StatusNotFound, `<html><body><h1>404 Not Found</h1><center>openresty</center></body></html>`), nil
		}),
	}

	_, err := fetchModelsWithClient(&config.Profile{
		OpenAIBaseURL: "https://gateway.example",
		OpenAIAPIKey:  "anthropic-key",
		OpenAIAPIType: config.OpenAIAPITypeAnthropicMessages,
	}, client)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "does not support Anthropic model listing") || !strings.Contains(msg, "https://gateway.example/v1/models") {
		t.Fatalf("expected actionable unsupported endpoint error, got %q", msg)
	}
	if strings.Contains(msg, "<html>") || strings.Contains(msg, "openresty") {
		t.Fatalf("expected sanitized error without HTML body, got %q", msg)
	}
}

func TestFetchModelsWithCustomModelListURL(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://gateway.example/custom/list-models" {
				t.Fatalf("url mismatch: %s", req.URL.String())
			}
			if req.Header.Get("x-api-key") != "anthropic-key" {
				t.Fatalf("missing anthropic api key header: %#v", req.Header)
			}
			return fakeResponse(req, http.StatusOK, `{"data":[{"id":"claude-custom"}]}`), nil
		}),
	}

	got, err := fetchModelsWithClient(&config.Profile{
		OpenAIBaseURL: "https://gateway.example",
		OpenAIAPIKey:  "anthropic-key",
		OpenAIAPIType: config.OpenAIAPITypeAnthropicMessages,
		ModelListURL:  "https://gateway.example/custom/list-models",
	}, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "claude-custom" {
		t.Fatalf("models mismatch: %v", got)
	}
}

func TestProfileManagerFetchModelsUsesSelectedAPIType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "anthropic-key" {
			t.Fatalf("missing anthropic api key header: %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-a"}]}`))
	}))
	defer server.Close()

	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {
				OpenAIBaseURL: server.URL,
				OpenAIAPIKey:  "anthropic-key",
				OpenAIAPIType: config.OpenAIAPITypeAnthropicMessages,
			},
		},
	})
	m.openModelsModal()

	cmd := m.fetchModelsFromAPI()
	msg := cmd()
	got := msg.(fetchModelsResultMsg)
	if got.err != nil {
		t.Fatalf("unexpected error: %v", got.err)
	}
	if len(got.models) != 1 || got.models[0] != "claude-a" {
		t.Fatalf("models mismatch: %v", got.models)
	}
}

func TestProfileManagerTestConnection_SetsStatusWithLogPath(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ui-trigger.log")
	t.Setenv("SPARK_MODEL_TEST_LOG", logPath)

	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {
				OpenAIBaseURL: "http://127.0.0.1:9999/v1",
				OpenAIAPIType: config.OpenAIAPITypeChatCompletions,
			},
		},
	})
	m.selectByName("p1")
	m.loadSelectedProfileFields()

	_ = m.testConnection()
	if !strings.Contains(m.status, "Testing connection... (log: ") {
		t.Fatalf("expected status to include log path, got %q", m.status)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected UI trigger log file to exist: %v", err)
	}
}

func TestProfileManagerSaveIgnoresPendingTestResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {
				OpenAIBaseURL: "http://127.0.0.1:9999/v1",
				OpenAIAPIType: config.OpenAIAPITypeChatCompletions,
			},
		},
		Integrations: map[string]*config.IntegrationConfig{},
	})
	m.selectByName("p1")
	m.loadSelectedProfileFields()

	if cmd := m.testConnection(); cmd == nil {
		t.Fatal("expected connection test command")
	}
	pendingSeq := m.runningTestSeq
	if pendingSeq == 0 {
		t.Fatal("expected pending test sequence")
	}

	m.save()
	if got := m.status; got != "Saved profile p1." {
		t.Fatalf("expected save status before stale result, got %q", got)
	}

	m.handleTestResult(testResultMsg{
		statusSeq: pendingSeq,
		result: probe.TestResult{
			Success: true,
			Message: "OK",
			Latency: 17 * time.Millisecond,
		},
	})

	if got := m.status; got != "Saved profile p1." {
		t.Fatalf("expected stale test result not to overwrite save status, got %q", got)
	}
	if got := m.statusSummaryText(); got != "✓ Saved profile" {
		t.Fatalf("expected save summary after stale result, got %q", got)
	}
	if m.lastTestSummary != "" {
		t.Fatalf("expected stale test summary to remain cleared, got %q", m.lastTestSummary)
	}
}

func TestHandleMainMouse_TestButtonReturnsCmd(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {OpenAIBaseURL: "http://127.0.0.1:9999/v1"},
		},
	})
	m.leftContentX = 0
	m.leftContentY = 100
	m.rightContentX = 10
	m.rightContentY = 20
	m.rightButtonsRelY = 5

	cmd := m.handleMainMouse(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    m.rightContentX + 1,
		Y:    m.rightContentY + m.rightButtonsRelY,
	})
	if cmd == nil {
		t.Fatal("expected non-nil cmd when clicking Test button")
	}
}

func TestHandleMainMouse_LeftButtons_DoNotTriggerOutsideButtonRow(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {},
		},
	})
	m.leftContentX = 10
	m.leftContentY = 20
	m.leftButtonsRelY = 8
	m.leftButtonsRelH = 1
	m.leftButtonsRowW = 20
	m.leftAddBtnW = 9

	btnY := m.leftContentY + m.leftButtonsRelY
	cmd := m.handleMainMouse(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    m.leftContentX + 1,
		Y:    btnY - 1,
	})
	if cmd != nil {
		t.Fatal("expected click above left button row not to trigger command")
	}

	cmd = m.handleMainMouse(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    m.leftContentX + 1,
		Y:    btnY,
	})
	if cmd != nil {
		t.Fatal("expected add action to open modal without returning a command")
	}
	if !m.modalOpen || m.modalKind != pmModalKindAddProfile {
		t.Fatal("expected add button click to open add-profile modal")
	}

	cmd = m.handleMainMouse(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    m.leftContentX + 1,
		Y:    btnY + 1,
	})
	if cmd != nil {
		t.Fatal("expected click below left button row not to trigger command")
	}
}

func TestHandleMainMouse_RightButtons_DoNotTriggerOutsideButtonRow(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {},
		},
	})
	m.rightContentX = 10
	m.rightContentY = 20
	m.rightButtonsRelY = 6
	m.rightButtonsRelH = 1
	m.rightButtonsRowW = 22
	m.rightTestBtnW = 10

	btnY := m.rightContentY + m.rightButtonsRelY
	cmd := m.handleMainMouse(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    m.rightContentX + 1,
		Y:    btnY - 1,
	})
	if cmd != nil {
		t.Fatal("expected click above right button row not to trigger command")
	}

	cmd = m.handleMainMouse(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    m.rightContentX + 1,
		Y:    btnY + 1,
	})
	if cmd != nil {
		t.Fatal("expected click below right button row not to trigger command")
	}
}

func TestHandleMainMouse_LeftButtons_OnlyInsideBorderAreaTriggers(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {},
		},
	})
	m.leftContentX = 10
	m.leftContentY = 20
	m.leftButtonsRelY = 6
	m.leftButtonsRelH = 3
	m.leftButtonsRowW = 20
	m.leftAddBtnW = 10

	btnY := m.leftContentY + m.leftButtonsRelY
	// Top border row should not trigger.
	cmd := m.handleMainMouse(tea.MouseMsg{Type: tea.MouseRelease, X: m.leftContentX + 1, Y: btnY})
	if cmd != nil || m.modalOpen {
		t.Fatal("expected top border row click not to trigger left actions")
	}
	// Middle row should trigger.
	_ = m.handleMainMouse(tea.MouseMsg{Type: tea.MouseRelease, X: m.leftContentX + 1, Y: btnY + 1})
	if !m.modalOpen || m.modalKind != pmModalKindAddProfile {
		t.Fatal("expected middle row click to trigger Add action")
	}
	// Reset and verify bottom border row does not trigger.
	m.modalOpen = false
	m.modalKind = pmModalKindNone
	cmd = m.handleMainMouse(tea.MouseMsg{Type: tea.MouseRelease, X: m.leftContentX + 1, Y: btnY + 2})
	if cmd != nil || m.modalOpen {
		t.Fatal("expected bottom border row click not to trigger left actions")
	}
}

func TestHandleMainMouse_RightButtons_OnlyInsideBorderAreaTriggers(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {},
		},
	})
	m.rightContentX = 10
	m.rightContentY = 20
	m.rightButtonsRelY = 6
	m.rightButtonsRelH = 3
	m.rightButtonsRowW = 22
	m.rightTestBtnW = 10

	btnY := m.rightContentY + m.rightButtonsRelY
	// Top border row should not trigger.
	cmd := m.handleMainMouse(tea.MouseMsg{Type: tea.MouseRelease, X: m.rightContentX + 1, Y: btnY})
	if cmd != nil {
		t.Fatal("expected top border row click not to trigger right actions")
	}
	// Middle row should trigger.
	cmd = m.handleMainMouse(tea.MouseMsg{Type: tea.MouseRelease, X: m.rightContentX + 1, Y: btnY + 1})
	if cmd == nil {
		t.Fatal("expected middle row click to trigger Test action")
	}
	// Bottom border row should not trigger.
	cmd = m.handleMainMouse(tea.MouseMsg{Type: tea.MouseRelease, X: m.rightContentX + 1, Y: btnY + 2})
	if cmd != nil {
		t.Fatal("expected bottom border row click not to trigger right actions")
	}
}

func TestModalMouseWheel_ModelsModalMovesCursor(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {},
		},
	})
	m.modelsDraft = []string{"m0", "m1", "m2"}
	m.defaultModel = "m0"
	m.openModelsModal()
	m.modalX, m.modalY, m.modalW, m.modalH = 10, 10, 40, 20

	if handled := m.handleModalWheel(tea.MouseMsg{Type: tea.MouseWheelDown, X: 12, Y: 12}); !handled {
		t.Fatal("expected wheel down to be handled inside models modal")
	}
	if m.modalCursor != 1 {
		t.Fatalf("expected cursor to move down to 1, got %d", m.modalCursor)
	}

	if handled := m.handleModalWheel(tea.MouseMsg{Type: tea.MouseWheelUp, X: 12, Y: 12}); !handled {
		t.Fatal("expected wheel up to be handled inside models modal")
	}
	if m.modalCursor != 0 {
		t.Fatalf("expected cursor to move up to 0, got %d", m.modalCursor)
	}
}

func TestHandleModalMouse_ModelsModalUsesScrollOffset(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {},
		},
	})
	models := make([]string, 0, 15)
	for i := 0; i < 15; i++ {
		models = append(models, "model-"+string(rune('a'+i)))
	}
	m.modelsDraft = models
	m.defaultModel = models[0]
	m.openModelsModal()
	m.modelModalVisibleCount = 10
	m.modalCursor = 11
	m.syncModelsModalScroll()
	if m.modelModalScroll <= 0 {
		t.Fatalf("expected positive scroll offset, got %d", m.modelModalScroll)
	}

	m.modalX, m.modalY, m.modalW, m.modalH = 0, 0, 80, 30
	// Search input shifts options by +1, and scroll marker takes one row when offset > 0.
	m.handleModalMouse(tea.MouseMsg{Type: tea.MouseRelease, X: 2, Y: m.modalY + 6})
	if m.modalCursor != m.modelModalScroll {
		t.Fatalf("expected cursor to map to scrolled index %d, got %d", m.modelModalScroll, m.modalCursor)
	}
}

func TestModelsModalSearchFiltersAndMovesCursor(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {},
		},
	})
	m.modelsDraft = []string{"gpt-4o", "o3-mini", "claude-3"}
	m.defaultModel = "gpt-4o"
	m.openModelsModal()

	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if q := m.modelSearchQuery; q != "o" {
		t.Fatalf("expected search query to be 'o', got %q", q)
	}
	filtered := m.filteredModelIndices()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered models, got %d", len(filtered))
	}
	if m.modalCursor != filtered[0] {
		t.Fatalf("expected cursor on first filtered item %d, got %d", filtered[0], m.modalCursor)
	}

	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.modelSearchQuery != "" {
		t.Fatalf("expected search query to clear, got %q", m.modelSearchQuery)
	}
}

func TestModelsModalTabTogglesSearchAndActionKeys(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {},
		},
	})
	m.modelsDraft = []string{"gpt-4o", "o3-mini"}
	m.defaultModel = "gpt-4o"
	m.openModelsModal()
	if !m.modelSearchFocused {
		t.Fatal("expected search to be focused by default")
	}

	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if m.modelSearchQuery != "f" {
		t.Fatalf("expected search query to capture typed f, got %q", m.modelSearchQuery)
	}

	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.modelSearchFocused {
		t.Fatal("expected tab to switch focus from search to actions")
	}
}

func TestModelsModalFunctionShortcutsDoNotConflictWithSearch(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {},
		},
	})
	m.modelsDraft = []string{"gpt-4o"}
	m.defaultModel = "gpt-4o"
	m.openModelsModal()
	if !m.modelSearchFocused {
		t.Fatal("expected search focused by default")
	}

	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.modelEditMode {
		t.Fatal("expected typed n to stay in search while search is focused")
	}
	if m.modelSearchQuery != "n" {
		t.Fatalf("expected search query to capture typed n, got %q", m.modelSearchQuery)
	}

	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeyTab})
	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.modelEditMode {
		t.Fatal("expected typed n to stay text input, even when search is paused")
	}

	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeyF6})
	if !m.modelEditMode {
		t.Fatal("expected F6 to trigger add-model edit mode")
	}
}

func TestModelsModalFetchUsesF5NotCtrlF(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})
	m.modelsDraft = []string{"gpt-4o"}
	m.defaultModel = "gpt-4o"
	m.openModelsModal()

	if cmd := m.handleModalKey(tea.KeyMsg{Type: tea.KeyCtrlF}); cmd != nil {
		t.Fatal("expected Ctrl+F not to trigger model fetch")
	}
	if m.modelModalNote != "" {
		t.Fatalf("expected Ctrl+F not to change fetch note, got %q", m.modelModalNote)
	}

	cmd := m.handleModalKey(tea.KeyMsg{Type: tea.KeyF5})
	if cmd == nil {
		t.Fatal("expected F5 to trigger model fetch")
	}
	if m.modelModalNote != "Fetching models from API..." {
		t.Fatalf("expected F5 to set fetch note, got %q", m.modelModalNote)
	}
}

func TestHandleMainMouse_ProfileListClickMatchesRenderedRow(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "alpha",
		Profiles: map[string]*config.Profile{
			"alpha": {},
			"beta":  {},
		},
	})
	m.width = 120
	m.height = 40
	ui := m.View()

	lines := strings.Split(ui, "\n")
	betaRow := -1
	for i, line := range lines {
		if strings.Contains(line, "beta") {
			betaRow = i
			break
		}
	}
	if betaRow < 0 {
		t.Fatalf("failed to find beta row in rendered UI")
	}

	_ = m.handleMainMouse(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    m.leftContentX + 2,
		Y:    betaRow,
	})
	if got := m.currentProfileName(); got != "beta" {
		t.Fatalf("expected clicking beta row to select beta, got %q", got)
	}
}
