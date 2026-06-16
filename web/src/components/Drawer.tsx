import React from 'react';
import { X, Save } from 'lucide-react';

interface DrawerProps {
  title: string;
  children: React.ReactNode;
  onClose: () => void;
  onSave: () => void;
}

const button =
  'inline-flex min-h-10 items-center justify-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2 text-sm font-semibold text-slate-700 shadow-sm transition-all hover:border-slate-300 hover:bg-slate-50 hover:text-slate-900 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50';

const primaryButton =
  'inline-flex min-h-10 items-center justify-center gap-2 rounded-xl bg-slate-900 px-4 py-2 text-sm font-semibold text-white shadow-md shadow-slate-900/10 transition-all hover:bg-slate-800 hover:shadow-lg active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50';

const iconButton =
  'inline-flex h-9 w-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-500 shadow-sm transition-all hover:border-slate-300 hover:bg-slate-50 hover:text-slate-900';

export function Drawer({ title, children, onClose, onSave }: DrawerProps) {
  return (
    <>
      <div className="fixed inset-0 z-40 bg-slate-900/30 backdrop-blur-sm transition-all duration-200" onClick={onClose} />
      <div className="fixed right-0 top-0 z-50 grid h-screen w-full grid-rows-[auto_1fr_auto] border-l border-slate-200/80 bg-white/95 shadow-2xl shadow-slate-900/20 backdrop-blur-xl sm:w-[560px] slide-in">
        <div className="flex items-center justify-between border-b border-slate-100 px-6 py-5">
          <h2 className="text-lg font-bold tracking-tight text-slate-900">{title}</h2>
          <button className={iconButton} title="Close" onClick={onClose}><X size={16} /></button>
        </div>
        <div className="grid content-start gap-5 overflow-auto p-6">{children}</div>
        <div className="flex justify-end gap-2.5 border-t border-slate-100 bg-slate-50/50 px-6 py-5">
          <button className={button} onClick={onClose}>Cancel</button>
          <button className={primaryButton} onClick={onSave}>
            <Save size={15} /> Save
          </button>
        </div>
      </div>
    </>
  );
}
