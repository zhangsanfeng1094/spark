import React from 'react';
import { Download, FileText, Save } from 'lucide-react';
import { Modal } from './Modal';
import { Field } from './Field';
import { PromptPreset, api } from '../api';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './ui/select';

type PromptTemplateKey = 'codex/gpt-5' | 'claude/claude-sonnet-4-20250514' | 'claude/claude-3-opus';

const promptTemplateOptions: Array<{ integration: string; model: string; key: PromptTemplateKey }> = [
  { integration: 'codex', model: 'gpt-5', key: 'codex/gpt-5' },
  { integration: 'claude', model: 'claude-sonnet-4-20250514', key: 'claude/claude-sonnet-4-20250514' },
  { integration: 'claude', model: 'claude-3-opus', key: 'claude/claude-3-opus' }
];

const promptTemplates: Record<PromptTemplateKey, string> = {
  'codex/gpt-5': `You are Codex, a coding agent based on GPT-5. You are working inside a local repository with the user.

Follow the repository's existing patterns, keep changes scoped, and verify the result before handing work back.

[Add your changes below]
`,
  'claude/claude-sonnet-4-20250514': `You are Claude, an AI assistant created by Anthropic.

You should be helpful, harmless, and honest. Answer the user's request directly and adapt to their context.

[Add your changes below]
`,
  'claude/claude-3-opus': `You are Claude, an AI assistant created by Anthropic.

The current date is {{date}}.

Be thoughtful, precise, and transparent about uncertainty when it matters.

[Add your changes below]
`
};

const input =
  'h-10 w-full rounded-lg border border-slate-200/80 bg-white px-3 py-2 text-sm text-slate-900 outline-none transition-all placeholder:text-slate-400 hover:border-slate-300 focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20';

const button =
  'inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-slate-200/80 bg-white px-4 text-sm font-medium text-slate-700 transition-all hover:bg-slate-50 hover:border-slate-300 active:scale-[0.98] disabled:pointer-events-none disabled:opacity-50';

const primaryButton =
  'inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-slate-900 px-4 text-sm font-medium text-white transition-all hover:bg-slate-800 active:scale-[0.98] disabled:pointer-events-none disabled:opacity-50';

interface PresetDrawerProps {
  draft: PromptPreset;
  setDraft: (p: PromptPreset) => void;
  onClose: () => void;
  onSave: () => void;
}

export function PresetDrawer({ draft, setDraft, onClose, onSave }: PresetDrawerProps) {
  const [templateSource, setTemplateSource] = React.useState<'blank' | 'model'>((draft.content || '').trim() ? 'model' : 'blank');
  const [templateKey, setTemplateKey] = React.useState<PromptTemplateKey>('claude/claude-sonnet-4-20250514');
  const [codexModels, setCodexModels] = React.useState<string[]>([]);
  const [selectedCodexModel, setSelectedCodexModel] = React.useState<string>('');
  const [claudeModels, setClaudeModels] = React.useState<string[]>([]);
  const [selectedClaudeModel, setSelectedClaudeModel] = React.useState<string>('');
  const [fetching, setFetching] = React.useState(false);
  const [fetchError, setFetchError] = React.useState('');
  const selectedTemplate = promptTemplateOptions.find((option) => option.key === templateKey) || promptTemplateOptions[0];

  const integrations = [...new Set(promptTemplateOptions.map((option) => option.integration))];

  // Load Codex models when component mounts
  React.useEffect(() => {
    const loadCodexModels = async () => {
      try {
        const result = await api.getCodexModels();
        setCodexModels(result.models);
        if (result.models.length > 0) {
          setSelectedCodexModel(result.models[0]);
        }
      } catch (err) {
        console.error('Failed to load Codex models:', err);
      }
    };
    loadCodexModels();
  }, []);

  // Load Claude models when component mounts
  React.useEffect(() => {
    const loadClaudeModels = async () => {
      try {
        const result = await api.getClaudeModels();
        setClaudeModels(result.models);
        if (result.models.length > 0) {
          setSelectedClaudeModel(result.models[0]);
        }
      } catch (err) {
        console.error('Failed to load Claude models:', err);
      }
    };
    loadClaudeModels();
  }, []);

  const fetchTemplate = async () => {
    if (selectedTemplate.integration === 'codex') {
      // Fetch from Codex API
      if (!selectedCodexModel) {
        setFetchError('No Codex model selected');
        return;
      }
      setFetching(true);
      setFetchError('');
      try {
        const result = await api.getCodexPrompt(selectedCodexModel);
        setDraft({
          ...draft,
          mode: 'replace',
          content: result.prompt,
          description: draft.description || `Based on ${selectedCodexModel} system prompt from Codex.`
        });
      } catch (err) {
        setFetchError((err as Error).message);
      } finally {
        setFetching(false);
      }
    } else if (selectedTemplate.integration === 'claude') {
      // Fetch from Claude API
      if (!selectedClaudeModel) {
        setFetchError('No Claude model selected');
        return;
      }
      setFetching(true);
      setFetchError('');
      try {
        const result = await api.getClaudePrompt(selectedClaudeModel);
        setDraft({
          ...draft,
          mode: 'replace',
          content: result.prompt,
          description: draft.description || `Based on ${selectedClaudeModel} system prompt from Claude.`
        });
      } catch (err) {
        setFetchError((err as Error).message);
      } finally {
        setFetching(false);
      }
    } else {
      // Use hardcoded template
      setDraft({
        ...draft,
        mode: 'replace',
        content: promptTemplates[templateKey],
        description: draft.description || `Based on ${selectedTemplate.model} default system prompt.`
      });
    }
  };

  return (
    <Modal isOpen={true} onClose={onClose} title={draft.name ? 'Edit Prompt Preset' : 'Create Prompt Preset'}>
      <div className="grid gap-4">
        <Field label="Name">
          <input className={input} value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} placeholder="Enter preset name" />
        </Field>
        <Field label="Description">
          <input className={input} value={draft.description} onChange={(e) => setDraft({ ...draft, description: e.target.value })} placeholder="Brief description of this preset" />
        </Field>

        <div className="grid gap-3 rounded-lg border border-slate-200/80 bg-slate-50/50 p-4">
          <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-slate-500">
            <FileText size={13} /> Prompt Content
          </div>
          <div className="inline-grid w-full grid-cols-2 rounded-lg border border-slate-200/80 bg-white p-1 gap-1">
            <button
              type="button"
              className={`rounded-md py-2 text-sm font-medium transition-all ${templateSource === 'blank' ? 'bg-slate-900 text-white' : 'text-slate-600 hover:text-slate-900 hover:bg-slate-50'}`}
              onClick={() => setTemplateSource('blank')}
            >
              Blank Template
            </button>
            <button
              type="button"
              className={`rounded-md py-2 text-sm font-medium transition-all ${templateSource === 'model' ? 'bg-slate-900 text-white' : 'text-slate-600 hover:text-slate-900 hover:bg-slate-50'}`}
              onClick={() => setTemplateSource('model')}
            >
              Fetch from Model
            </button>
          </div>
          {templateSource === 'model' && (
            <div className="grid gap-2">
              <div className="grid gap-2 sm:grid-cols-[1fr_1.4fr_auto]">
                <Select
                  value={selectedTemplate.integration}
                  onValueChange={(value) => {
                    const next = promptTemplateOptions.find((option) => option.integration === value) || promptTemplateOptions[0];
                    setTemplateKey(next.key);
                  }}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {integrations.map((integration) => (
                      <SelectItem key={integration} value={integration}>
                        {integration}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>

                <Select
                  value={
                    selectedTemplate.integration === 'codex'
                      ? selectedCodexModel
                      : selectedTemplate.integration === 'claude'
                      ? selectedClaudeModel
                      : templateKey
                  }
                  onValueChange={(value) => {
                    if (selectedTemplate.integration === 'codex') {
                      setSelectedCodexModel(value);
                    } else if (selectedTemplate.integration === 'claude') {
                      setSelectedClaudeModel(value);
                    } else {
                      setTemplateKey(value as PromptTemplateKey);
                    }
                  }}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {selectedTemplate.integration === 'codex'
                      ? codexModels.map((model) => (
                          <SelectItem key={model} value={model}>
                            {model}
                          </SelectItem>
                        ))
                      : selectedTemplate.integration === 'claude'
                      ? claudeModels.map((model) => (
                          <SelectItem key={model} value={model}>
                            {model}
                          </SelectItem>
                        ))
                      : promptTemplateOptions
                          .filter((option) => option.integration === selectedTemplate.integration)
                          .map((option) => (
                            <SelectItem key={option.key} value={option.key}>
                              {option.model}
                            </SelectItem>
                          ))
                    }
                  </SelectContent>
                </Select>

                <button
                  className={button}
                  type="button"
                  onClick={fetchTemplate}
                  disabled={fetching}
                >
                  <Download size={15} /> {fetching ? 'Fetching...' : 'Fetch'}
                </button>
              </div>
              {fetchError && (
                <div className="text-xs text-red-600">{fetchError}</div>
              )}
            </div>
          )}
        </div>

        <Field label="Content">
          <textarea
            className={`${input} min-h-64 resize-y font-mono text-xs leading-relaxed bg-slate-50/50 focus:bg-white`}
            value={draft.content || ''}
            onChange={(e) => setDraft({ ...draft, content: e.target.value })}
            placeholder="Enter your system prompt content here..."
          />
        </Field>

        <Field label="Injection Mode">
          <div className="grid gap-2">
            <button
              type="button"
              className={`rounded-lg border px-4 py-3 text-left transition-all ${draft.mode === 'replace' ? 'border-amber-300 bg-amber-50 text-slate-900' : 'border-slate-200/80 bg-white text-slate-600 hover:border-slate-300 hover:bg-slate-50'}`}
              onClick={() => setDraft({ ...draft, mode: 'replace' })}
            >
              <span className="block text-sm font-semibold">Replace</span>
              <span className="mt-0.5 block text-xs text-slate-600">Replace the model's original system prompt with this preset.</span>
            </button>
            <button
              type="button"
              className={`rounded-lg border px-4 py-3 text-left transition-all ${draft.mode === 'append' ? 'border-blue-300 bg-blue-50 text-slate-900' : 'border-slate-200/80 bg-white text-slate-600 hover:border-slate-300 hover:bg-slate-50'}`}
              onClick={() => setDraft({ ...draft, mode: 'append' })}
            >
              <span className="block text-sm font-semibold">Append</span>
              <span className="mt-0.5 block text-xs text-slate-600">Add this preset after the model's original system prompt.</span>
            </button>
          </div>
        </Field>

        <Field label="File">
          <input className={input} value={draft.file} onChange={(e) => setDraft({ ...draft, file: e.target.value })} placeholder="config/prompts/my-preset.txt" />
        </Field>

        {/* Modal Footer Actions */}
        <div className="flex items-center justify-end gap-2 border-t border-slate-200/60 pt-4 mt-1">
          <button className={button} onClick={onClose}>
            Cancel
          </button>
          <button className={primaryButton} onClick={onSave}>
            <Save size={15} /> Save Preset
          </button>
        </div>
      </div>
    </Modal>
  );
}
