import React, { useEffect, useMemo, useState } from 'react';
import { Plus, Save, Trash2, Zap, Globe, Bot, Gem, Rabbit } from 'lucide-react';
import { api, Profile, ProfilesResponse } from '../api';
import { Header } from './Header';
import { Field } from './Field';
import { Badge } from './Badge';
import { ModelsEditor } from './ModelsEditor';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './ui/select';

const input =
  'min-h-10 w-full rounded-xl border border-slate-200 bg-white px-3.5 py-2 text-sm text-slate-900 shadow-sm outline-none transition-all placeholder:text-slate-400 focus:border-indigo-500 focus:ring-4 focus:ring-indigo-500/5';

const button =
  'inline-flex min-h-10 items-center justify-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2 text-sm font-semibold text-slate-700 shadow-sm transition-all hover:border-slate-300 hover:bg-slate-50 hover:text-slate-900 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50';

const primaryButton =
  'inline-flex min-h-10 items-center justify-center gap-2 rounded-xl bg-slate-900 px-4 py-2 text-sm font-semibold text-white shadow-md shadow-slate-900/10 transition-all hover:bg-slate-800 hover:shadow-lg active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50';

const dangerButton =
  'inline-flex min-h-10 items-center justify-center gap-2 rounded-xl border border-red-200 bg-white px-4 py-2 text-sm font-semibold text-red-600 shadow-sm transition-all hover:bg-red-50 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50';

const templates: Record<string, Partial<Profile>> = {
  OpenAI: { openai_base_url: 'https://api.openai.com/v1', openai_api_type: 'responses,chat_completions', models: [] },
  'OpenAI Compatible': { openai_base_url: '', openai_api_type: 'responses,chat_completions', models: [] },
  Anthropic: {
    openai_base_url: 'https://api.anthropic.com',
    openai_api_type: 'anthropic_messages',
    models: ['claude-sonnet-4-20250514'],
    default_model: 'claude-sonnet-4-20250514'
  },
  Gemini: {
    openai_base_url: 'https://generativelanguage.googleapis.com/v1beta',
    openai_api_type: 'gemini_generate_content',
    models: ['gemini-2.5-flash'],
    default_model: 'gemini-2.5-flash'
  },
  Ollama: { openai_base_url: 'http://localhost:11434/v1', openai_api_type: 'responses,chat_completions', models: [] }
};

const providerIcons: Record<string, React.ReactNode> = {
  OpenAI: <Zap size={20} />,
  'OpenAI Compatible': <Globe size={20} />,
  Anthropic: <Bot size={20} />,
  Gemini: <Gem size={20} />,
  Ollama: <Rabbit size={20} />
};

function makeProfile(provider: string, name: string): Profile {
  const template = templates[provider];
  return {
    name,
    provider_type: provider,
    openai_base_url: template.openai_base_url || '',
    openai_api_type: template.openai_api_type || 'responses,chat_completions',
    model_list_url: '',
    models: template.models || [],
    default_model: template.default_model || '',
    has_api_key: false,
    api_key: ''
  };
}

function uniqueName(names: string[], base: string) {
  if (!names.includes(base)) return base;
  for (let i = 2; ; i += 1) {
    const candidate = `${base}-${i}`;
    if (!names.includes(candidate)) return candidate;
  }
}

function selectAfterSave(next: ProfilesResponse, name: string, setSelected: (v: string) => void, setDraft: (v: Profile) => void, setOldName: (v: string) => void) {
  const profile = next.profiles.find((p) => p.name === name) || next.profiles[0];
  if (!profile) return;
  setSelected(profile.name);
  setDraft({ ...profile, api_key: '', clear_api_key: false });
  setOldName(profile.name);
}

export function ProfilesView() {
  const [data, setData] = useState<ProfilesResponse | null>(null);
  const [selected, setSelected] = useState('');
  const [draft, setDraft] = useState<Profile | null>(null);
  const [oldName, setOldName] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    api.getProfiles().then((next) => {
      setData(next);
      const first = next.default_profile || next.profiles[0]?.name || '';
      setSelected(first);
      const profile = next.profiles.find((p) => p.name === first) || next.profiles[0];
      if (profile) {
        setDraft({ ...profile, api_key: '' });
        setOldName(profile.name);
      }
    }).catch((err: Error) => setError(err.message));
  }, []);

  const profileNames = useMemo(() => data?.profiles.map((p) => p.name) || [], [data]);

  const selectProfile = (name: string) => {
    const profile = data?.profiles.find((p) => p.name === name);
    if (!profile) return;
    setSelected(name);
    setDraft({ ...profile, api_key: '', clear_api_key: false });
    setOldName(profile.name);
  };

  const saveProfile = async () => {
    if (!draft) return;
    try {
      const payload = { ...draft, api_key: draft.api_key?.trim() ? draft.api_key : undefined };
      const next = oldName ? await api.updateProfile(oldName, payload) : await api.createProfile(payload);
      setData(next);
      selectAfterSave(next, payload.name, setSelected, setDraft, setOldName);
      setError('');
    } catch (err) {
      setError((err as Error).message);
    }
  };

  if (!data || !draft) return <div className="p-8 text-sm text-slate-500 font-semibold">{error || 'Loading…'}</div>;

  return (
    <section className="mx-auto grid max-w-7xl grid-cols-1 items-start gap-6 md:grid-cols-[260px_minmax(0,1fr)]">
      <div className="md:col-span-2">
        <Header title="Profiles" subtitle="Configure providers, models and API credentials" error={error} />
      </div>

      <div className="grid gap-1.5 rounded-2xl border border-slate-200 bg-white p-3 shadow-sm">
        <button
          className={`${primaryButton} mb-2 w-full`}
          onClick={() => {
            const profile = makeProfile('OpenAI Compatible', uniqueName(profileNames, 'profile'));
            setOldName('');
            setDraft(profile);
            setSelected(profile.name);
          }}
        >
          <Plus size={15} /> New Profile
        </button>
        <div className="space-y-1">
          {data.profiles.map((profile) => (
            <button
              key={profile.name}
              className={`flex min-h-10 w-full items-center justify-between rounded-xl px-3.5 py-2 text-left text-sm font-semibold transition-all ${
                selected === profile.name
                  ? 'bg-slate-900 text-white shadow-sm scale-[1.01]'
                  : 'text-slate-600 hover:bg-slate-50 hover:text-slate-900'
              }`}
              onClick={() => selectProfile(profile.name)}
            >
              <span className="truncate">{profile.name}</span>
              {data.default_profile === profile.name && (
                <Badge tone={selected === profile.name ? 'invert' : 'sky'}>default</Badge>
              )}
            </button>
          ))}
        </div>
      </div>

      <div className="grid gap-6">
        <form
          className="grid gap-5 rounded-2xl border border-slate-200 bg-white p-6 shadow-sm md:grid-cols-2"
          onSubmit={(e) => { e.preventDefault(); void saveProfile(); }}
        >
          <Field label="Name">
            <input className={input} value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
          </Field>
          <Field label="Provider">
            <Select
              value={draft.provider_type || 'OpenAI Compatible'}
              onValueChange={(value) => setDraft({ ...draft, ...templates[value], provider_type: value })}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.keys(templates).map((name) => (
                  <SelectItem key={name} value={name}>
                    {name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label="Base URL">
            <input className={input} value={draft.openai_base_url} onChange={(e) => setDraft({ ...draft, openai_base_url: e.target.value })} />
          </Field>
          <Field label="API Type">
            <input className={input} value={draft.openai_api_type} onChange={(e) => setDraft({ ...draft, openai_api_type: e.target.value })} />
          </Field>
          <Field label="Models URL">
            <input className={input} value={draft.model_list_url} onChange={(e) => setDraft({ ...draft, model_list_url: e.target.value })} />
          </Field>
          <Field label="API Key">
            <input
              className={input}
              type="password"
              placeholder={draft.has_api_key ? '•••••••• stored' : 'Enter API key'}
              value={draft.api_key || ''}
              onChange={(e) => setDraft({ ...draft, api_key: e.target.value, clear_api_key: false })}
            />
          </Field>
          <div className="md:col-span-2">
            <button
              className={button}
              type="button"
              onClick={() => setDraft({ ...draft, api_key: '', clear_api_key: true, has_api_key: false })}
            >
              Clear key
            </button>
          </div>

          <ModelsEditor profile={draft} setProfile={setDraft} />

          <div className="flex flex-wrap justify-end gap-2 border-t border-slate-100 pt-5 md:col-span-2">
            <button
              className={button}
              type="button"
              onClick={async () => { const next = await api.setDefaultProfile(draft.name); setData(next); }}
              disabled={!oldName}
            >
              Set default
            </button>
            <button
              className={dangerButton}
              type="button"
              onClick={async () => {
                const next = await api.deleteProfile(oldName);
                setData(next);
                selectAfterSave(next, next.default_profile, setSelected, setDraft, setOldName);
              }}
              disabled={!oldName}
            >
              <Trash2 size={15} /> Delete
            </button>
            <button className={primaryButton} type="submit"><Save size={15} /> Save</button>
          </div>
        </form>
      </div>
    </section>
  );
}
