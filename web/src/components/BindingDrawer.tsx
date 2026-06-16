import React from 'react';
import { Modal } from './Modal';
import { Field } from './Field';
import { PromptBinding, PromptPreset } from '../api';
import { Checkbox } from './ui/checkbox';
import { Save } from 'lucide-react';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './ui/select';

const input =
  'h-10 w-full rounded-lg border border-slate-200/80 bg-white px-3 py-2 text-sm text-slate-900 outline-none transition-all placeholder:text-slate-400 hover:border-slate-300 focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20';

const button =
  'inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-slate-200/80 bg-white px-4 text-sm font-medium text-slate-700 transition-all hover:bg-slate-50 hover:border-slate-300 active:scale-[0.98] disabled:pointer-events-none disabled:opacity-50';

const primaryButton =
  'inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-slate-900 px-4 text-sm font-medium text-white transition-all hover:bg-slate-800 active:scale-[0.98] disabled:pointer-events-none disabled:opacity-50';

interface BindingDrawerProps {
  draft: PromptBinding;
  setDraft: (b: PromptBinding) => void;
  presets: PromptPreset[];
  onClose: () => void;
  onSave: () => void;
}

export function BindingDrawer({ draft, setDraft, presets, onClose, onSave }: BindingDrawerProps) {
  return (
    <Modal isOpen={true} onClose={onClose} title="Model Binding">
      <div className="grid gap-4">
        <Field label="Integration">
          <Select value={draft.integration} onValueChange={(value) => setDraft({ ...draft, integration: value })}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="codex">codex</SelectItem>
              <SelectItem value="claude">claude</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        <Field label="Model">
          <input className={input} value={draft.model} onChange={(e) => setDraft({ ...draft, model: e.target.value })} placeholder="e.g., gpt-4 or *" />
        </Field>
        <Field label="Preset">
          <Select value={draft.preset} onValueChange={(value) => setDraft({ ...draft, preset: value })}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {presets.map((p) => (
                <SelectItem key={p.name} value={p.name}>
                  {p.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <label className="inline-flex items-center gap-2.5 text-sm font-medium text-slate-700 cursor-pointer">
          <Checkbox
            checked={draft.enabled}
            onCheckedChange={(checked) => setDraft({ ...draft, enabled: checked as boolean })}
          />
          Enabled
        </label>

        {/* Modal Footer Actions */}
        <div className="flex items-center justify-end gap-2 border-t border-slate-200/60 pt-4 mt-1">
          <button className={button} onClick={onClose}>
            Cancel
          </button>
          <button className={primaryButton} onClick={onSave}>
            <Save size={15} /> Save Binding
          </button>
        </div>
      </div>
    </Modal>
  );
}
