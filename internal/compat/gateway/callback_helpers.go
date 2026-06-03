package gateway

func callLogf(logf func(string, ...any), format string, args ...any) {
	if logf != nil {
		logf(format, args...)
	}
}

func callWarnf(warnf func(string), summary string) {
	if warnf != nil {
		warnf(summary)
	}
}
