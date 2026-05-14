package compatir

type Usage struct {
	InputTokens              int
	OutputTokens             int
	TotalTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	Raw                      map[string]any
}

func (u Usage) Add(next Usage) Usage {
	out := Usage{
		InputTokens:              u.InputTokens + next.InputTokens,
		OutputTokens:             u.OutputTokens + next.OutputTokens,
		TotalTokens:              u.TotalTokens + next.TotalTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens + next.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens + next.CacheReadInputTokens,
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = out.InputTokens + out.OutputTokens
	}
	if len(u.Raw) > 0 || len(next.Raw) > 0 {
		out.Raw = make(map[string]any, len(u.Raw)+len(next.Raw))
		for k, v := range u.Raw {
			out.Raw[k] = v
		}
		for k, v := range next.Raw {
			out.Raw[k] = v
		}
	}
	return out
}
