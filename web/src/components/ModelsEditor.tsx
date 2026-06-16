import React, { useState } from 'react';
import { Plus, X, RefreshCw } from 'lucide-react';
import { Field } from './Field';
import { api, Profile } from '../api';
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

interface ModelsEditorProps {
  profile: Profile;
  setProfile: (p: Profile) => void;
}

export function ModelsEditor({ profile, setProfile }: ModelsEditorProps) {
  const [model, setModel] = useState('');
  const [fetching, setFetching] = useState(false);
  const [fetchError, setFetchError] = useState('');

  const addModel = () => {
    const value = model.trim();
    if (!value || profile.models.includes(value)) return;
    const models = [...profile.models, value];
    setProfile({ ...profile, models, default_model: profile.default_model || value });
    setModel('');
  };

  const fetchFromBaseURL = async () => {
    setFetching(true);
    setFetchError('');
    try {
      const result = await api.fetchModelsForProfile({
        name: profile.name,
        openai_base_url: profile.openai_base_url,
        openai_api_type: profile.openai_api_type,
        model_list_url: profile.model_list_url,
        anthropic_base_url: profile.anthropic_base_url,
        api_key: profile.api_key
      });
      if (result.models && result.models.length > 0) {
        setProfile({
          ...profile,
          models: result.models,
          default_model: profile.default_model || result.models[0]
        });
      } else {
        setFetchError('No models returned from API');
      }
    } catch (err) {
      setFetchError((err as Error).message);
    } finally {
      setFetching(false);
    }
  };

  return (
    <div className="grid gap-4.5 md:col-span-2">
      <Field label="Models">
        <div className="grid grid-cols-[minmax(0,1fr)_auto_auto] gap-2.5">
          <input
            className={input}
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder="e.g. gpt-4o-mini"
            onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addModel(); } }}
          />
          <button className={button} type="button" onClick={addModel}>
            <Plus size={15} className="text-indigo-500" /> Add
          </button>
          <button
            className={button}
            type="button"
            onClick={fetchFromBaseURL}
            disabled={fetching || !profile.openai_base_url}
            title="Fetch models from the current base URL"
          >
            <RefreshCw size={15} className={fetching ? 'animate-spin' : ''} />
            {fetching ? 'Fetching...' : 'Fetch Models'}
          </button>
        </div>
        {fetchError && (
          <div className="text-xs text-red-600 mt-1">{fetchError}</div>
        )}
      </Field>
      <Field label="Default model">
        <Select value={profile.default_model || undefined} onValueChange={(value) => setProfile({ ...profile, default_model: value })}>
          <SelectTrigger>
            <SelectValue placeholder="— none —" />
          </SelectTrigger>
          <SelectContent>
            {profile.models.map((m) => (
              <SelectItem key={m} value={m}>
                {m}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <div className="flex flex-wrap gap-2">
        {profile.models.map((m) => (
          <span key={m} className="inline-flex max-w-full items-center gap-1.5 rounded-full border border-slate-200 bg-slate-50/70 py-1 pl-3.5 pr-1.5 text-sm font-semibold text-slate-700 shadow-sm transition-all hover:border-slate-300">
            <span className="truncate">{m}</span>
            <button
              className="inline-flex h-5 w-5 items-center justify-center rounded-full text-slate-400 transition hover:bg-slate-250 hover:text-slate-800"
              type="button"
              title="Remove"
              onClick={() => setProfile({
                ...profile,
                models: profile.models.filter((x) => x !== m),
                default_model: profile.default_model === m ? '' : profile.default_model
              })}
            >
              <X size={12} />
            </button>
          </span>
        ))}
      </div>
    </div>
  );
}
