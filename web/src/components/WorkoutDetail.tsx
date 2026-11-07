// What was recorded along a workout: the series as charts over time, and the
// parts (intervals, laps, splits) as tables with what each one took.
import { LineChart } from './Chart'
import { clock, thin } from '../lib/track'
import type { TrackPart, WorkoutTrack } from '../lib/types'
import { fmtDistance, fmtPace } from '../lib/units'

const PACED = new Set(['running', 'walking', 'hiking'])

/** How a speed in m/s reads for this kind of workout: a pace on foot, km/h (mph) otherwise. */
export function speedText(kind: string, ms: number, imperial: boolean): string {
  if (!ms) return '—'
  if (PACED.has(kind)) return fmtPace(1000 / ms)
  return imperial ? `${(ms * 2.236936).toFixed(1)} mph` : `${(ms * 3.6).toFixed(1)} km/h`
}

const CHARTS: { key: string; title: string; color: string; format: (v: number, kind: string, imperial: boolean) => string }[] = [
  { key: 'heart_rate', title: 'Heart rate', color: '#ef4444', format: v => `${Math.round(v)}` },
  { key: 'speed', title: 'Pace', color: '#06b6d4', format: (v, k, imp) => speedText(k, v, imp) },
  { key: 'power', title: 'Power', color: '#f59e0b', format: v => `${Math.round(v)} W` },
  { key: 'cadence', title: 'Cadence', color: '#a855f7', format: v => `${Math.round(v)}` },
  { key: 'stride_length', title: 'Stride length', color: '#64748b', format: v => `${v.toFixed(2)} m` },
  { key: 'ground_contact', title: 'Ground contact', color: '#64748b', format: v => `${Math.round(v)} ms` },
  { key: 'vertical_oscillation', title: 'Vertical oscillation', color: '#64748b', format: v => `${v.toFixed(1)} cm` },
]

function avg(pts: [number, number][]) { return pts.length ? pts.reduce((a, p) => a + p[1], 0) / pts.length : 0 }

/** One chart per series the workout has, x in minutes:seconds from the start. */
export function SeriesCharts({ track, kind, imperial }: { track: WorkoutTrack; kind: string; imperial: boolean }) {
  return <>{CHARTS.filter(c => (track.series[c.key]?.length || 0) > 1).map(c => {
    const pts = track.series[c.key]
    const title = c.key === 'speed' && !PACED.has(kind) ? 'Speed' : c.key === 'cadence' && kind === 'cycling' ? 'Cadence (rpm)' : c.key === 'cadence' ? 'Cadence (steps/min)' : c.title
    const max = Math.max(...pts.map(p => p[1]))
    return <section key={c.key} className="card"><div className="card-head"><div className="grow"><h2>{title}</h2>
      <p className="muted small">Average {c.format(avg(pts), kind, imperial)}{c.key !== 'speed' ? ` · max ${c.format(max, kind, imperial)}` : ` · best ${c.format(max, kind, imperial)}`}</p></div></div>
      <div className="card-body"><LineChart points={thin(pts, 400).map(([t, v]) => ({ x: String(t), y: v }))} color={c.color} kind={c.key === 'heart_rate' ? 'area' : 'line'} xLabel={x => clock(+x)} format={v => c.format(v, kind, imperial)} /></div></section>
  })}</>
}

const PURPOSE: Record<string, string> = { warmup: 'Warm-up', work: 'Work', recovery: 'Recovery', cooldown: 'Cool-down' }

/** Intervals, laps or splits with time, distance, pace and the averages over each. */
export function PartsTable({ title, sub, parts, kind, imperial, first }: { title: string; sub?: string; parts: TrackPart[]; kind: string; imperial: boolean; first: string }) {
  if (!parts.length) return null
  const has = (k: keyof TrackPart) => parts.some(p => !!p[k])
  const cols = { goal: has('goal'), dist: has('distance_m'), speed: has('speed_ms'), hr: has('hr_avg'), power: has('power_avg'), cad: has('cadence_avg'), climb: has('climb_m') }
  return <section className="card"><div className="card-head"><div className="grow"><h2>{title}</h2>{sub && <p className="muted small">{sub}</p>}</div></div>
    <div className="card-body flush"><table className="table compact parts"><thead><tr>
      <th>{first}</th>{cols.goal && <th>Goal</th>}<th className="right">Time</th>{cols.dist && <th className="right">Distance</th>}{cols.speed && <th className="right">{PACED.has(kind) ? 'Pace' : 'Speed'}</th>}
      {cols.hr && <th className="right">Heart rate</th>}{cols.power && <th className="right">Power</th>}{cols.cad && <th className="right">Cadence</th>}{cols.climb && <th className="right">Climb</th>}
    </tr></thead><tbody>{parts.map((p, i) => <tr key={i} className={p.purpose ? `purpose-${p.purpose}` : ''}>
      <td>{p.kind === 'split' ? (p.n && Math.abs(p.distance_m - (imperial ? 1609.344 : 1000)) > 2 ? fmtDistance(p.distance_m) : p.n) : <span className="row" style={{ gap: 6 }}>{p.purpose && <i className="purpose-dot" />}{p.label || PURPOSE[p.purpose || ''] || p.kind}</span>}</td>
      {cols.goal && <td className="muted">{p.goal || ''}</td>}
      <td className="right tnum">{clock(p.duration_s)}</td>
      {cols.dist && <td className="right tnum">{p.distance_m ? fmtDistance(p.distance_m) : '—'}</td>}
      {cols.speed && <td className="right tnum">{speedText(kind, p.speed_ms || 0, imperial)}</td>}
      {cols.hr && <td className="right tnum">{p.hr_avg ? `${p.hr_avg}${p.hr_max ? ` / ${p.hr_max}` : ''}` : '—'}</td>}
      {cols.power && <td className="right tnum">{p.power_avg ? `${p.power_avg} W` : '—'}</td>}
      {cols.cad && <td className="right tnum">{p.cadence_avg || '—'}</td>}
      {cols.climb && <td className="right tnum">{Math.round(p.climb_m)} m</td>}
    </tr>)}</tbody></table></div></section>
}
