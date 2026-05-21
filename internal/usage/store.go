package usage

import (
	"database/sql"
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

	_ "modernc.org/sqlite"
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
	return filepath.Join(filepath.Dir(configPath), "token_usage.db"), nil
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
	db, err := openStore(path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`
		INSERT INTO usage_records (
			timestamp_ms, client, model, stream,
			input_tokens, output_tokens, total_tokens, cached_input_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Timestamp.UnixMilli(),
		record.Client,
		record.Model,
		boolInt(record.Stream),
		record.InputTokens,
		record.OutputTokens,
		record.TotalTokens,
		record.CachedInputTokens,
	)
	return err
}

func Read(path string) ([]Record, error) {
	db, err := openStore(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT timestamp_ms, client, model, stream,
			input_tokens, output_tokens, total_tokens, cached_input_tokens
		FROM usage_records
		ORDER BY timestamp_ms ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []Record
	for rows.Next() {
		var record Record
		var timestampMs int64
		var stream int
		if err := rows.Scan(
			&timestampMs,
			&record.Client,
			&record.Model,
			&stream,
			&record.InputTokens,
			&record.OutputTokens,
			&record.TotalTokens,
			&record.CachedInputTokens,
		); err != nil {
			return nil, err
		}
		record.Timestamp = time.UnixMilli(timestampMs).UTC()
		record.Stream = stream != 0
		if record.TotalTokens == 0 && (record.InputTokens > 0 || record.OutputTokens > 0) {
			record.TotalTokens = record.InputTokens + record.OutputTokens
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func Count(path string) (int, error) {
	db, err := openStore(path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_records`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func QuerySummary(path string, window Window, now time.Time) (Summary, error) {
	db, err := openStore(path)
	if err != nil {
		return Summary{}, err
	}
	defer db.Close()
	return querySummary(db, window, now)
}

func QueryBreakdowns(path string, window Window, now time.Time) ([]Breakdown, error) {
	db, err := openStore(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return queryBreakdowns(db, window, now)
}

func QueryDailySeries(path string, window Window, now time.Time) ([]DailySummary, error) {
	db, err := openStore(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return queryDailySeries(db, window, now)
}

func QueryHourlySeriesForToday(path string, now time.Time) ([]DailySummary, error) {
	db, err := openStore(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return queryHourlySeriesForToday(db, now)
}

func QueryHeavyRequests(path string, window Window, now time.Time, limit int) ([]HeavyRequest, error) {
	db, err := openStore(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return queryHeavyRequests(db, window, now, limit)
}

func QueryWindows(path string, now time.Time) (map[Window]WindowData, int, error) {
	db, err := openStore(path)
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_records`).Scan(&count); err != nil {
		return nil, 0, err
	}
	out := make(map[Window]WindowData, len(Windows()))
	for _, window := range Windows() {
		summary, err := querySummary(db, window, now)
		if err != nil {
			return nil, 0, err
		}
		breakdowns, err := queryBreakdowns(db, window, now)
		if err != nil {
			return nil, 0, err
		}
		var series []DailySummary
		if window == WindowToday {
			series, err = queryHourlySeriesForToday(db, now)
		} else {
			series, err = queryDailySeries(db, window, now)
		}
		if err != nil {
			return nil, 0, err
		}
		heavy, err := queryHeavyRequests(db, window, now, 5)
		if err != nil {
			return nil, 0, err
		}
		out[window] = WindowData{
			Summary:       summary,
			Breakdowns:    breakdowns,
			Series:        series,
			HeavyRequests: heavy,
		}
	}
	return out, count, nil
}

type WindowData struct {
	Summary       Summary
	Breakdowns    []Breakdown
	Series        []DailySummary
	HeavyRequests []HeavyRequest
}

func openStore(path string) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("usage path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := initStore(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return db, nil
}

func initStore(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp_ms INTEGER NOT NULL,
			client TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			stream INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			cached_input_tokens INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_usage_records_timestamp
			ON usage_records(timestamp_ms);
		CREATE INDEX IF NOT EXISTS idx_usage_records_client_model_timestamp
			ON usage_records(client, model, timestamp_ms);
		CREATE INDEX IF NOT EXISTS idx_usage_records_total_timestamp
			ON usage_records(total_tokens DESC, timestamp_ms DESC);
	`)
	return err
}

func querySummary(db *sql.DB, window Window, now time.Time) (Summary, error) {
	where, args := windowWhere(window, now)
	summary := Summary{Window: window, Client: ClientAll()}
	err := db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(cached_input_tokens), 0)
		FROM usage_records`+where, args...).Scan(
		&summary.Requests,
		&summary.InputTokens,
		&summary.OutputTokens,
		&summary.TotalTokens,
		&summary.CachedInputTokens,
	)
	return summary, err
}

func queryBreakdowns(db *sql.DB, window Window, now time.Time) ([]Breakdown, error) {
	where, args := windowWhere(window, now)
	rows, err := db.Query(`
		SELECT
			CASE WHEN trim(client) = '' THEN 'unknown' ELSE lower(trim(client)) END AS client_display,
			CASE WHEN trim(model) = '' THEN 'unknown model' ELSE trim(model) END AS model_display,
			COUNT(*) AS requests,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			COALESCE(SUM(cached_input_tokens), 0) AS cached_input_tokens
		FROM usage_records`+where+`
		GROUP BY client_display, model_display
		ORDER BY total_tokens DESC, requests DESC, client_display ASC, model_display ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Breakdown{}
	for rows.Next() {
		var row Breakdown
		if err := rows.Scan(
			&row.Client,
			&row.Model,
			&row.Requests,
			&row.InputTokens,
			&row.OutputTokens,
			&row.TotalTokens,
			&row.CachedInputTokens,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func queryDailySeries(db *sql.DB, window Window, now time.Time) ([]DailySummary, error) {
	days := dailySeriesDays(window, now)
	if len(days) == 0 {
		return days, nil
	}
	start := days[0].Day
	end := days[len(days)-1].Day.AddDate(0, 0, 1)
	rows, err := db.Query(`
		SELECT timestamp_ms, input_tokens, output_tokens, total_tokens, cached_input_tokens
		FROM usage_records
		WHERE timestamp_ms >= ? AND timestamp_ms < ?`,
		start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := make(map[string]*DailySummary, len(days))
	for i := range days {
		byDay[dayKey(days[i].Day)] = &days[i]
	}
	for rows.Next() {
		record, err := scanSeriesRecord(rows)
		if err != nil {
			return nil, err
		}
		day := byDay[dayKey(record.Timestamp.In(now.Location()))]
		if day != nil {
			addRecordToDaily(day, record)
		}
	}
	return days, rows.Err()
}

func queryHourlySeriesForToday(db *sql.DB, now time.Time) ([]DailySummary, error) {
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
	rows, err := db.Query(`
		SELECT timestamp_ms, input_tokens, output_tokens, total_tokens, cached_input_tokens
		FROM usage_records
		WHERE timestamp_ms >= ? AND timestamp_ms < ?`,
		start.UnixMilli(), start.AddDate(0, 0, 1).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanSeriesRecord(rows)
		if err != nil {
			return nil, err
		}
		hour := byHour[hourKey(record.Timestamp.In(now.Location()))]
		if hour != nil {
			addRecordToDaily(hour, record)
		}
	}
	return out, rows.Err()
}

func queryHeavyRequests(db *sql.DB, window Window, now time.Time, limit int) ([]HeavyRequest, error) {
	if limit <= 0 {
		limit = 5
	}
	where, args := windowWhere(window, now)
	args = append(args, limit)
	rows, err := db.Query(`
		SELECT
			timestamp_ms,
			CASE WHEN trim(client) = '' THEN 'unknown' ELSE lower(trim(client)) END AS client_display,
			CASE WHEN trim(model) = '' THEN 'unknown model' ELSE trim(model) END AS model_display,
			stream, input_tokens, output_tokens, total_tokens, cached_input_tokens
		FROM usage_records`+where+`
		ORDER BY total_tokens DESC, timestamp_ms DESC
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HeavyRequest{}
	for rows.Next() {
		var row HeavyRequest
		var timestampMs int64
		var stream int
		if err := rows.Scan(
			&timestampMs,
			&row.Client,
			&row.Model,
			&stream,
			&row.InputTokens,
			&row.OutputTokens,
			&row.TotalTokens,
			&row.CachedInputTokens,
		); err != nil {
			return nil, err
		}
		row.Timestamp = time.UnixMilli(timestampMs).UTC()
		row.Stream = stream != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

type seriesScanner interface {
	Scan(dest ...any) error
}

func scanSeriesRecord(row seriesScanner) (Record, error) {
	var record Record
	var timestampMs int64
	err := row.Scan(
		&timestampMs,
		&record.InputTokens,
		&record.OutputTokens,
		&record.TotalTokens,
		&record.CachedInputTokens,
	)
	record.Timestamp = time.UnixMilli(timestampMs).UTC()
	return record, err
}

func windowWhere(window Window, now time.Time) (string, []any) {
	if now.IsZero() {
		now = time.Now()
	}
	switch window {
	case WindowToday:
		start := startOfDay(now.In(now.Location()))
		return " WHERE timestamp_ms >= ? AND timestamp_ms < ?", []any{start.UnixMilli(), start.AddDate(0, 0, 1).UnixMilli()}
	case Window7D:
		return " WHERE timestamp_ms >= ?", []any{now.AddDate(0, 0, -7).UnixMilli()}
	case Window30D:
		return " WHERE timestamp_ms >= ?", []any{now.AddDate(0, 0, -30).UnixMilli()}
	case WindowAll:
		return "", nil
	default:
		return " WHERE 0", nil
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func Summaries(path string, now time.Time) ([]Summary, error) {
	db, err := openStore(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	out := make([]Summary, 0, len(Windows()))
	for _, window := range Windows() {
		summary, err := querySummary(db, window, now)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, nil
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
	cacheCreation := intFromAny(usage["cache_creation_input_tokens"])
	cacheRead := intFromAny(usage["cache_read_input_tokens"])
	if cacheCreation != 0 || cacheRead != 0 {
		input += cacheCreation + cacheRead
	}
	total := intFromAny(usage["total_tokens"])
	if total == 0 && (input > 0 || output > 0) {
		total = input + output
	}
	cached := intFromAny(usage["cached_input_tokens"])
	if cached == 0 {
		cached = cacheRead
	}
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
	record, _ := RecordFromUsageMap(usage.Raw, model, stream, now)
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
	return record, true
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
