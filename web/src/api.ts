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
  getProfiles: () => request<ProfilesResponse>('/api/profiles'),
  createProfile: (profile: Profile) =>
    request<ProfilesResponse>('/api/profiles', { method: 'POST', body: JSON.stringify(profile) }),
  updateProfile: (oldName: string, profile: Profile) =>
    request<ProfilesResponse>(`/api/profiles/${encode(oldName)}`, { method: 'PUT', body: JSON.stringify(profile) }),
  deleteProfile: (name: string) => request<ProfilesResponse>(`/api/profiles/${encode(name)}`, { method: 'DELETE' }),
  setDefaultProfile: (name: string) =>
    request<ProfilesResponse>('/api/profiles/default', { method: 'PUT', body: JSON.stringify({ name }) }),
  fetchModelsForProfile: (profile: Partial<Profile>) =>
    request<{ models: string[] }>('/api/profiles/fetch-models', { method: 'POST', body: JSON.stringify(profile) })
};
