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

type Breakdown struct {
	Client            string
	Model             string
	Requests          int
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
}

type DailySummary struct {
	Day               time.Time
	Requests          int
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
}

type HeavyRequest struct {
	Timestamp         time.Time
	Client            string
	Model             string
	Stream            bool
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

func ReplaceRecorder(fn func(Record) error) func() {
	recorderMu.Lock()
	prev := recorder
	recorder = fn
	recorderMu.Unlock()

	return func() {
		recorderMu.Lock()
		defer recorderMu.Unlock()
		recorder = prev
	}
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

func SummarizeWindow(records []Record, window Window, now time.Time) Summary {
	if now.IsZero() {
		now = time.Now()
	}
	summary := Summary{Window: window, Client: ClientAll()}
	for _, record := range records {
		if !inWindow(record.Timestamp, window, now) {
			continue
		}
		addRecordToSummary(&summary, record)
	}
	return summary
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
				addRecordToSummary(&summary, record)
			}
			out = append(out, summary)
		}
	}
	return out
}

func BreakdownsForWindow(records []Record, window Window, now time.Time) []Breakdown {
	if now.IsZero() {
		now = time.Now()
	}
	byKey := map[string]*Breakdown{}
	for _, record := range records {
		if !inWindow(record.Timestamp, window, now) {
			continue
		}
		client := displayClient(record.Client)
		model := displayModel(record.Model)
		key := client + "\x00" + model
		breakdown := byKey[key]
		if breakdown == nil {
			breakdown = &Breakdown{Client: client, Model: model}
			byKey[key] = breakdown
		}
		addRecordToBreakdown(breakdown, record)
	}
	out := make([]Breakdown, 0, len(byKey))
	for _, breakdown := range byKey {
		out = append(out, *breakdown)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalTokens != out[j].TotalTokens {
			return out[i].TotalTokens > out[j].TotalTokens
		}
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		if out[i].Client != out[j].Client {
			return out[i].Client < out[j].Client
		}
		return out[i].Model < out[j].Model
	})
	return out
}

func DailySeriesForWindow(records []Record, window Window, now time.Time) []DailySummary {
	if now.IsZero() {
		now = time.Now()
	}
	days := dailySeriesDays(window, now)
	if len(days) == 0 {
		return nil
	}
	byDay := make(map[string]*DailySummary, len(days))
	for i := range days {
		key := dayKey(days[i].Day)
		byDay[key] = &days[i]
	}
	for _, record := range records {
		if !inWindow(record.Timestamp, window, now) {
			continue
		}
		key := dayKey(record.Timestamp.In(now.Location()))
		day := byDay[key]
		if day == nil {
			continue
		}
		addRecordToDaily(day, record)
	}
	return days
}

func HourlySeriesForToday(records []Record, now time.Time) []DailySummary {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(now.Location())
	start := startOfDay(now)
	hours := now.Hour() + 1
	out := make([]DailySummary, 0, hours)
	byHour := make(map[string]*DailySummary, hours)
	for i := 0; i < hours; i++ {
		point := DailySummary{Day: start.Add(time.Duration(i) * time.Hour)}
		out = append(out, point)
		byHour[hourKey(point.Day)] = &out[len(out)-1]
	}
	for _, record := range records {
		if !inWindow(record.Timestamp, WindowToday, now) {
			continue
		}
		hour := byHour[hourKey(record.Timestamp.In(now.Location()))]
		if hour == nil {
			continue
		}
		addRecordToDaily(hour, record)
	}
	return out
}

func HeavyRequestsForWindow(records []Record, window Window, now time.Time, limit int) []HeavyRequest {
	if now.IsZero() {
		now = time.Now()
	}
	if limit <= 0 {
		limit = 5
	}
	out := make([]HeavyRequest, 0, limit)
	for _, record := range records {
		if !inWindow(record.Timestamp, window, now) {
			continue
		}
		out = append(out, HeavyRequest{
			Timestamp:         record.Timestamp,
			Client:            displayClient(record.Client),
			Model:             displayModel(record.Model),
			Stream:            record.Stream,
			InputTokens:       record.InputTokens,
			OutputTokens:      record.OutputTokens,
			TotalTokens:       recordTotal(record),
			CachedInputTokens: record.CachedInputTokens,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalTokens != out[j].TotalTokens {
			return out[i].TotalTokens > out[j].TotalTokens
		}
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	if len(out) > limit {
		out = out[:limit]
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

func displayClient(client string) string {
	client = normalizeClient(client)
	if client == "" {
		return "unknown"
	}
	return client
}

func displayModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "unknown model"
	}
	return model
}

func addRecordToSummary(summary *Summary, record Record) {
	summary.Requests++
	summary.InputTokens += record.InputTokens
	summary.OutputTokens += record.OutputTokens
	summary.TotalTokens += recordTotal(record)
	summary.CachedInputTokens += record.CachedInputTokens
}

func addRecordToBreakdown(summary *Breakdown, record Record) {
	summary.Requests++
	summary.InputTokens += record.InputTokens
	summary.OutputTokens += record.OutputTokens
	summary.TotalTokens += recordTotal(record)
	summary.CachedInputTokens += record.CachedInputTokens
}

func addRecordToDaily(summary *DailySummary, record Record) {
	summary.Requests++
	summary.InputTokens += record.InputTokens
	summary.OutputTokens += record.OutputTokens
	summary.TotalTokens += recordTotal(record)
	summary.CachedInputTokens += record.CachedInputTokens
}

func recordTotal(record Record) int {
	if record.TotalTokens != 0 {
		return record.TotalTokens
	}
	return record.InputTokens + record.OutputTokens
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

func dailySeriesDays(window Window, now time.Time) []DailySummary {
	count := 30
	switch window {
	case WindowToday:
		count = 1
	case Window7D:
		count = 7
	case Window30D, WindowAll:
		count = 30
	}
	start := startOfDay(now.In(now.Location())).AddDate(0, 0, -(count - 1))
	days := make([]DailySummary, 0, count)
	for i := 0; i < count; i++ {
		days = append(days, DailySummary{Day: start.AddDate(0, 0, i)})
	}
	return days
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func dayKey(t time.Time) string {
	return startOfDay(t).Format("2006-01-02")
}

func hourKey(t time.Time) string {
	t = t.Truncate(time.Hour)
	return t.Format("2006-01-02 15")
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
