import React from 'react';

export type BadgeTone = 'slate' | 'sky' | 'amber' | 'green' | 'red' | 'invert';

interface BadgeProps {
  children: React.ReactNode;
  tone?: BadgeTone;
}

export function Badge({ children, tone = 'slate' }: BadgeProps) {
  const tones: Record<BadgeTone, string> = {
    slate: 'bg-slate-100 text-slate-600 border-slate-200/60',
    sky: 'bg-indigo-50 text-indigo-700 border-indigo-100',
    amber: 'bg-amber-50 text-amber-700 border-amber-200/60',
    green: 'bg-emerald-50 text-emerald-700 border-emerald-200/60',
    red: 'bg-red-50 text-red-700 border-red-200/60',
    invert: 'bg-white/15 text-white border-white/20'
  };
  return (
    <span className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold ${tones[tone]}`}>
      {children}
    </span>
  );
}
