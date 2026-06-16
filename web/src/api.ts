export type PromptIssue = {
  severity: 'error' | 'warning';
  active: boolean;
  message: string;
};

export type PromptPreset = {
  name: string;
  description: string;
  file: string;
  mode: 'append' | 'replace';
  content?: string;
};

export type PromptBinding = {
  integration: string;
  model: string;
  preset: string;
  enabled: boolean;
};

export type PromptsResponse = {
  enabled: boolean;
  presets: PromptPreset[];
  bindings: PromptBinding[];
  issues: PromptIssue[];
};

export type Profile = {
  name: string;
  provider_type?: string;
  openai_base_url: string;
  api_key?: string;
  clear_api_key?: boolean;
  openai_api_type: string;
  model_list_url: string;
  models: string[];
  default_model: string;
  has_api_key: boolean;
  anthropic_base_url?: string;
};

export type ProfilesResponse = {
  default_profile: string;
  profiles: Profile[];
};

type APIError = {
  error?: {
    code?: string;
    message?: string;
  };
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers
    }
  });
  if (!response.ok) {
    let message = response.statusText;
    try {
      const body = (await response.json()) as APIError;
      message = body.error?.message || message;
    } catch {
      // Keep the HTTP status text when the response is not JSON.
    }
    throw new Error(message);
  }
  return (await response.json()) as T;
}

const encode = encodeURIComponent;

export const api = {
  getPrompts: () => request<PromptsResponse>('/api/prompts'),
  setPromptsEnabled: (enabled: boolean) =>
    request<PromptsResponse>('/api/prompts/enabled', { method: 'PUT', body: JSON.stringify({ enabled }) }),
  createPreset: (preset: PromptPreset) =>
    request<PromptsResponse>('/api/prompts/presets', { method: 'POST', body: JSON.stringify(preset) }),
  updatePreset: (name: string, preset: PromptPreset) =>
    request<PromptsResponse>(`/api/prompts/presets/${encode(name)}`, { method: 'PUT', body: JSON.stringify(preset) }),
  deletePreset: (name: string) => request<PromptsResponse>(`/api/prompts/presets/${encode(name)}`, { method: 'DELETE' }),
  createBinding: (binding: PromptBinding) =>
    request<PromptsResponse>('/api/prompts/bindings', { method: 'POST', body: JSON.stringify(binding) }),
  updateBinding: (old: PromptBinding, binding: PromptBinding) =>
    request<PromptsResponse>(`/api/prompts/bindings/${encode(old.integration)}/${encode(old.model)}`, {
      method: 'PUT',
      body: JSON.stringify(binding)
    }),
  deleteBinding: (binding: PromptBinding) =>
    request<PromptsResponse>(`/api/prompts/bindings/${encode(binding.integration)}/${encode(binding.model)}`, { method: 'DELETE' }),
  validatePrompts: () => request<{ issues: PromptIssue[] }>('/api/prompts/validate', { method: 'POST' }),
  getProfiles: () => request<ProfilesResponse>('/api/profiles'),
  createProfile: (profile: Profile) =>
    request<ProfilesResponse>('/api/profiles', { method: 'POST', body: JSON.stringify(profile) }),
  updateProfile: (oldName: string, profile: Profile) =>
    request<ProfilesResponse>(`/api/profiles/${encode(oldName)}`, { method: 'PUT', body: JSON.stringify(profile) }),
  deleteProfile: (name: string) => request<ProfilesResponse>(`/api/profiles/${encode(name)}`, { method: 'DELETE' }),
  setDefaultProfile: (name: string) =>
    request<ProfilesResponse>('/api/profiles/default', { method: 'PUT', body: JSON.stringify({ name }) }),
  fetchModelsForProfile: (profile: Partial<Profile>) =>
    request<{ models: string[] }>('/api/profiles/fetch-models', { method: 'POST', body: JSON.stringify(profile) }),
  getCodexModels: () => request<{ models: string[] }>('/api/codex/models'),
  getCodexPrompt: (model: string) => request<{ prompt: string }>(`/api/codex/prompt?model=${encode(model)}`),
  getClaudeModels: () => request<{ models: string[] }>('/api/claude/models'),
  getClaudePrompt: (model: string) => request<{ prompt: string }>(`/api/claude/prompt?model=${encode(model)}`)
};
