import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { RANGES, type Habit, type Workout, type WorkoutStats } from '../lib/types'
import { fmtDate, fmtDateTime, fmtISOTime, weekday } from '../lib/format'
import { fmtDistance, fmtDurationS, fmtPace } from '../lib/units'
import { Confirm, Empty, Loading, PageHeader, Segmented, useToast } from '../components/ui'
import { Icon, KIND_ICON } from '../components/icons'
import { Bars } from '../components/Chart'
import { WorkoutModal } from '../components/forms'
import { Select } from '../components/Select'
import { openCreate, useCatalog, useCrumb, useOnChange } from '../components/Layout'

export default function Workouts() {
  const [sp, setSp] = useSearchParams()
  const catalog = useCatalog()
  const range = sp.get('range') || 'm'
  const kind = sp.get('kind') || ''
  const [stats, setStats] = useState<WorkoutStats | null>(null)
  const [list, setList] = useState<Workout[] | null>(null)
  const load = () => {
    api.get<WorkoutStats>(`${V1}/workouts/stats?range=${range}`).then(setStats).catch(() => {})
    api.get<Workout[]>(`${V1}/workouts?range=${range}&from=${rangeFrom(range)}&limit=300${kind ? `&kind=${kind}` : ''}`).then(setList).catch(() => {})
  }
  useEffect(() => { load() }, [range, kind]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(load)
  if (!stats || !list) return <Loading />
  const total = stats.by_kind.reduce((a, k) => ({ n: a.n + k.count, s: a.s + k.duration_s, d: a.d + k.distance_m, e: a.e + k.energy_kcal }), { n: 0, s: 0, d: 0, e: 0 })
  const weeks = Object.entries(stats.weekly).sort(([a], [b]) => a.localeCompare(b)).slice(-12)
  const byDay = new Map<string, Workout[]>()
  for (const w of list) { byDay.set(w.date, [...(byDay.get(w.date) || []), w]) }
  return (
    <div className="stack loose">
      <PageHeader title="Workouts" sub={`${total.n} in this range · ${fmtDurationS(total.s)}${total.d ? ` · ${fmtDistance(total.d)}` : ''}${total.e ? ` · ${Math.round(total.e).toLocaleString()} kcal` : ''}`}
        actions={<button type="button" className="btn primary" onClick={() => openCreate('workout')}><Icon name="plus" size={15} />Add</button>} />
      <div className="row wrap filters">
        <div className="scroll-x"><Segmented size="sm" value={range} onChange={v => setSp({ range: v, ...(kind ? { kind } : {}) })} options={RANGES.map(r => ({ value: r.id, label: r.label }))} /></div>
        <Select value={kind} onChange={v => setSp({ range, ...(v ? { kind: v } : {}) })} options={(catalog?.workout_kinds || []).map(k => ({ value: k.key, label: k.name }))} placeholder="Every kind" clearLabel="Every kind" />
      </div>
      {stats.by_kind.length > 0 && <div className="two-col">
        <section className="card"><div className="card-head"><div className="grow"><h2>By kind</h2></div></div><div className="card-body flush">
          <ul className="list">{stats.by_kind.map(k => <li key={k.kind}><Link to={`/workouts?range=${range}&kind=${k.kind}`} className="list-row"><span className="list-ico"><Icon name={KIND_ICON[k.kind] || 'activity'} size={16} /></span><span className="grow"><b>{k.name}</b><span className="muted small block">{k.count} × · {fmtDurationS(k.duration_s)}{k.distance_m ? ` · ${fmtDistance(k.distance_m)}` : ''}{k.energy_kcal ? ` · ${Math.round(k.energy_kcal)} kcal` : ''}</span></span></Link></li>)}</ul>
        </div></section>
        <section className="card"><div className="card-head"><div className="grow"><h2>Per week</h2><p className="muted small">Workouts per week, last {weeks.length}</p></div></div><div className="card-body">
          <Bars items={weeks.map(([d, n]) => ({ label: fmtDate(d, false), value: n }))} format={v => String(v)} height={110} />
        </div></section>
      </div>}
      {list.length === 0 ? <Empty title="No workouts in this range" icon="run"><button type="button" className="link-btn" onClick={() => openCreate('workout')}>Add one</button>, or let the phone send them.</Empty> : (
        <section className="card"><div className="card-body flush">
          {[...byDay.entries()].map(([d, ws]) => (
            <div key={d}>
              <div className="day-h">{weekday(d)} {fmtDate(d)}</div>
              <ul className="list">{ws.map(w => (
                <li key={w.id}><Link to={`/workouts/${w.id}`} className="list-row">
                  <span className="list-ico" style={{ background: 'var(--accent-soft)' }}><Icon name={KIND_ICON[w.kind] || 'activity'} size={16} /></span>
                  <span className="grow"><b>{w.name || w.kind}</b><span className="muted small block">{fmtISOTime(w.start_at)} · {fmtDurationS(w.duration_s)}{w.distance_m ? ` · ${fmtDistance(w.distance_m)}` : ''}{w.distance_m > 500 && (w.kind === 'running' || w.kind === 'walking') ? ` · ${fmtPace(w.duration_s / (w.distance_m / 1000))}` : ''}{w.energy_kcal ? ` · ${Math.round(w.energy_kcal)} kcal` : ''}{w.avg_hr ? ` · ♥ ${Math.round(w.avg_hr)}` : ''}</span></span>
                  {w.habit_id && <Icon name="check-circle" size={14} className="ok" title="Ticked a habit" />}
                  <Icon name="chevron-right" size={14} className="muted" />
                </Link></li>
              ))}</ul>
            </div>
          ))}
        </div></section>
      )}
    </div>
  )
}

function rangeFrom(range: string): string {
  const days = ({ w: 7, '2w': 14, m: 30, '3m': 90, '6m': 180, y: 365, all: 3650 } as Record<string, number>)[range] || 30
  const d = new Date(); d.setDate(d.getDate() - days + 1)
  return d.toISOString().slice(0, 10)
}

export function WorkoutPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { settings } = useApp()
  const [w, setW] = useState<Workout | null>(null)
  const [edit, setEdit] = useState(false)
  const [del, setDel] = useState(false)
  const [habits, setHabits] = useState<{ connected: boolean; habits: Habit[]; open_url?: string } | null>(null)
  const load = () => api.get<Workout>(`${V1}/workouts/${id}`).then(setW).catch(e => toast((e as Error).message, 'err'))
  useEffect(() => { load() }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { api.get<{ connected: boolean; habits: Habit[]; open_url?: string }>(`${V1}/habits/list`).then(setHabits).catch(() => {}) }, [])
  useCrumb(w ? (w.name || w.kind) : undefined)
  if (!w) return <Loading />
  const tick = async (habitId: number) => { try { const r = await api.post<{ ok: boolean; error?: string }>(`${V1}/workouts/${w.id}/habit`, { habit_id: habitId }); toast(r.ok ? 'Habit ticked' : r.error || 'failed', r.ok ? 'ok' : 'err'); load() } catch (e) { toast((e as Error).message, 'err') } }
  const remove = async () => { try { await api.del(`${V1}/workouts/${w.id}`); navigate('/workouts') } catch (e) { toast((e as Error).message, 'err') } }
  const pace = w.distance_m > 500 ? w.duration_s / (w.distance_m / 1000) : 0
  const stats: [string, string][] = [
    ['Duration', fmtDurationS(w.duration_s)],
    ...(w.distance_m ? [['Distance', fmtDistance(w.distance_m)] as [string, string]] : []),
    ...(pace && (w.kind === 'running' || w.kind === 'walking' || w.kind === 'hiking') ? [['Pace', fmtPace(pace)] as [string, string]] : []),
    ...(w.distance_m && w.kind === 'cycling' ? [['Speed', `${((w.distance_m / 1000) / (w.duration_s / 3600)).toFixed(1)} km/h`] as [string, string]] : []),
    ...(w.energy_kcal ? [['Energy', `${Math.round(w.energy_kcal)} kcal`] as [string, string]] : []),
    ...(w.avg_hr ? [['Avg heart rate', `${Math.round(w.avg_hr)} bpm`] as [string, string]] : []),
    ...(w.max_hr ? [['Max heart rate', `${Math.round(w.max_hr)} bpm`] as [string, string]] : []),
    ...(w.elevation_m ? [['Elevation', `${Math.round(w.elevation_m)} m`] as [string, string]] : []),
    ...(w.steps ? [['Steps', w.steps.toLocaleString()] as [string, string]] : []),
    ...(w.temperature !== null ? [['Weather', `${Math.round(settings?.units === 'imperial' ? w.temperature * 9 / 5 + 32 : w.temperature)}°${w.humidity !== null ? `, ${Math.round(w.humidity)}% humidity` : ''}`] as [string, string]] : []),
  ]
  return (
    <div className="stack loose">
      <PageHeader title={<span className="row" style={{ gap: 10 }}><span className="tile-ico lg" style={{ '--tile': '#22c55e' } as React.CSSProperties}><Icon name={KIND_ICON[w.kind] || 'activity'} size={18} /></span>{w.name || w.kind}</span>}
        sub={`${fmtDateTime(w.start_at)} · ${w.source}${w.device ? ` · ${w.device}` : ''}`}
        actions={<><button type="button" className="btn" onClick={() => setEdit(true)}><Icon name="edit" size={15} />Edit</button><button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}>Delete</button></>} />
      <div className="stats-row">{stats.map(([l, v]) => <div key={l} className="stat"><span className="stat-l">{l}</span><span className="stat-v tnum">{v}</span></div>)}</div>
      {w.notes && <section className="card"><div className="card-body"><p style={{ whiteSpace: 'pre-wrap' }}>{w.notes}</p></div></section>}
      <section className="card"><div className="card-head"><div className="grow"><h2>Pocket Habits</h2><p className="muted small">{w.habit_id ? `Ticked habit #${w.habit_id} on ${fmtDate(w.date)}` : w.habit_error ? `Could not tick a habit: ${w.habit_error}` : 'Tick a habit for this workout (rules under Settings → Connected apps do it automatically).'}</p></div></div>
        <div className="card-body">
          {habits?.connected ? <div className="row wrap">{habits.habits.map(h => <button key={h.id} type="button" className={`chip ${w.habit_id === h.id ? 'active' : ''}`} onClick={() => tick(h.id)}><Icon name="habit" size={13} />{h.name}</button>)}{habits.habits.length === 0 && <span className="muted small">No habits in Pocket Habits yet.</span>}</div>
            : <p className="muted small">Pocket Habits is not connected. <Link to="/settings/apps">Connect it</Link></p>}
        </div>
      </section>
      {edit && <WorkoutModal workout={w} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); load() }} />}
      {del && <Confirm title="Delete this workout?" message="It is removed here; the phone may send it again on the next sync." confirmLabel="Delete" onCancel={() => setDel(false)} onConfirm={remove} />}
    </div>
  )
}

import { useApp } from '../lib/app-context'
