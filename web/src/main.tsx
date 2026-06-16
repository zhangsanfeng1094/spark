import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Sparkles, Layers, Cpu, Activity } from 'lucide-react';
import { NavButton } from './components/NavButton';
import { PromptsView } from './components/PromptsView';
import { ProfilesView } from './components/ProfilesView';
import './styles.css';

function App() {
  const [tab, setTab] = useState<'prompts' | 'profiles'>('prompts');

  return (
    <div className="grid min-h-screen grid-cols-1 bg-gradient-to-br from-slate-50 via-white to-slate-100 text-sm text-slate-800 md:grid-cols-[260px_minmax(0,1fr)]">
      <aside className="flex flex-col border-b border-slate-200/70 bg-white/80 px-4 py-4 backdrop-blur-xl md:border-b-0 md:border-r md:px-5 md:py-8">
        <div className="flex items-center gap-2.5 text-xl font-extrabold tracking-tight text-slate-900 md:mb-10">
          <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-slate-900 text-white shadow-md shadow-slate-900/10">
            <Sparkles size={18} className="text-indigo-400" />
          </span>
          <span className="bg-gradient-to-r from-slate-950 to-slate-750 bg-clip-text text-transparent">Spark</span>
        </div>

        <div className="flex flex-row gap-1.5 md:flex-col md:gap-1.5">
          <NavButton active={tab === 'prompts'} onClick={() => setTab('prompts')} icon={Layers}>
            Prompts
          </NavButton>
          <NavButton active={tab === 'profiles'} onClick={() => setTab('profiles')} icon={Cpu}>
            Profiles
          </NavButton>
        </div>

        <div className="hidden mt-auto md:block pt-6 border-t border-slate-100">
          <div className="rounded-2xl bg-slate-50 p-4 border border-slate-100/70 shadow-sm">
            <div className="flex items-center gap-2 text-xs font-semibold text-slate-400 uppercase tracking-wider mb-1.5">
              <Activity size={12} className="text-indigo-500 animate-pulse" /> Live Status
            </div>
            <div className="text-xs text-slate-600 leading-relaxed font-medium">
              Connected and operational. Managing your orchestration layer.
            </div>
          </div>
        </div>
      </aside>
      <main className="min-w-0 px-4 py-6 md:px-10 md:py-10">
        {tab === 'prompts' ? <PromptsView /> : <ProfilesView />}
      </main>
    </div>
  );
}

createRoot(document.getElementById('root')!).render(<App />);
