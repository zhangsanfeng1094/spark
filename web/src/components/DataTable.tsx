import React from 'react';

interface DataTableProps {
  headers: string[];
  children: React.ReactNode;
}

const tableCell = 'overflow-hidden text-ellipsis whitespace-nowrap border-b border-slate-100 px-4 py-3.5 text-left align-middle text-sm text-slate-700';

export function DataTable({ headers, children }: DataTableProps) {
  return (
    <div className="overflow-hidden rounded-2xl border border-slate-200/80 bg-white shadow-sm transition-all">
      <table className="w-full table-fixed">
        <thead>
          <tr className="bg-slate-50/70">
            {headers.map((h, i) => (
              <th
                key={`${h}-${i}`}
                className={`border-b border-slate-100 px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-slate-400 ${i === 3 ? 'hidden md:table-cell' : ''}`}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100/70">{children}</tbody>
      </table>
    </div>
  );
}

export { tableCell };
