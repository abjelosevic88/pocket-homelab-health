// What was recorded along a workout: the summary at the top, the series as
// charts over time, and the parts (intervals, laps, splits) as tables with
// what each one took.
import { LineChart } from './Chart'
import { Icon } from './icons'
import { clock, thin } from '../lib/track'
import type { StepTarget, TrackPart, WorkoutTrack } from '../lib/types'
import { fmtDistance, fmtPace } from '../lib/units'

const PACED = new Set(['running', 'walking', 'hiking'])

/** How a speed in m/s reads for this kind of workout: a pace on foot, km/h (mph) otherwise. */
export function speedText(kind: string, ms: number, imperial: boolean): string {
  if (!ms) return '—'
  if (PACED.has(kind)) return fmtPace(1000 / ms)
  return imperial ? `${(ms * 2.236936).toFixed(1)} mph` : `${(ms * 3.6).toFixed(1)} km/h`
}

/** "3.48 km" → ["3.48", "km"], "9:18/km" → ["9:18", "/km"], so the number can be big and the unit small. */
function valueUnit(s: string): [string, string] {
  const m = s.match(/^([\d.,:]+)\s*(.*)$/)
  return m ? [m[1], m[2]] : [s, '']
}

export type BigStat = { label: string; value: string }
export type MinorStat = { icon: string; label: string; value: React.ReactNode; color?: string }

/** The workout at a glance: up to four big numbers and a row of smaller ones. */
export function WorkoutSummary({ big, minor }: { big: BigStat[]; minor: MinorStat[] }) {
  return <>
    {big.length > 0 && <div className="wo-big">{big.map(s => {
      const [v, u] = valueUnit(s.value)
      return <div key={s.label} className="wo-big-item"><span className="wo-big-v tnum">{v}{u && <small>{u}</small>}</span><span className="wo-big-l">{s.label}</span></div>
    })}</div>}
    {minor.length > 0 && <div className="wo-minor">{minor.map(s =>
      <div key={s.label} className="wo-minor-item"><span className="wo-minor-ico" style={{ '--tile': s.color || 'var(--muted)' } as React.CSSProperties}><Icon name={s.icon} size={15} /></span>
        <span className="wo-minor-txt"><span className="wo-minor-l">{s.label}</span><span className="wo-minor-v tnum">{s.value}</span></span></div>)}</div>}
  </>
}

/** An effort score out of ten as ten little bars. */
export function EffortMeter({ value }: { value: number }) {
  const v = Math.max(0, Math.min(10, Math.round(value)))
  const color = v <= 3 ? '#22c55e' : v <= 6 ? '#eab308' : v <= 8 ? '#f97316' : '#ef4444'
  return <span className="effort-meter" aria-hidden="true">{Array.from({ length: 10 }, (_, i) => <i key={i} style={i < v ? { background: color } : undefined} />)}</span>
}

const CHARTS: { key: string; title: string; color: string; unit?: string; format: (v: number, kind: string, imperial: boolean) => string }[] = [
  { key: 'heart_rate', title: 'Heart rate', color: '#ef4444', unit: 'bpm', format: v => `${Math.round(v)}` },
  { key: 'speed', title: 'Pace', color: '#06b6d4', format: (v, k, imp) => speedText(k, v, imp) },
  { key: 'power', title: 'Power', color: '#f59e0b', format: v => `${Math.round(v)} W` },
  { key: 'cadence', title: 'Cadence', color: '#a855f7', format: v => `${Math.round(v)}` },
  { key: 'stride_length', title: 'Stride length', color: '#64748b', format: v => `${v.toFixed(2)} m` },
  { key: 'ground_contact', title: 'Ground contact', color: '#64748b', format: v => `${Math.round(v)} ms` },
  { key: 'vertical_oscillation', title: 'Vertical oscillation', color: '#64748b', format: v => `${v.toFixed(1)} cm` },
]

function avg(pts: [number, number][]) { return pts.length ? pts.reduce((a, p) => a + p[1], 0) / pts.length : 0 }

/** A chart in a card, with its headline number on the right. */
export function ChartCard({ title, value, unit, note, color, children }: { title: string; value?: string; unit?: string; note?: string; color: string; children: React.ReactNode }) {
  return <section className="card wo-chart">
    <div className="wo-chart-head"><span className="wo-chart-dot" style={{ background: color }} /><h2 className="grow">{title}</h2>
      {value && <span className="wo-chart-v tnum">{value}{unit && <small>{unit}</small>}{note && <span className="wo-chart-note">{note}</span>}</span>}</div>
    <div className="wo-chart-body">{children}</div>
  </section>
}

/** One chart per series the workout has, x in minutes:seconds from the start. */
export function SeriesCharts({ track, kind, imperial }: { track: WorkoutTrack; kind: string; imperial: boolean }) {
  return <>{CHARTS.filter(c => (track.series[c.key]?.length || 0) > 1).map(c => {
    const pts = track.series[c.key]
    const title = c.key === 'speed' && !PACED.has(kind) ? 'Speed' : c.key === 'cadence' && kind === 'cycling' ? 'Cadence (rpm)' : c.key === 'cadence' ? 'Cadence (steps/min)' : c.title
    const max = Math.max(...pts.map(p => p[1]))
    const [v, u] = valueUnit(c.format(avg(pts), kind, imperial))
    return <ChartCard key={c.key} title={title} color={c.color} value={v} unit={c.unit || u} note={`avg · ${c.key === 'speed' ? 'best' : 'max'} ${c.format(max, kind, imperial)}`}>
      <LineChart points={thin(pts, 400).map(([t, y]) => ({ x: String(t), y }))} color={c.color} kind={c.key === 'heart_rate' ? 'area' : 'line'} xLabel={x => clock(+x)} format={y => c.format(y, kind, imperial)} height={170} />
    </ChartCard>
  })}</>
}

/** A value in a target's units: a pace or speed, bpm, W, per minute. */
function targetValue(t: StepTarget, v: number, kind: string, imperial: boolean): string {
  if (t.type === 'speed') return speedText(kind, v, imperial)
  return String(Math.round(v))
}

/** "4:45–5:05/km", "Zone 2 · 120–140", "≥ 250 W". A pace range runs from the slower end. */
function targetText(t: StepTarget, kind: string, imperial: boolean): string {
  const unit = t.type.startsWith('heart_rate') ? ' bpm' : t.type.startsWith('power') ? ' W' : t.type === 'cadence' ? '/min' : ''
  const lo = t.range_min, hi = t.range_max
  const v = (x: number) => targetValue(t, x, kind, imperial)
  const paced = t.type === 'speed' && PACED.has(kind)
  let range = ''
  if (lo && hi) range = paced ? `${v(hi).replace(/\/.*$/, '')}–${v(lo)}` : `${v(lo)}–${v(hi)}${unit}`
  else if (lo) range = paced ? `≤ ${v(lo)}` : `≥ ${v(lo)}${unit}`
  else if (hi) range = paced ? `≥ ${v(hi)}` : `≤ ${v(hi)}${unit}`
  const zone = t.zone ? `${t.type === 'power_zone' ? 'Power zone' : 'Zone'} ${t.zone}` : ''
  return [zone, range].filter(Boolean).join(' · ') + (t.metric === 'average' ? ' avg' : '')
}

const STATUS: Record<string, { label: string; cls: string }> = { in: { label: 'on target', cls: 'tg-in' }, below: { label: 'below', cls: 'tg-below' }, above: { label: 'above', cls: 'tg-above' } }

/** The target and how the step went against it: the step's average, and a bar of time below, in and above. */
function TargetCell({ t, kind, imperial }: { t: StepTarget; kind: string; imperial: boolean }) {
  const st = STATUS[t.status]
  const paced = t.type === 'speed' && PACED.has(kind)
  // below = slower on foot (a lower speed), above = faster
  const left = t.below_pct, right = t.above_pct
  const tip = t.covered_s ? `${Math.round(t.in_pct)}% of the step in range · ${Math.round(left)}% ${paced ? 'slower' : 'below'} · ${Math.round(right)}% ${paced ? 'faster' : 'above'}${t.zones_from === 'estimated' ? ' · zones estimated from max and resting heart rate (set them in Settings)' : ''}` : undefined
  return <span className="tg-cell" title={tip}>
    <span className="tg-goal">{targetText(t, kind, imperial)}</span>
    {(t.actual > 0 || st) && <span className="tg-did"><span className="tnum">{t.actual ? targetValue(t, t.actual, kind, imperial) : '—'}</span>
      {st && <span className={`tg-badge ${st.cls}`}>{paced && t.status !== 'in' ? (t.status === 'below' ? 'slower' : 'faster') : st.label}</span>}
      {t.covered_s > 0 && <span className="tg-bar" aria-hidden="true"><i className="tg-below" style={{ width: `${left}%` }} /><i className="tg-in" style={{ width: `${t.in_pct}%` }} /><i className="tg-above" style={{ width: `${right}%` }} /></span>}
      {t.covered_s > 0 && <span className="muted small tnum">{Math.round(t.in_pct)}%</span>}
    </span>}
  </span>
}

const PURPOSE: Record<string, string> = { warmup: 'Warm-up', work: 'Work', recovery: 'Recovery', cooldown: 'Cool-down' }

/** Intervals, laps or splits with time, distance, pace and the averages over each.
 *  The pace column carries a bar, longer for the faster parts, so the rhythm of a session shows at a glance. */
export function PartsTable({ title, sub, parts, kind, imperial, first }: { title: string; sub?: string; parts: TrackPart[]; kind: string; imperial: boolean; first: string }) {
  if (!parts.length) return null
  const has = (k: keyof TrackPart) => parts.some(p => !!p[k])
  const cols = { target: has('target'), goal: has('goal'), dist: has('distance_m'), speed: has('speed_ms'), hr: has('hr_avg'), power: has('power_avg'), cad: has('cadence_avg'), climb: has('climb_m') }
  // a part of a few metres has no meaningful pace
  const speedOf = (p: TrackPart) => p.distance_m >= 50 && p.speed_ms ? p.speed_ms : 0
  const top = Math.max(...parts.map(speedOf), 0)
  const low = Math.min(...parts.map(speedOf).filter(v => v > 0), top)
  const barPct = (s: number) => !s || !top ? 0 : top === low ? 100 : 35 + 65 * ((s - low) / (top - low))
  return <section className="card"><div className="card-head"><div className="grow"><h2>{title}</h2>{sub && <p className="muted small">{sub}</p>}</div></div>
    <div className="card-body flush"><table className="table parts"><thead><tr>
      <th>{first}</th>{cols.goal && <th>Goal</th>}{cols.target && <th>Target · done</th>}<th className="right">Time</th>{cols.dist && <th className="right">Distance</th>}{cols.speed && <th className="pace-col">{PACED.has(kind) ? 'Pace' : 'Speed'}</th>}
      {cols.hr && <th className="right">Heart rate</th>}{cols.power && <th className="right">Power</th>}{cols.cad && <th className="right">Cadence</th>}{cols.climb && <th className="right">Climb</th>}
    </tr></thead><tbody>{parts.map((p, i) => {
      const s = speedOf(p)
      return <tr key={i} className={p.purpose ? `purpose-${p.purpose}` : ''}>
        <td className="part-name">{p.kind === 'split' ? (p.n && Math.abs(p.distance_m - (imperial ? 1609.344 : 1000)) > 2 ? fmtDistance(p.distance_m) : p.n) : <span className="row" style={{ gap: 8 }}>{p.purpose && <i className="purpose-dot" />}{p.label || PURPOSE[p.purpose || ''] || p.kind}</span>}</td>
        {cols.goal && <td className="muted">{p.goal || ''}</td>}
        {cols.target && <td>{p.target ? <TargetCell t={p.target} kind={kind} imperial={imperial} /> : ''}</td>}
        <td className="right tnum">{clock(p.duration_s)}</td>
        {cols.dist && <td className="right tnum">{p.distance_m ? fmtDistance(p.distance_m) : '—'}</td>}
        {cols.speed && <td className="pace-col"><span className="pace-cell"><span className="pace-bar"><i style={{ width: `${barPct(s)}%` }} /></span><span className="tnum">{s ? speedText(kind, s, imperial) : '—'}</span></span></td>}
        {cols.hr && <td className="right tnum">{p.hr_avg ? <>{p.hr_avg}{p.hr_max ? <span className="muted"> / {p.hr_max}</span> : null}</> : '—'}</td>}
        {cols.power && <td className="right tnum">{p.power_avg ? `${p.power_avg} W` : '—'}</td>}
        {cols.cad && <td className="right tnum">{p.cadence_avg || '—'}</td>}
        {cols.climb && <td className="right tnum">{Math.round(p.climb_m)} m</td>}
      </tr>
    })}</tbody></table></div></section>
}
