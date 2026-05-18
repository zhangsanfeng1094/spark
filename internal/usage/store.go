package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"spark/internal/compat/ir"
	"spark/internal/config"
)

type Record struct {
	Timestamp         time.Time `json:"timestamp"`
	Client            string    `json:"client,omitempty"`
	Model             string    `json:"model,omitempty"`
	Stream            bool      `json:"stream"`
	InputTokens       int       `json:"input_tokens"`
	OutputTokens      int       `json:"output_tokens"`
	TotalTokens       int       `json:"total_tokens"`
	CachedInputTokens int       `json:"cached_input_tokens"`
}

type Summary struct {
	Window            Window
	Client            string
	Requests          int
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
}

type Window string

const (
	WindowToday Window = "today"
	Window7D    Window = "7d"
	Window30D   Window = "30d"
	WindowAll   Window = "all"
)

var defaultStoreMu sync.Mutex
var recorderMu sync.RWMutex
var recorder func(Record) error

func Windows() []Window {
	return []Window{WindowToday, Window7D, Window30D, WindowAll}
}

func ClientAll() string { return "all" }

func DefaultPath() (string, error) {
	configPath, err := config.ConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(configPath), "token_usage.jsonl"), nil
}

func AppendDefault(record Record) error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	defaultStoreMu.Lock()
	defer defaultStoreMu.Unlock()
	return Append(path, record)
}

func EnableDefaultRecorder() {
	SetRecorder(AppendDefault)
}

func SetRecorder(fn func(Record) error) {
	recorderMu.Lock()
	defer recorderMu.Unlock()
	recorder = fn
}

func RecordIR(usage ir.Usage, model string, stream bool, now time.Time) {
	record, ok := RecordFromIRUsage(usage, model, stream, now)
	if !ok {
		return
	}
	recorderMu.RLock()
	fn := recorder
	recorderMu.RUnlock()
	if fn != nil {
		_ = fn(record)
	}
}

func Append(path string, record Record) error {
	if path == "" {
		return errors.New("usage path is required")
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}
	if record.TotalTokens == 0 && (record.InputTokens > 0 || record.OutputTokens > 0) {
		record.TotalTokens = record.InputTokens + record.OutputTokens
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func Read(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var records []Record
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if record.Timestamp.IsZero() {
			continue
		}
		if record.TotalTokens == 0 && (record.InputTokens > 0 || record.OutputTokens > 0) {
			record.TotalTokens = record.InputTokens + record.OutputTokens
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func Summaries(path string, now time.Time) ([]Summary, error) {
	records, err := Read(path)
	if err != nil {
		return nil, err
	}
	return Summarize(records, now), nil
}

func DefaultSummaries(now time.Time) ([]Summary, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return Summaries(path, now)
}

func Summarize(records []Record, now time.Time) []Summary {
	return SummarizeForClients(records, []string{ClientAll()}, now)
}

func SummarizeForClients(records []Record, clients []string, now time.Time) []Summary {
	if now.IsZero() {
		now = time.Now()
	}
	if len(clients) == 0 {
		clients = []string{ClientAll()}
	}
	out := make([]Summary, 0, len(Windows())*len(clients))
	for _, client := range clients {
		client = normalizeClient(client)
		for _, window := range Windows() {
			summary := Summary{Window: window, Client: client}
			for _, record := range records {
				if !inWindow(record.Timestamp, window, now) || !recordMatchesClient(record, client) {
					continue
				}
				summary.Requests++
				summary.InputTokens += record.InputTokens
				summary.OutputTokens += record.OutputTokens
				total := record.TotalTokens
				if total == 0 {
					total = record.InputTokens + record.OutputTokens
				}
				summary.TotalTokens += total
				summary.CachedInputTokens += record.CachedInputTokens
			}
			out = append(out, summary)
		}
	}
	return out
}

func Clients(records []Record) []string {
	seen := map[string]bool{ClientAll(): true}
	for _, record := range records {
		client := normalizeClient(record.Client)
		if client == "" {
			client = "unknown"
		}
		seen[client] = true
	}
	clients := make([]string, 0, len(seen))
	for client := range seen {
		clients = append(clients, client)
	}
	sort.Slice(clients, func(i, j int) bool {
		if clients[i] == ClientAll() {
			return true
		}
		if clients[j] == ClientAll() {
			return false
		}
		return clients[i] < clients[j]
	})
	return clients
}

func RecordFromUsageMap(usage map[string]any, model string, stream bool, now time.Time) (Record, bool) {
	input := intFromAny(usage["input_tokens"])
	if input == 0 {
		input = intFromAny(usage["prompt_tokens"])
	}
	output := intFromAny(usage["output_tokens"])
	if output == 0 {
		output = intFromAny(usage["completion_tokens"])
	}
	total := intFromAny(usage["total_tokens"])
	if total == 0 && (input > 0 || output > 0) {
		total = input + output
	}
	cached := intFromAny(usage["cached_input_tokens"])
	if cached == 0 {
		cached = intFromAny(usage["cached_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(mapValue(usage["input_tokens_details"])["cached_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(mapValue(usage["prompt_tokens_details"])["cached_tokens"])
	}
	if input == 0 && output == 0 && total == 0 && cached == 0 {
		return Record{}, false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return Record{
		Timestamp:         now,
		Client:            "",
		Model:             model,
		Stream:            stream,
		InputTokens:       input,
		OutputTokens:      output,
		TotalTokens:       total,
		CachedInputTokens: cached,
	}, true
}

func RecordFromIRUsage(usage ir.Usage, model string, stream bool, now time.Time) (Record, bool) {
	record, ok := RecordFromUsageMap(usage.Raw, model, stream, now)
	if usage.InputTokens != 0 {
		record.InputTokens = usage.InputTokens
	}
	if usage.OutputTokens != 0 {
		record.OutputTokens = usage.OutputTokens
	}
	if usage.TotalTokens != 0 {
		record.TotalTokens = usage.TotalTokens
	}
	if usage.CacheReadInputTokens != 0 {
		record.CachedInputTokens = usage.CacheReadInputTokens
	}
	if record.TotalTokens == 0 && (record.InputTokens > 0 || record.OutputTokens > 0) {
		record.TotalTokens = record.InputTokens + record.OutputTokens
	}
	if record.Timestamp.IsZero() {
		if now.IsZero() {
			now = time.Now().UTC()
		}
		record.Timestamp = now
	}
	record.Model = model
	record.Stream = stream
	if record.InputTokens == 0 && record.OutputTokens == 0 && record.TotalTokens == 0 && record.CachedInputTokens == 0 {
		return Record{}, false
	}
	return record, ok || true
}

func normalizeClient(client string) string {
	return strings.ToLower(strings.TrimSpace(client))
}

func recordMatchesClient(record Record, client string) bool {
	if client == "" || client == ClientAll() {
		return true
	}
	recordClient := normalizeClient(record.Client)
	if recordClient == "" {
		recordClient = "unknown"
	}
	return recordClient == client
}

func inWindow(t time.Time, window Window, now time.Time) bool {
	t = t.In(now.Location())
	switch window {
	case WindowToday:
		y1, m1, d1 := now.Date()
		y2, m2, d2 := t.Date()
		return y1 == y2 && m1 == m2 && d1 == d2
	case Window7D:
		return !t.Before(now.AddDate(0, 0, -7))
	case Window30D:
		return !t.Before(now.AddDate(0, 0, -30))
	case WindowAll:
		return true
	default:
		return false
	}
}

func mapValue(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}
