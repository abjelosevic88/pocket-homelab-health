// Left column: create button, sections with today's numbers — the Pocket Calendar sidebar.
import { useState } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import type { Counts } from '../lib/types'
import { Icon } from './icons'
import { createItems, type CreateKind } from './Layout'
import { type Anchor, Menu, rectOf } from './ui'

export function Sidebar({ onCreate, onNavigate, counts }: { onCreate: (k: CreateKind) => void; onNavigate: () => void; counts: Counts | null }) {
  const { status, settings } = useApp()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const title = settings?.title || status?.title || 'Pocket Health'
  const nav: { to: string; label: string; icon: string; end?: boolean; badge?: number | string; badgeKind?: string; badgeTitle?: string; count?: string }[] = [
    { to: '/', label: 'Today', icon: 'home', end: true, count: counts && counts.steps ? `${counts.steps.toLocaleString()} steps` : undefined },
    { to: '/metrics', label: 'Metrics', icon: 'activity' },
    { to: '/workouts', label: 'Workouts', icon: 'run', count: counts && counts.workouts_week ? `${counts.workouts_week} this week` : undefined },
    { to: '/sleep', label: 'Sleep', icon: 'moon', count: counts && counts.sleep_min ? counts.sleep_text : undefined },
    { to: '/goals', label: 'Goals', icon: 'target', count: counts && counts.goals_total ? counts.goals_text : undefined },
    { to: '/medications', label: 'Medications', icon: 'pill', badge: counts?.meds_due, badgeKind: 'warn', badgeTitle: 'doses due' },
    { to: '/insights', label: 'Insights', icon: 'chart' },
  ]
  return (
    <aside className="sidebar" aria-label="Health">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Health' ? title : <>Pocket <b>Health</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^\d/.test(status.version) ? 'v' + status.version : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={17} strokeWidth={2.2} />New<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems(onCreate)} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.badge && <span className={`badge ${n.badgeKind || ''}`} title={`${n.badge} ${n.badgeTitle || ''}`}>{n.badge}</span>}
            {!n.badge && !!n.count && <span className="sb-count">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      <section className="sb-sec">
        <h4 className="sb-h"><span className="sb-h-btn">Quick log</span></h4>
        <div className="sb-quick">
          {[['weight', 'Weight', 'scale'], ['water', 'Water', 'droplet'], ['blood_pressure', 'Blood pressure', 'gauge'], ['mood', 'Mood', 'smile']].map(([k, l, i]) => (
            <button key={k} type="button" className="sb-quick-btn" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('ph:create', { detail: { kind: 'log', metric: k } })) }}><Icon name={i} size={14} />{l}</button>
          ))}
        </div>
      </section>

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('ph:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
