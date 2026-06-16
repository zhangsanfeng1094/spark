import React from 'react';
import { AlertTriangle } from 'lucide-react';

interface HeaderProps {
  title: string;
  subtitle?: string;
  error: string;
}

export function Header({ title, subtitle, error }: HeaderProps) {
  return (
    <div className="mb-8 flex flex-wrap items-end justify-between gap-4 border-b border-slate-100 pb-5">
      <div>
        <h1 className="text-3xl font-extrabold tracking-tight text-slate-900 bg-gradient-to-r from-slate-900 to-slate-700 bg-clip-text text-transparent">
          {title}
        </h1>
        {subtitle && <p className="mt-1.5 text-sm font-semibold text-slate-400">{subtitle}</p>}
      </div>
      {error && (
        <div className="flex items-center gap-2 rounded-xl border border-red-200 bg-red-50/50 px-4 py-2.5 text-sm font-semibold text-red-800 shadow-sm backdrop-blur-sm animate-shake">
          <AlertTriangle size={16} className="text-red-500" />
          {error}
        </div>
      )}
    </div>
  );
}
