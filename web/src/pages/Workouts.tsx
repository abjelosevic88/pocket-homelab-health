import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { RANGES, type Habit, type Route, type Workout, type WorkoutStats, type WorkoutTrack } from '../lib/types'
import { fmtDate, fmtDateTime, fmtISOTime, weekday } from '../lib/format'
import { fmtDistance, fmtDurationS, fmtPace } from '../lib/units'
import { Confirm, CoreHint, Empty, Loading, PageHeader, Segmented, useToast } from '../components/ui'
import { Icon, KIND_ICON } from '../components/icons'
import { Bars, LineChart } from '../components/Chart'
import { WorkoutModal } from '../components/forms'
import { Select } from '../components/Select'
import { RouteMap } from '../components/RouteMap'
import { clock, cumulative, speeds as trackSpeeds, thin } from '../lib/track'
import { ChartCard, EffortMeter, PartsTable, SeriesCharts, WorkoutSummary, speedText, type BigStat, type MinorStat } from '../components/WorkoutDetail'
import { openCreate, useCatalog, useCrumb, useOnChange } from '../components/Layout'

// a colour per kind of workout, so a list of them reads at a glance
const KIND_COLOR: Record<string, string> = { running: '#f97316', walking: '#22c55e', hiking: '#16a34a', cycling: '#06b6d4', swimming: '#3b82f6', strength: '#a855f7', functional: '#a855f7', cross_training: '#a855f7', core: '#a855f7', hiit: '#ef4444', yoga: '#ec4899', pilates: '#ec4899', stretching: '#ec4899', mind_body: '#8b5cf6', cooldown: '#64748b' }
export const kindColor = (k: string) => KIND_COLOR[k] || '#e11d48'
const ON_FOOT = new Set(['running', 'walking', 'hiking'])

export default function Workouts() {
  const [sp, setSp] = useSearchParams()
  const range = sp.get('range') || 'm'
  const kind = sp.get('kind') || ''
  const [stats, setStats] = useState<WorkoutStats | null>(null)
  const [list, setList] = useState<Workout[] | null>(null)
  const LIMIT = 300
  const load = () => {
    api.get<WorkoutStats>(`${V1}/workouts/stats?range=${range}`).then(setStats).catch(() => {})
    api.get<Workout[]>(`${V1}/workouts?range=${range}&from=${rangeFrom(range)}&limit=${LIMIT}${kind ? `&kind=${kind}` : ''}`).then(setList).catch(() => {})
  }
  useEffect(() => { load() }, [range, kind]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(load)
  if (!stats || !list) return <Loading />
  const shown = kind ? stats.by_kind.filter(k => k.kind === kind) : stats.by_kind
  const total = shown.reduce((a, k) => ({ n: a.n + k.count, s: a.s + k.duration_s, d: a.d + k.distance_m, e: a.e + k.energy_kcal }), { n: 0, s: 0, d: 0, e: 0 })
  const weeks = Object.entries(stats.weekly).sort(([a], [b]) => a.localeCompare(b)).slice(-12)
  const kindMax = Math.max(...stats.by_kind.map(k => k.duration_s), 1)
  // grouped by month: a header with the month's totals, then one row per workout
  const byMonth = new Map<string, Workout[]>()
  for (const w of list) { const m = w.date.slice(0, 7); byMonth.set(m, [...(byMonth.get(m) || []), w]) }
  const monthName = (ym: string) => { const [y, m] = ym.split('-').map(Number); return new Date(y, m - 1, 1).toLocaleDateString(undefined, { month: 'long', year: 'numeric' }) }
  const setKind = (k: string) => setSp({ range, ...(k ? { kind: k } : {}) })
  const big: [string, string][] = [
    ['Workouts', String(total.n)],
    ['Time', fmtDurationS(total.s)],
    ...(total.d ? [['Distance', fmtDistance(total.d)] as [string, string]] : []),
    ...(total.e ? [['Energy', `${Math.round(total.e).toLocaleString()} kcal`] as [string, string]] : []),
  ]
  return (
    <div className="stack loose wl-page">
      <PageHeader title="Workouts" sub={`${fmtDate(rangeFrom(range))} – today`}
        actions={<><div className="scroll-x"><Segmented size="sm" value={range} onChange={v => setSp({ range: v, ...(kind ? { kind } : {}) })} options={RANGES.map(r => ({ value: r.id, label: r.label }))} /></div>
          <button type="button" className="btn primary" onClick={() => openCreate('workout')}><Icon name="plus" size={15} />Add</button></>} />

      {stats.by_kind.length > 1 && <div className="wl-kinds scroll-x">
        <button type="button" className={`wl-kind ${!kind ? 'active' : ''}`} onClick={() => setKind('')}>All<span className="tnum">{stats.by_kind.reduce((a, k) => a + k.count, 0)}</span></button>
        {stats.by_kind.map(k => <button key={k.kind} type="button" className={`wl-kind ${kind === k.kind ? 'active' : ''}`} style={{ '--tile': kindColor(k.kind) } as React.CSSProperties} onClick={() => setKind(kind === k.kind ? '' : k.kind)}>
          <Icon name={KIND_ICON[k.kind] || 'activity'} size={13} />{k.name}<span className="tnum">{k.count}</span></button>)}
      </div>}

      {total.n > 0 && <section className="card wl-summary">
        <div className="wo-big">{big.map(([l, v]) => { const m = v.match(/^([\d.,:]+)\s*([a-zA-Z/]+)$/); return <div key={l} className="wo-big-item"><span className="wo-big-v tnum">{m ? m[1] : v}{m && m[2] && <small>{m[2]}</small>}</span><span className="wo-big-l">{l}</span></div> })}</div>
      </section>}

      {stats.by_kind.length > 0 && <div className="wl-grid">
        <section className="card"><div className="card-head"><div className="grow"><h2>Per week</h2><p className="muted small">Workouts in each of the last {weeks.length} weeks</p></div></div><div className="card-body">
          <Bars items={weeks.map(([d, n]) => ({ label: fmtDate(d, false), value: n }))} format={v => String(v)} height={150} />
        </div></section>
        <section className="card"><div className="card-head"><div className="grow"><h2>By kind</h2><p className="muted small">Time spent on each</p></div></div>
          <div className="card-body"><div className="kind-bars">{stats.by_kind.map(k => <button key={k.kind} type="button" className={`kind-row ${kind === k.kind ? 'active' : ''}`} style={{ '--tile': kindColor(k.kind) } as React.CSSProperties} onClick={() => setKind(kind === k.kind ? '' : k.kind)}>
            <span className="kind-ico"><Icon name={KIND_ICON[k.kind] || 'activity'} size={14} /></span>
            <span className="kind-main"><span className="kind-top"><b>{k.name}</b><span className="muted small tnum">{k.count} × · {fmtDurationS(k.duration_s)}{k.distance_m ? ` · ${fmtDistance(k.distance_m)}` : ''}</span></span>
              <span className="kind-track"><i style={{ width: `${Math.max(3, (k.duration_s / kindMax) * 100)}%` }} /></span></span>
          </button>)}</div></div>
        </section>
      </div>}

      {list.length === 0 ? <Empty title="No workouts in this range" icon="run"><button type="button" className="link-btn" onClick={() => openCreate('workout')}>Add one</button>, or let the phone send them.</Empty> : <>
        {[...byMonth.entries()].map(([ym, ws]) => {
          const mt = ws.reduce((a, w) => ({ s: a.s + w.duration_s, d: a.d + w.distance_m }), { s: 0, d: 0 })
          return <section key={ym} className="card wl-month">
            <div className="wl-month-head"><h2>{monthName(ym)}</h2><span className="muted small tnum">{ws.length} {ws.length === 1 ? 'workout' : 'workouts'} · {fmtDurationS(mt.s)}{mt.d ? ` · ${fmtDistance(mt.d)}` : ''}</span></div>
            <ul className="list">{ws.map((w, i) => {
              const sameDay = i > 0 && ws[i - 1].date === w.date
              const pace = w.distance_m > 500 && ON_FOOT.has(w.kind) ? fmtPace(w.duration_s / (w.distance_m / 1000)) : ''
              return <li key={w.id}><Link to={`/workouts/${w.id}`} className={`wl-row ${sameDay ? 'same-day' : ''}`} style={{ '--tile': kindColor(w.kind) } as React.CSSProperties}>
                <span className="wl-date">{!sameDay && <><span>{weekday(w.date)}</span><b className="tnum">{Number(w.date.slice(8, 10))}</b></>}</span>
                <span className="wl-ico"><Icon name={KIND_ICON[w.kind] || 'activity'} size={16} /></span>
                <span className="wl-name"><b>{w.name || w.kind}</b><span className="muted small">{fmtISOTime(w.start_at)}<span className="wl-mob">{w.distance_m ? ` · ${fmtDistance(w.distance_m)}` : ''}{pace ? ` · ${pace}` : ''}</span>{w.place ? ` · ${w.place}` : ''}{w.device ? <span className="wl-desk"> · {w.device}</span> : null}</span></span>
                <span className="wl-m tnum"><b>{fmtDurationS(w.duration_s)}</b><span>time</span></span>
                <span className="wl-m tnum">{w.distance_m ? <><b>{fmtDistance(w.distance_m)}</b><span>distance</span></> : null}</span>
                <span className="wl-m tnum wl-opt">{pace ? <><b>{pace}</b><span>pace</span></> : null}</span>
                <span className="wl-m tnum wl-opt">{w.energy_kcal ? <><b>{Math.round(w.energy_kcal)}</b><span>kcal</span></> : null}</span>
                <span className="wl-m tnum wl-opt">{w.avg_hr ? <><b className="wl-hr"><Icon name="heart" size={11} />{Math.round(w.avg_hr)}</b><span>bpm</span></> : null}</span>
                <span className="wl-end">{w.habit_id ? <Icon name="check-circle" size={14} className="ok" title="Ticked a habit" /> : null}<Icon name="chevron-right" size={14} className="muted" /></span>
              </Link></li>
            })}</ul>
          </section>
        })}
        {list.length >= LIMIT && <p className="muted small center">Showing the latest {LIMIT}. Pick a shorter range or one kind to see the rest.</p>}
      </>}
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
  const [route, setRoute] = useState<Route | null>(null)
  const loadRoute = (fresh = false) => api.get<Route>(`${V1}/workouts/${id}/route${fresh ? '?fresh=1' : ''}`).then(setRoute).catch(() => {})
  useEffect(() => { setRoute(null); loadRoute() }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  const [detail, setDetail] = useState<WorkoutTrack | null>(null)
  useEffect(() => { setDetail(null); api.get<WorkoutTrack>(`${V1}/workouts/${id}/track`).then(setDetail).catch(() => {}) }, [id])
  const load = () => api.get<Workout>(`${V1}/workouts/${id}`).then(setW).catch(e => toast((e as Error).message, 'err'))
  useEffect(() => { load() }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { api.get<{ connected: boolean; habits: Habit[]; open_url?: string }>(`${V1}/habits/list`).then(setHabits).catch(() => {}) }, [])
  const track = route?.track
  const speeds = useMemo(() => track && track.length > 1 && track.some(p => p[3] > 0) ? trackSpeeds(track) : undefined, [track])
  const elevation = useMemo(() => {
    if (!track || track.length < 2 || !track.some(p => p[2] !== 0)) return []
    const d = cumulative(track)
    return thin(track.map((p, i) => ({ x: String(d[i]), y: p[2] }))) // x: metres from the start
  }, [track])
  useCrumb(w ? (w.name || w.kind) : undefined)
  if (!w) return <Loading />
  const tick = async (habitId: number) => { try { const r = await api.post<{ ok: boolean; error?: string }>(`${V1}/workouts/${w.id}/habit`, { habit_id: habitId }); toast(r.ok ? 'Habit ticked' : r.error || 'failed', r.ok ? 'ok' : 'err'); load() } catch (e) { toast((e as Error).message, 'err') } }
  const remove = async () => { try { await api.del(`${V1}/workouts/${w.id}`); navigate('/workouts') } catch (e) { toast((e as Error).message, 'err') } }
  const imperial = settings?.units === 'imperial'
  const paced = w.kind === 'running' || w.kind === 'walking' || w.kind === 'hiking'
  const pace = w.distance_m > 500 ? w.duration_s / (w.distance_m / 1000) : 0
  const big: BigStat[] = [
    ...(w.distance_m ? [{ label: 'Distance', value: fmtDistance(w.distance_m) }] : []),
    { label: 'Time', value: clock(w.duration_s) },
    ...(pace && paced ? [{ label: 'Avg pace', value: fmtPace(pace) }] : []),
    ...(w.distance_m && !paced && w.duration_s ? [{ label: 'Avg speed', value: speedText(w.kind, w.distance_m / w.duration_s, imperial) }] : []),
    ...(w.energy_kcal || w.total_kcal ? [{ label: w.energy_kcal ? 'Active energy' : 'Energy', value: `${Math.round(w.energy_kcal || w.total_kcal)} kcal` }] : []),
  ].slice(0, 4)
  const effort = w.effort ?? w.effort_estimated
  const minor: MinorStat[] = [
    ...(w.avg_hr ? [{ icon: 'heart', label: 'Avg heart rate', value: `${Math.round(w.avg_hr)} bpm`, color: '#ef4444' }] : []),
    ...(w.max_hr ? [{ icon: 'heart', label: 'Max heart rate', value: `${Math.round(w.max_hr)} bpm`, color: '#ef4444' }] : []),
    ...(w.total_kcal > w.energy_kcal && w.energy_kcal ? [{ icon: 'flame', label: 'Total energy', value: `${Math.round(w.total_kcal)} kcal`, color: '#f97316' }] : []),
    ...(w.elevation_m ? [{ icon: 'mountain', label: 'Climb', value: `${Math.round(w.elevation_m)} m`, color: '#22c55e' }] : []),
    ...(effort !== null && effort !== undefined ? [{ icon: 'gauge', label: w.effort !== null ? 'Effort' : 'Effort (estimated)', color: '#eab308',
      value: <span className="row" style={{ gap: 8 }}>{Math.round(effort)}/10<EffortMeter value={effort} />{w.effort !== null && w.effort_estimated !== null && <span className="muted small">est. {Math.round(w.effort_estimated)}</span>}</span> }] : []),
    ...(w.steps ? [{ icon: 'footprints', label: 'Steps', value: w.steps.toLocaleString(), color: '#06b6d4' }] : []),
  ]
  const place = w.place || route?.place
  const temp = w.temperature !== null ? Math.round(imperial ? w.temperature * 9 / 5 + 32 : w.temperature) : null
  const hasMap = !!route?.connected && route.points.length > 1
  const routeNote = !route ? 'Looking for the positions recorded during the workout…' : route.source === 'phone' ? `Recorded by ${w.device || 'the phone'} · ${route.n.toLocaleString()} positions · ${fmtDistance(route.distance_m)}` : !route.connected ? `Pocket Movement is not connected${route.detail ? `: ${route.detail}` : ''}.` : route.detail ? `Could not read the route: ${route.detail}` : route.n === 0 ? 'No route: nothing was recorded between the start and the end of this workout.' : `${route.n.toLocaleString()} positions from Pocket Movement · ${fmtDistance(route.distance_m)} recorded${w.distance_m ? ` (the workout says ${fmtDistance(w.distance_m)})` : ''}`
  const routeActions = route?.connected && route.source !== 'phone' && <span className="row" style={{ gap: 6 }}><button type="button" className="btn ghost sm" onClick={() => loadRoute(true)}><Icon name="refresh" size={14} />Refresh</button>{route.distance_m > 0 && Math.abs(route.distance_m - w.distance_m) > 50 && <button type="button" className="btn sm" onClick={async () => { try { setW(await api.post<Workout>(`${V1}/workouts/${w.id}/route/apply`)); toast('Distance taken from the route') } catch (e) { toast((e as Error).message, 'err') } }}>Use this distance</button>}</span>
  const elevRange = elevation.length > 1 ? [Math.min(...elevation.map(p => p.y)), Math.max(...elevation.map(p => p.y))] : null
  const planned = detail ? detail.segments.filter(p => p.kind !== 'lap') : []
  const laps = detail ? detail.segments.filter(p => p.kind === 'lap') : []
  const plannedTitle = planned.some(p => p.kind === 'interval') ? 'Intervals' : planned.some(p => p.kind === 'activity') ? 'Activities' : 'Segments'
  const splitWord = detail?.split_unit === 'mi' ? 'mile' : 'kilometre'
  return (
    <div className="stack loose wo-page">
      <section className="card wo-hero">
        <div className="wo-hero-top">
          <span className="wo-hero-ico" style={{ '--tile': '#22c55e' } as React.CSSProperties}><Icon name={KIND_ICON[w.kind] || 'activity'} size={22} /></span>
          <div className="grow" style={{ minWidth: 0 }}>
            <h1>{w.name || w.kind}</h1>
            <div className="wo-meta">
              <span>{weekday(w.date)}, {fmtDate(w.date)}</span>
              <span>{fmtISOTime(w.start_at)}{w.end_at ? `–${fmtISOTime(w.end_at)}` : ''}</span>
              {place && <span className="row" style={{ gap: 4 }}><Icon name="pin" size={13} />{place}</span>}
            </div>
            {(w.device || temp !== null) && <div className="wo-tags">
              {w.device && <span className="wo-tag"><Icon name="smartphone" size={13} />{w.device}</span>}
              {temp !== null && <span className="wo-tag"><Icon name="thermometer" size={13} />{temp}°{w.humidity !== null ? ` · ${Math.round(w.humidity)}% humidity` : ''}</span>}
            </div>}
          </div>
          <div className="wo-hero-actions"><button type="button" className="btn sm" onClick={() => setEdit(true)}><Icon name="edit" size={14} />Edit</button><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(true)}>Delete</button></div>
        </div>
        <WorkoutSummary big={big} minor={minor} />
      </section>
      {w.notes && <section className="card"><div className="card-body"><p style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{w.notes}</p></div></section>}
      {hasMap ? <section className="card wo-map">
          <div className="wo-map-frame"><RouteMap points={route!.points} speeds={speeds} />{speeds && <span className="pace-legend wo-map-legend"><span>Slower</span><i /><span>Faster</span></span>}</div>
          <div className="wo-map-foot"><Icon name="route" size={14} /><span className="grow muted small">{routeNote}</span>{routeActions}</div>
        </section>
        : <div className="wo-noroute"><Icon name="route" size={16} /><span className="grow">{routeNote}{route && !route.connected && <> Connect it under <Link to="/settings/apps">Settings → Connected apps</Link> to get a map for every workout.{!settings?.core_configured && <CoreHint />}</>}</span>{routeActions}</div>}
      {detail && <>
        <PartsTable title={plannedTitle} sub={planned.some(p => p.kind === 'interval') ? 'The steps of the planned workout, as run' : undefined} parts={planned} kind={w.kind} imperial={imperial} first="Part" />
        <PartsTable title="Laps" parts={laps} kind={w.kind} imperial={imperial} first="Lap" />
        <PartsTable title="Splits" sub={`Per ${splitWord}, ${detail.distance_from === 'series' ? 'from the distance the device measured' : 'from the route'}`} parts={detail.splits} kind={w.kind} imperial={imperial} first={detail.split_unit === 'mi' ? 'Mile' : 'Km'} />
      </>}
      {(elevation.length > 1 || detail) && <div className="wo-charts">
        {detail && <SeriesCharts track={detail} kind={w.kind} imperial={imperial} />}
        {elevation.length > 1 && <ChartCard title="Elevation" color="#22c55e" value={w.elevation_m ? String(Math.round(w.elevation_m)) : undefined} unit="m" note={`climbed · ${Math.round(elevRange![0])}–${Math.round(elevRange![1])} m`}>
          <LineChart points={elevation} color="#22c55e" kind="area" height={170} xLabel={x => fmtDistance(+x) === '—' ? '0' : fmtDistance(+x)} format={v => `${Math.round(v)} m`} />
        </ChartCard>}
      </div>}
      <section className="card wo-habits"><div className="wo-habits-row">
        <span className="wo-minor-ico" style={{ '--tile': 'var(--accent)' } as React.CSSProperties}><Icon name="habit" size={15} /></span>
        <div className="grow" style={{ minWidth: 0 }}><h2>Pocket Habits</h2><p className="muted small">{w.habit_id ? `Ticked habit #${w.habit_id} on ${fmtDate(w.date)}` : w.habit_error ? `Could not tick a habit: ${w.habit_error}` : 'Tick a habit for this workout (rules under Settings → Connected apps do it automatically).'}</p></div>
        {!habits?.connected && <Link className="btn sm" to="/settings/apps">Connect</Link>}
      </div>
        {habits?.connected && <div className="row wrap wo-habits-chips">{habits.habits.map(h => <button key={h.id} type="button" className={`chip ${w.habit_id === h.id ? 'active' : ''}`} onClick={() => tick(h.id)}><Icon name="habit" size={13} />{h.name}</button>)}{habits.habits.length === 0 && <span className="muted small">No habits in Pocket Habits yet.</span>}</div>}
      </section>
      {edit && <WorkoutModal workout={w} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); load() }} />}
      {del && <Confirm title="Delete this workout?" message="It is removed here; the phone may send it again on the next sync." confirmLabel="Delete" onCancel={() => setDel(false)} onConfirm={remove} />}
    </div>
  )
}

import { useApp } from '../lib/app-context'
