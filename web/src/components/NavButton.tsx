import React from 'react';

interface NavButtonProps {
  active: boolean;
  children: React.ReactNode;
  onClick: () => void;
  icon: React.ComponentType<{ size?: number; className?: string }>;
}

export function NavButton({ active, children, onClick, icon: Icon }: NavButtonProps) {
  return (
    <button
      className={`min-h-10 flex items-center gap-2.5 rounded-xl px-3.5 py-2 text-left text-sm font-semibold transition-all duration-150 active:scale-[0.99] ${
        active
          ? 'bg-slate-900 text-white shadow-md shadow-slate-900/10'
          : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900'
      } md:w-full`}
      onClick={onClick}
    >
      <Icon size={16} className={active ? 'text-indigo-400' : 'text-slate-400 transition-colors'} />
      {children}
    </button>
  );
}
