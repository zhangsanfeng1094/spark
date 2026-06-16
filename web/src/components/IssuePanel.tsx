import React from 'react';
import { AlertTriangle } from 'lucide-react';
import { PromptsResponse } from '../api';
import { Badge } from './Badge';

interface IssuePanelProps {
  data: PromptsResponse;
}

export function IssuePanel({ data }: IssuePanelProps) {
  if (!data.issues.length) return null;
  return (
    <div className="mt-6 grid gap-2.5">
      {data.issues.map((issue, i) => {
        const isError = issue.severity === 'error';
        return (
          <div
            key={i}
            className={`flex items-start gap-3 rounded-xl border px-4 py-3.5 text-sm shadow-sm transition-all ${
              isError
                ? 'border-red-200/80 bg-red-50/50 text-red-900'
                : 'border-amber-200/80 bg-amber-50/50 text-amber-900'
            }`}
          >
            <div className="mt-0.5">
              <AlertTriangle size={16} className={isError ? "text-red-500" : "text-amber-500"} />
            </div>
            <div className="flex-1">
              <span className="font-semibold">{isError ? 'Error: ' : 'Warning: '}</span>
              {issue.message}
            </div>
            <Badge tone={isError ? 'red' : 'amber'}>{issue.active ? 'Active' : 'Inactive'}</Badge>
          </div>
        );
      })}
    </div>
  );
}
