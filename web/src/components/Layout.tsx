// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Catalog, Counts } from '../lib/types'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { LogModal, MedicationModal, SleepModal, SymptomModal, WorkoutModal } from './forms'
import { Avatar, type Anchor, type MenuItem, Menu, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'ph-sidebar'

/** Something was added or changed from the shell (or a page): the counts and the pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('ph:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('ph:changed', h)
    return () => window.removeEventListener('ph:changed', h)
  }, [])
}

/** The page's own name for the top bar breadcrumb. */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

/** The metric catalog, for every page that needs it. */
const CatalogCtx = createContext<Catalog | null>(null)
export const useCatalog = () => useContext(CatalogCtx)

export type CreateKind = 'log' | 'workout' | 'sleep' | 'medication' | 'symptom'

/** Open a create dialog from anywhere (a page's button); detail may carry a metric key for "log". */
export const openCreate = (k: CreateKind, metric?: string) => window.dispatchEvent(new CustomEvent('ph:create', { detail: { kind: k, metric } }))

/** The create menu: the sidebar's "New" button and the phone's + button. */
export function createItems(onCreate: (k: CreateKind) => void): MenuItem[] {
  return [
    { label: 'Log a measurement', icon: 'plus', hint: 'N', onSelect: () => onCreate('log') },
    { label: 'Add a workout', icon: 'run', hint: 'W', onSelect: () => onCreate('workout') },
    { label: 'Add a night of sleep', icon: 'moon', onSelect: () => onCreate('sleep') },
    'sep',
    { label: 'Log a symptom', icon: 'alert', hint: 'S', onSelect: () => onCreate('symptom') },
    { label: 'Add a medication', icon: 'pill', onSelect: () => onCreate('medication') },
  ]
}

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const [sp, setSp] = useSearchParams()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])

  const [counts, setCounts] = useState<Counts | null>(null)
  const [catalog, setCatalog] = useState<Catalog | null>(null)
  const load = useCallback(() => { api.get<Counts>(`${V1}/dashboard`).then(setCounts).catch(() => {}) }, [])
  useEffect(() => { load() }, [loc.pathname, load])
  useEffect(() => { api.get<Catalog>(`${V1}/catalog`).then(setCatalog).catch(() => {}) }, [])
  useOnChange(load)

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [modal, setModal] = useState<{ kind: CreateKind; metric?: string } | null>(null)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)

  // /?log=1 (the PWA shortcut) opens the log dialog
  useEffect(() => { if (sp.get('log')) { setModal({ kind: 'log' }); setSp(new URLSearchParams(), { replace: true }) } }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const create = (k: CreateKind, metric?: string) => { closeDrawer(); setModal({ kind: k, metric }) }
  const createRef = useRef(create)
  createRef.current = create

  useEffect(() => {
    const open = () => setShortcuts(true)
    const make = (e: Event) => { const d = (e as CustomEvent<{ kind: CreateKind; metric?: string }>).detail; createRef.current(d.kind, d.metric) }
    window.addEventListener('ph:shortcuts', open)
    window.addEventListener('ph:create', make)
    return () => { window.removeEventListener('ph:shortcuts', open); window.removeEventListener('ph:create', make) }
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      const mk = (k: CreateKind) => { e.preventDefault(); createRef.current(k) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'n': mk('log'); break
        case 'w': mk('workout'); break
        case 's': mk('symptom'); break
        case 'h': go('/'); break
        case 'm': go('/metrics'); break
        case 'o': go('/workouts'); break
        case 'z': go('/sleep'); break
        case 'p': go('/medications'); break
        case 'i': go('/insights'); break
        case 'g': go('/goals'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'log', label: 'Log a measurement', icon: 'plus', hint: 'N', keywords: 'weight blood pressure water record add', run: () => create('log') },
    { id: 'workout', label: 'Add a workout', icon: 'run', hint: 'W', keywords: 'run gym exercise training', run: () => create('workout') },
    { id: 'sleep', label: 'Add a night of sleep', icon: 'moon', keywords: 'bed night', run: () => create('sleep') },
    { id: 'symptom', label: 'Log a symptom', icon: 'alert', hint: 'S', keywords: 'headache pain sick', run: () => create('symptom') },
    { id: 'medication', label: 'Add a medication', icon: 'pill', keywords: 'pill drug supplement', run: () => create('medication') },
    { id: 'home', label: 'Today', icon: 'home', hint: 'H', run: () => navigate('/') },
    { id: 'metrics', label: 'All metrics', icon: 'activity', hint: 'M', keywords: 'steps heart body vitals', run: () => navigate('/metrics') },
    { id: 'workouts', label: 'Workouts', icon: 'run', hint: 'O', run: () => navigate('/workouts') },
    { id: 'sleep-page', label: 'Sleep', icon: 'moon', hint: 'Z', run: () => navigate('/sleep') },
    { id: 'goals', label: 'Goals', icon: 'target', hint: 'G', keywords: 'streak target', run: () => navigate('/goals') },
    { id: 'meds', label: 'Medications & symptoms', icon: 'pill', hint: 'P', run: () => navigate('/medications') },
    { id: 'insights', label: 'Insights', icon: 'chart', hint: 'I', keywords: 'trends correlations weather recap', run: () => navigate('/insights') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'sources', label: 'Settings: phone & imports', icon: 'smartphone', keywords: 'health auto export apple import csv sync', run: () => navigate('/settings/sources') },
    { id: 'apps', label: 'Settings: connected apps', icon: 'layers', keywords: 'habits core integrations', run: () => navigate('/settings/apps') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  const closeModal = () => setModal(null)
  const done = () => { closeModal(); announceChange() }
  return (
    <CrumbCtx.Provider value={setCrumb}>
      <CatalogCtx.Provider value={catalog}>
        <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
          {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
          <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
            <Sidebar onCreate={create} onNavigate={closeDrawer} counts={counts} />
          </div>
          <div className="main">
            <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
            <main className="content"><div className="content-inner"><Outlet /></div></main>
          </div>
          {mobile && <button type="button" className="fab" aria-label="New" title="New" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
          {fabMenu && <Menu anchor={fabMenu} align="right" items={createItems(create)} onClose={() => setFabMenu(null)} width={250} />}
          {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} catalog={catalog} />}
          {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
          {modal?.kind === 'log' && <LogModal metric={modal.metric} onClose={closeModal} onSaved={done} />}
          {modal?.kind === 'workout' && <WorkoutModal onClose={closeModal} onSaved={w => { done(); navigate(`/workouts/${w.id}`) }} />}
          {modal?.kind === 'sleep' && <SleepModal onClose={closeModal} onSaved={() => { done(); navigate('/sleep') }} />}
          {modal?.kind === 'symptom' && <SymptomModal onClose={closeModal} onSaved={() => { done(); navigate('/medications?tab=symptoms') }} />}
          {modal?.kind === 'medication' && <MedicationModal onClose={closeModal} onSaved={() => { done(); navigate('/medications') }} />}
        </div>
      </CatalogCtx.Provider>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = { '': ['Today', '/'], metrics: ['Metrics', '/metrics'], workouts: ['Workouts', '/workouts'], sleep: ['Sleep', '/sleep'], goals: ['Goals', '/goals'], medications: ['Medications', '/medications'], insights: ['Insights', '/insights'] }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('ph:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
