package proxy

import usagepkg "spark/internal/usage"

func installUsageRecorder(client, fallbackModel string, logf func(format string, args ...any)) func() {
	return usagepkg.ReplaceRecorder(func(record usagepkg.Record) error {
		if record.Client == "" {
			record.Client = client
		}
		if record.Model == "" {
			record.Model = fallbackModel
		}
		err := usagepkg.AppendDefault(record)
		if err != nil && logf != nil {
			logf("token usage record failed: %v", err)
		}
		return err
	})
}
