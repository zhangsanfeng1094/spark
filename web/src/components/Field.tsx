import React from 'react';

interface FieldProps {
  label: string;
  children: React.ReactNode;
}

export function Field({ label, children }: FieldProps) {
  return (
    <label className="grid gap-2 text-xs font-semibold uppercase tracking-wider text-slate-400">
      {label}
      {children}
    </label>
  );
}
