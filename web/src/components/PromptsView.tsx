import React, { useEffect, useState } from 'react';
import { Plus, MessageSquare, Link2, Search, Edit3, Trash2, FileText, Zap, Bot } from 'lucide-react';
import { api, PromptBinding, PromptPreset, PromptsResponse } from '../api';
import { Header } from './Header';
import { DataTable, tableCell } from './DataTable';
import { IssuePanel } from './IssuePanel';
import { PresetDrawer } from './PresetDrawer';
import { BindingDrawer } from './BindingDrawer';
import { Checkbox } from './ui/checkbox';

const emptyPreset: PromptPreset = { name: '', description: '', file: '', mode: 'append', content: '' };
const emptyBinding: PromptBinding = { integration: 'codex', model: '*', preset: '', enabled: true };

const input =
  'h-10 w-full rounded-lg border border-slate-200/80 bg-white px-3 py-2 text-sm text-slate-900 outline-none transition-all placeholder:text-slate-400 hover:border-slate-300 focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20';

const button =
  'inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-slate-200/80 bg-white px-4 text-sm font-medium text-slate-700 transition-all hover:bg-slate-50 hover:border-slate-300 active:scale-[0.98] disabled:pointer-events-none disabled:opacity-50';

const primaryButton =
  'inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-slate-900 px-4 text-sm font-medium text-white transition-all hover:bg-slate-800 active:scale-[0.98] disabled:pointer-events-none disabled:opacity-50';

const iconButton =
  'inline-flex h-8 w-8 items-center justify-center rounded-lg border border-slate-200/80 bg-white text-slate-500 transition-all hover:bg-slate-50 hover:border-slate-300 hover:text-slate-900 active:scale-95';

export function PromptsView() {
  const [data, setData] = useState<PromptsResponse | null>(null);
  const [error, setError] = useState('');
  const [presetDraft, setPresetDraft] = useState<PromptPreset | null>(null);
  const [presetOldName, setPresetOldName] = useState('');
  const [bindingDraft, setBindingDraft] = useState<PromptBinding | null>(null);
  const [bindingOld, setBindingOld] = useState<PromptBinding | null>(null);

  // Tab and search state
  const [activeTab, setActiveTab] = useState<'presets' | 'bindings'>('presets');
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedPresetName, setSelectedPresetName] = useState<string | null>(null);

  useEffect(() => {
    api.getPrompts().then(setData).catch((err: Error) => setError(err.message));
  }, []);

  const savePreset = async () => {
    if (!presetDraft) return;
    try {
      const next = presetOldName ? await api.updatePreset(presetOldName, presetDraft) : await api.createPreset(presetDraft);
      setData(next);
      setPresetDraft(null);
      setPresetOldName('');
      setError('');
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const saveBinding = async () => {
    if (!bindingDraft) return;
    try {
      const next = bindingOld ? await api.updateBinding(bindingOld, bindingDraft) : await api.createBinding(bindingDraft);
      setData(next);
      setBindingDraft(null);
      setBindingOld(null);
      setError('');
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const toggleBinding = async (binding: PromptBinding) => {
    try {
      const updated = { ...binding, enabled: !binding.enabled };
      const next = await api.updateBinding(binding, updated);
      setData(next);
      setError('');
    } catch (err) {
      setError((err as Error).message);
    }
  };

  if (!data) return <div className="p-8 text-sm text-slate-500 font-semibold">{error || 'Loading…'}</div>;

  // Filtering
  const filteredPresets = data.presets.filter(p =>
    p.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
    (p.description || '').toLowerCase().includes(searchQuery.toLowerCase()) ||
    (p.content || '').toLowerCase().includes(searchQuery.toLowerCase())
  );

  const filteredBindings = data.bindings.filter(b =>
    b.model.toLowerCase().includes(searchQuery.toLowerCase()) ||
    b.integration.toLowerCase().includes(searchQuery.toLowerCase()) ||
    b.preset.toLowerCase().includes(searchQuery.toLowerCase())
  );

  const toggleGlobalEnabled = async () => {
    try {
      const next = await api.setPromptsEnabled(!data.enabled);
      setData(next);
      setError('');
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <section className="mx-auto max-w-7xl">
      <Header title="Prompts" subtitle="System prompt presets and model assignment" error={error} />

      {/* Global Enable/Disable Toggle */}
      <div className="mb-6 flex items-center justify-between rounded-xl border border-slate-200/80 bg-white p-4 shadow-sm">
        <div>
          <h3 className="text-sm font-semibold text-slate-900">Prompt Injection</h3>
          <p className="text-xs text-slate-500 mt-0.5">
            {data.enabled
              ? 'System prompts will be injected according to bindings below'
              : 'All prompt injections are disabled. Bindings and presets are preserved.'}
          </p>
        </div>
        <label className="inline-flex items-center gap-2.5 cursor-pointer">
          <Checkbox
            checked={data.enabled}
            onCheckedChange={toggleGlobalEnabled}
          />
          <span className="text-sm font-medium text-slate-700">
            {data.enabled ? 'Enabled' : 'Disabled'}
          </span>
        </label>
      </div>

      {/* Clean tab navigation with integrated actions */}
      <div className="mb-8 flex items-center gap-8 border-b border-slate-200/60">
        <button
          onClick={() => setActiveTab('presets')}
          className={`relative flex items-center gap-2.5 pb-3.5 text-sm font-semibold transition-all ${
            activeTab === 'presets'
              ? 'text-slate-900'
              : 'text-slate-500 hover:text-slate-700'
          }`}
        >
          <MessageSquare size={16} strokeWidth={2.5} />
          Presets
          <span className={`rounded-md px-1.5 py-0.5 text-xs font-bold transition-all ${
            activeTab === 'presets' ? 'bg-slate-900 text-white' : 'bg-slate-100 text-slate-600'
          }`}>
            {data.presets.length}
          </span>
          {activeTab === 'presets' && (
            <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-slate-900" />
          )}
        </button>
        <button
          onClick={() => setActiveTab('bindings')}
          className={`relative flex items-center gap-2.5 pb-3.5 text-sm font-semibold transition-all ${
            activeTab === 'bindings'
              ? 'text-slate-900'
              : 'text-slate-500 hover:text-slate-700'
          }`}
        >
          <Link2 size={16} strokeWidth={2.5} />
          Bindings
          <span className={`rounded-md px-1.5 py-0.5 text-xs font-bold transition-all ${
            activeTab === 'bindings' ? 'bg-slate-900 text-white' : 'bg-slate-100 text-slate-600'
          }`}>
            {data.bindings.length}
          </span>
          {activeTab === 'bindings' && (
            <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-slate-900" />
          )}
        </button>

        {/* Right-aligned actions */}
        <div className="ml-auto flex items-center gap-3 pb-3.5">
          {/* Search */}
          <div className="relative">
            <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
            <input
              placeholder="Search..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="h-9 w-56 rounded-lg border border-slate-200/80 bg-white pl-9 pr-3 text-sm text-slate-900 outline-none transition-all placeholder:text-slate-400 hover:border-slate-300 focus:border-slate-400 focus:ring-2 focus:ring-slate-400/10"
            />
          </div>

          {/* Action button */}
          {activeTab === 'presets' ? (
            <button
              className={primaryButton}
              onClick={() => {
                setPresetOldName('');
                setPresetDraft({ ...emptyPreset });
              }}
            >
              <Plus size={16} strokeWidth={2.5} /> New Preset
            </button>
          ) : (
            <button
              className={primaryButton}
              onClick={() => {
                setBindingOld(null);
                setBindingDraft({ ...emptyBinding, preset: data.presets[0]?.name || '' });
              }}
            >
              <Plus size={16} strokeWidth={2.5} /> New Binding
            </button>
          )}
        </div>
      </div>

      {activeTab === 'presets' ? (
        filteredPresets.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-20 rounded-xl border border-dashed border-slate-200 bg-slate-50/30">
            <div className="w-14 h-14 rounded-full bg-slate-100 flex items-center justify-center mb-3 text-slate-400">
              <MessageSquare size={24} strokeWidth={2} />
            </div>
            <h3 className="text-sm font-semibold text-slate-900 mb-1">
              {searchQuery ? 'No matching presets' : 'No prompt presets yet'}
            </h3>
            <p className="text-xs text-slate-500 mb-5 text-center max-w-sm">
              {searchQuery ? 'Try adjusting your search query.' : 'Create your first prompt preset to configure system prompts.'}
            </p>
            {!searchQuery && (
              <button
                className={button}
                onClick={() => {
                  setPresetOldName('');
                  setPresetDraft({ ...emptyPreset });
                }}
              >
                <Plus size={16} /> Create Preset
              </button>
            )}
          </div>
        ) : (
          <div className="grid gap-4 sm:grid-cols-1 md:grid-cols-2">
            {filteredPresets.map((preset) => {
              const isExpanded = selectedPresetName === preset.name;
              return (
                <div
                  key={preset.name}
                  onClick={() => setSelectedPresetName(isExpanded ? null : preset.name)}
                  className={`group flex flex-col justify-between rounded-xl border bg-white p-5 transition-all duration-200 cursor-pointer hover:border-slate-300 hover:shadow-sm ${
                    isExpanded ? 'ring-2 ring-slate-900/5 border-slate-300 shadow-sm' : 'border-slate-200/80'
                  }`}
                >
                  <div>
                    {/* Card Header Line */}
                    <div className="flex items-start justify-between gap-3 mb-2.5">
                      <div className="flex items-center gap-2 truncate">
                        <h3 className="text-sm font-semibold text-slate-900 truncate" title={preset.name}>
                          {preset.name}
                        </h3>
                        <span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium border ${
                          preset.mode === 'replace'
                            ? 'bg-amber-50 text-amber-700 border-amber-200/60'
                            : 'bg-blue-50 text-blue-700 border-blue-200/60'
                        }`}>
                          {preset.mode === 'replace' ? 'Replace' : 'Append'}
                        </span>
                      </div>

                      {/* Actions */}
                      <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                        <button
                          className={iconButton}
                          title="Edit"
                          onClick={(e) => {
                            e.stopPropagation();
                            setPresetOldName(preset.name);
                            setPresetDraft({ ...preset, content: preset.content || '' });
                          }}
                        >
                          <Edit3 size={13} strokeWidth={2} />
                        </button>
                        <button
                          className={`${iconButton} hover:border-red-200 hover:bg-red-50 hover:text-red-600`}
                          title="Delete"
                          onClick={async (e) => {
                            e.stopPropagation();
                            if (confirm(`Delete preset "${preset.name}"?`)) {
                              try {
                                const next = await api.deletePreset(preset.name);
                                setData(next);
                              } catch (err) {
                                setError((err as Error).message);
                              }
                            }
                          }}
                        >
                          <Trash2 size={13} strokeWidth={2} />
                        </button>
                      </div>
                    </div>

                    {/* Description */}
                    {preset.description ? (
                      <p className="text-xs text-slate-600 line-clamp-2 mb-3 leading-relaxed">
                        {preset.description}
                      </p>
                    ) : (
                      <p className="text-xs text-slate-400 italic mb-3">
                        No description
                      </p>
                    )}

                    {/* File info */}
                    <div className="flex items-center gap-1.5 text-xs text-slate-500 mb-3">
                      <FileText size={12} />
                      <code className="text-[11px] font-mono truncate bg-slate-50 border border-slate-200/60 px-2 py-0.5 rounded text-slate-600">
                        {preset.file}
                      </code>
                    </div>

                    {/* Content preview */}
                    <div className="text-xs font-mono text-slate-600 bg-slate-50 rounded-lg border border-slate-200/60 px-3 py-2.5 line-clamp-2 leading-relaxed">
                      {preset.content || <span className="italic text-slate-400">(No content)</span>}
                    </div>
                  </div>

                  {/* Expansion full content */}
                  {isExpanded && (
                    <div className="mt-4 border-t border-slate-100 pt-4 fade-in" onClick={(e) => e.stopPropagation()}>
                      <div className="flex items-center justify-between mb-2">
                        <span className="text-[10px] font-semibold uppercase tracking-wider text-slate-500">Full Content</span>
                        <button
                          onClick={() => setSelectedPresetName(null)}
                          className="text-xs font-medium text-slate-500 hover:text-slate-900"
                        >
                          Collapse
                        </button>
                      </div>
                      <pre className="text-xs font-mono bg-slate-900 text-slate-200 rounded-lg p-4 whitespace-pre-wrap leading-relaxed max-h-60 overflow-auto border border-slate-800">
                        {preset.content || '(Empty content)'}
                      </pre>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )
      ) : (
        filteredBindings.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-20 rounded-xl border border-dashed border-slate-200 bg-slate-50/30">
            <div className="w-14 h-14 rounded-full bg-slate-100 flex items-center justify-center mb-3 text-slate-400">
              <Link2 size={24} strokeWidth={2} />
            </div>
            <h3 className="text-sm font-semibold text-slate-900 mb-1">
              {searchQuery ? 'No matching bindings' : 'No model bindings yet'}
            </h3>
            <p className="text-xs text-slate-500 mb-5 text-center max-w-sm">
              {searchQuery ? 'Try adjusting your search query.' : 'Create model bindings to associate prompt presets with specific platforms and models.'}
            </p>
            {!searchQuery && (
              <button
                className={button}
                onClick={() => {
                  setBindingOld(null);
                  setBindingDraft({ ...emptyBinding, preset: data.presets[0]?.name || '' });
                }}
              >
                <Plus size={16} /> Add Binding
              </button>
            )}
          </div>
        ) : (
          <DataTable headers={['Status', 'Integration', 'Target Model', 'Bound Preset', '']}>
            {filteredBindings.map((binding) => (
              <tr key={`${binding.integration}/${binding.model}`} className="transition hover:bg-slate-50">
                {/* Inline Toggle active/inactive */}
                <td className={tableCell}>
                  <button
                    onClick={() => toggleBinding(binding)}
                    className="flex items-center gap-2 cursor-pointer focus:outline-none group"
                    title={binding.enabled ? 'Click to disable' : 'Click to enable'}
                  >
                    {binding.enabled ? (
                      <span className="inline-flex items-center rounded-md bg-emerald-50 px-2 py-1 text-xs font-medium text-emerald-700 border border-emerald-200/60 group-hover:bg-emerald-100 transition-colors">
                        <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 mr-1.5 animate-pulse" />
                        Active
                      </span>
                    ) : (
                      <span className="inline-flex items-center rounded-md bg-slate-100 px-2 py-1 text-xs font-medium text-slate-600 border border-slate-200/60 group-hover:bg-slate-150 transition-colors">
                        <span className="w-1.5 h-1.5 rounded-full bg-slate-400 mr-1.5" />
                        Inactive
                      </span>
                    )}
                  </button>
                </td>
                {/* Integration */}
                <td className={`${tableCell} font-medium text-slate-900`}>
                  <span className="inline-flex items-center gap-1.5">
                    {binding.integration === 'codex' ? (
                      <>
                        <Zap size={14} className="text-amber-500" />
                        <span>Codex</span>
                      </>
                    ) : (
                      <>
                        <Bot size={14} className="text-indigo-500" />
                        <span>Claude</span>
                      </>
                    )}
                  </span>
                </td>
                {/* Target Model */}
                <td className={tableCell}>
                  <code className="text-xs bg-slate-50 border border-slate-200/60 text-slate-700 px-2.5 py-1 rounded-md font-mono font-medium">
                    {binding.model}
                  </code>
                </td>
                {/* Bound Preset */}
                <td className={tableCell}>
                  <span className="inline-flex items-center gap-1.5 rounded-md border border-blue-200/60 bg-blue-50 px-2.5 py-1 text-xs font-medium text-blue-700">
                    {binding.preset}
                  </span>
                </td>
                {/* Action buttons */}
                <td className={`${tableCell} w-20 text-right`}>
                  <div className="flex items-center justify-end gap-1">
                    <button
                      className={iconButton}
                      title="Edit"
                      onClick={() => {
                        setBindingOld(binding);
                        setBindingDraft({ ...binding });
                      }}
                    >
                      <Edit3 size={13} strokeWidth={2} />
                    </button>
                    <button
                      className={`${iconButton} hover:border-red-200 hover:bg-red-50 hover:text-red-600`}
                      title="Delete"
                      onClick={async () => {
                        if (confirm(`Delete binding for ${binding.integration}/${binding.model}?`)) {
                          try {
                            const next = await api.deleteBinding(binding);
                            setData(next);
                          } catch (err) {
                            setError((err as Error).message);
                          }
                        }
                      }}
                    >
                      <Trash2 size={13} strokeWidth={2} />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </DataTable>
        )
      )}

      <IssuePanel data={data} />
      {presetDraft && <PresetDrawer draft={presetDraft} setDraft={setPresetDraft} onClose={() => setPresetDraft(null)} onSave={savePreset} />}
      {bindingDraft && <BindingDrawer draft={bindingDraft} setDraft={setBindingDraft} presets={data.presets} onClose={() => setBindingDraft(null)} onSave={saveBinding} />}
    </section>
  );
}
