import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { HomeData, Tile } from '../lib/types'
import { addDays, ago, fmtDate, fmtISOTime, todayISO, weekday } from '../lib/format'
import { fmtDistance, fmtDurationS, fmtMetric, fmtMinutes, fmtPair, unitOf } from '../lib/units'
import { Empty, Loading, useToast } from '../components/ui'
import { Icon, KIND_ICON, WEATHER_ICON } from '../components/icons'
import { Ring, Sparkline, Stacked } from '../components/Chart'
import { openCreate, useOnChange } from '../components/Layout'

export default function Home() {
  const navigate = useNavigate()
  const { toast } = useToast()
  const [date, setDate] = useState(todayISO())
  const [data, setData] = useState<HomeData | null>(null)
  const load = () => api.get<HomeData>(`${V1}/home?date=${date}`).then(setData).catch(e => toast((e as Error).message, 'err'))
  useEffect(() => { load() }, [date]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(load)
  if (!data) return <Loading />
  const isToday = date === todayISO()
  const dose = async (medId: number, slot: string, status: 'taken' | 'skipped') => {
    try { await api.post(`${V1}/medications/${medId}/log`, { date, slot, status }); load() } catch (e) { toast((e as Error).message, 'err') }
  }
  const undo = async (logId: number) => { try { await api.del(`${V1}/medications/log/${logId}`); load() } catch (e) { toast((e as Error).message, 'err') } }
  const night = data.nights.find(n => n.date === date)
  const todaysWorkouts = data.workouts.filter(w => w.date === date)
  const earlier = data.workouts.filter(w => w.date !== date)
  const due = data.medications.flatMap(m => (m.slots || []).filter(s => s.time && !s.status).map(s => ({ m, s })))
  return (
    <div className="stack loose">
      <div className="home-head">
        <div className="grow">
          <h2 className="home-title">{isToday ? 'Today' : weekday(date, true)}<span className="muted"> · {fmtDate(date)}</span></h2>
          <p className="muted small">
            {data.goals_total ? `${data.goals_met} of ${data.goals_total} goals met` : 'No daily goals yet'}
            {data.weather && <> · <span className="inline-ico"><Icon name={WEATHER_ICON[data.weather.condition] || 'cloud'} size={13} />{data.weather.t_max !== null ? `${Math.round(data.weather.t_max)}°` : ''}{data.weather.precip_mm ? `, ${data.weather.precip_mm} mm rain` : ''}</span></>}
            {data.last_sync && isToday && <> · last sync {ago(data.last_sync)}</>}
          </p>
        </div>
        <div className="row">
          <button type="button" className="btn ghost icon-only" onClick={() => setDate(addDays(date, -1))} aria-label="Previous day"><Icon name="chevron-left" size={18} /></button>
          <input type="date" value={date} max={todayISO()} onChange={e => e.target.value && setDate(e.target.value)} className="date-pick" />
          <button type="button" className="btn ghost icon-only" onClick={() => setDate(addDays(date, 1))} disabled={isToday} aria-label="Next day"><Icon name="chevron-right" size={18} /></button>
          {!isToday && <button type="button" className="btn sm" onClick={() => setDate(todayISO())}>Today</button>}
        </div>
      </div>

      {!data.has_data && <div className="callout info"><Icon name="smartphone" size={15} /><div><b>Nothing here yet.</b> Connect your phone (the Pocket Homelab app or Health Auto Export, a few taps) or import an Apple Health export under <Link to="/settings/sources">Settings → Phone & imports</Link>, or <button type="button" className="link-btn" onClick={() => openCreate('log')}>log something by hand</button>.</div></div>}

      <div className="tiles">
        {data.tiles.map(t => <TileCard key={t.metric.key} t={t} onClick={() => navigate(`/metrics/${t.metric.key}`)} />)}
      </div>

      <div className="home-grid">
        <section className="card">
          <div className="card-head"><div className="grow"><h2>Sleep</h2><p className="muted small">{night ? `${fmtISOTime(night.start_at)} → ${fmtISOTime(night.end_at)}` : 'No night recorded for this morning'}</p></div><Link to="/sleep" className="btn ghost sm">All nights</Link></div>
          <div className="card-body">
            {night ? <div className="stack">
              <div className="row" style={{ alignItems: 'baseline', gap: 10 }}><span className="big-num">{fmtMinutes(night.asleep_s / 60)}</span><span className="muted small">asleep · {fmtMinutes(night.in_bed_s / 60)} in bed</span></div>
              <Stacked parts={[{ value: night.deep_s, color: '#4338ca', label: 'Deep' }, { value: night.core_s, color: '#818cf8', label: 'Core' }, { value: night.rem_s, color: '#c7d2fe', label: 'REM' }, { value: night.awake_s, color: '#f59e0b', label: 'Awake' }]} />
              <div className="legend small muted"><span><i style={{ background: '#4338ca' }} />Deep {fmtMinutes(night.deep_s / 60)}</span><span><i style={{ background: '#818cf8' }} />Core {fmtMinutes(night.core_s / 60)}</span><span><i style={{ background: '#c7d2fe' }} />REM {fmtMinutes(night.rem_s / 60)}</span><span><i style={{ background: '#f59e0b' }} />Awake {fmtMinutes(night.awake_s / 60)}</span></div>
            </div> : <Empty title="No sleep data" icon="moon"><button type="button" className="link-btn" onClick={() => openCreate('sleep')}>Add a night</button></Empty>}
          </div>
        </section>

        <section className="card">
          <div className="card-head"><div className="grow"><h2>Workouts</h2><p className="muted small">{todaysWorkouts.length ? `${todaysWorkouts.length} today` : 'None today'}{earlier.length ? ` · ${earlier.length} earlier this week` : ''}</p></div><button type="button" className="btn ghost sm" onClick={() => openCreate('workout')}><Icon name="plus" size={14} />Add</button></div>
          <div className="card-body flush">
            {data.workouts.length === 0 ? <div className="pad"><Empty title="No workouts this week" icon="run" /></div> : <ul className="list">
              {data.workouts.slice(0, 6).map(w => (
                <li key={w.id}><Link to={`/workouts/${w.id}`} className="list-row">
                  <span className="list-ico" style={{ background: 'var(--accent-soft)' }}><Icon name={KIND_ICON[w.kind] || 'activity'} size={16} /></span>
                  <span className="grow"><b>{w.name || w.kind}</b><span className="muted small block">{w.date === date ? fmtISOTime(w.start_at) : fmtDate(w.date, false)} · {fmtDurationS(w.duration_s)}{w.distance_m ? ` · ${fmtDistance(w.distance_m)}` : ''}{w.energy_kcal ? ` · ${Math.round(w.energy_kcal)} kcal` : ''}{w.avg_hr ? ` · ♥ ${Math.round(w.avg_hr)}` : ''}</span></span>
                  {w.habit_id && <Icon name="check-circle" size={14} className="ok" title="Ticked a habit" />}
                </Link></li>
              ))}
            </ul>}
          </div>
        </section>

        <section className="card">
          <div className="card-head"><div className="grow"><h2>Medications</h2><p className="muted small">{due.length ? `${due.length} dose${due.length === 1 ? '' : 's'} still due` : data.medications.length ? 'All doses handled' : 'Nothing scheduled'}</p></div><Link to="/medications" className="btn ghost sm">Manage</Link></div>
          <div className="card-body flush">
            {data.medications.length === 0 ? <div className="pad"><Empty title="No medications" icon="pill"><button type="button" className="link-btn" onClick={() => openCreate('medication')}>Add one</button></Empty></div> : <ul className="list">
              {data.medications.map(m => (m.slots || []).map(s => (
                <li key={`${m.id}-${s.time}-${s.log_id}`} className="list-row">
                  <span className="list-ico" style={{ background: m.color || 'var(--accent-soft)', color: m.color ? '#fff' : undefined }}><Icon name="pill" size={15} /></span>
                  <span className="grow"><b>{m.name}</b> <span className="muted small">{m.dose}</span><span className="muted small block">{s.time || 'extra dose'}{s.status ? ` · ${s.status}${s.taken_at ? ' ' + fmtISOTime(s.taken_at) : ''}` : ''}</span></span>
                  {s.status ? <button type="button" className="btn ghost sm" onClick={() => undo(s.log_id)}>Undo</button>
                    : <span className="row"><button type="button" className="btn sm primary" onClick={() => dose(m.id, s.time, 'taken')}><Icon name="check" size={14} />Taken</button><button type="button" className="btn ghost sm" onClick={() => dose(m.id, s.time, 'skipped')}>Skip</button></span>}
                </li>
              )))}
            </ul>}
          </div>
        </section>

        <section className="card">
          <div className="card-head"><div className="grow"><h2>Symptoms</h2><p className="muted small">The last 7 days</p></div><button type="button" className="btn ghost sm" onClick={() => openCreate('symptom')}><Icon name="plus" size={14} />Log</button></div>
          <div className="card-body flush">
            {data.symptoms.length === 0 ? <div className="pad"><Empty title="Nothing logged" icon="smile">Feeling fine.</Empty></div> : <ul className="list">
              {data.symptoms.map(x => <li key={x.id} className="list-row"><span className={`sev sev-${x.severity}`}>{x.severity}</span><span className="grow"><b>{x.name}</b><span className="muted small block">{fmtDate(x.date, false)} {fmtISOTime(x.at)}{x.note ? ` · ${x.note}` : ''}</span></span></li>)}
            </ul>}
          </div>
        </section>
      </div>
    </div>
  )
}

function TileCard({ t, onClick }: { t: Tile; onClick: () => void }) {
  const { settings } = useApp()
  const m = t.metric
  const v = t.value
  const pct = t.goal ? t.pct : 0
  const stale = t.date && !t.is_today && m.agg === 'last' ? false : false
  return (
    <button type="button" className="tile" onClick={onClick} style={{ '--tile': m.color } as React.CSSProperties}>
      <div className="tile-head"><span className="tile-ico"><Icon name={m.icon} size={15} /></span><span className="tile-name ellipsis">{m.name}</span>{t.streak > 1 && <span className="tile-streak" title={`${t.streak} days in a row`}><Icon name="fire" size={11} />{t.streak}</span>}</div>
      <div className="tile-main">
        <div className="grow">
          <div className="tile-val">{v === null ? <span className="muted">—</span> : m.pair ? fmtPair(m, v, t.value2) : fmtMetric(m, v, false)}<span className="tile-unit">{v !== null && !m.pair && !m.key.startsWith('sleep_') && unitOf(m) !== 'count' ? unitOf(m) : ''}</span></div>
          {t.goal ? <div className="tile-goal muted small">{t.goal.direction === 'at_most' ? 'under' : 'of'} {m.key.startsWith('sleep_') ? fmtMinutes(t.goal.target) : fmtMetric(m, t.goal.target)}</div>
            : <div className="tile-goal muted small">{v !== null && t.date && m.agg === 'last' && t.date !== todayISO() ? fmtDate(t.date, false) : settings?.units && m.agg !== 'last' ? 'today' : ''}{stale ? '' : ''}</div>}
        </div>
        {t.goal ? <Ring pct={pct} size={44} stroke={5} color={m.color}><span className="small tnum">{pct}%</span></Ring> : <Sparkline values={t.spark} color={m.color} width={72} height={28} />}
      </div>
    </button>
  )
}
