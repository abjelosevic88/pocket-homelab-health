import { useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { RANGES, type MetricPage as Page, type Sample } from '../lib/types'
import { fmtDate, fmtDateTime } from '../lib/format'
import { display, fmtMetric, fmtMinutes, fmtPair, unitOf } from '../lib/units'
import { Confirm, Loading, PageHeader, Segmented, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Bars, LineChart } from '../components/Chart'
import { GoalModal, LogModal } from '../components/forms'
import { openCreate, useCrumb, useOnChange } from '../components/Layout'

export default function MetricPage() {
  const { key = '' } = useParams()
  const [sp, setSp] = useSearchParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const range = sp.get('range') || 'm'
  const [page, setPage] = useState<Page | null>(null)
  const [goal, setGoal] = useState(false)
  const [edit, setEdit] = useState<Sample | null>(null)
  const [del, setDel] = useState<Sample | null>(null)
  const load = () => api.get<Page>(`${V1}/metrics/${key}?range=${range}`).then(setPage).catch(e => toast((e as Error).message, 'err'))
  useEffect(() => { load() }, [key, range]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(load)
  useCrumb(page?.metric.name)
  if (!page) return <Loading />
  const m = page.metric
  const sleep = m.key.startsWith('sleep_')
  const fmtV = (v: number) => sleep ? fmtMinutes(v) : fmtMetric(m, v, false)
  const pts = page.points.map(p => ({ x: p.date, y: display(m, p.value)[0], y2: p.value2 !== undefined && p.value2 !== null ? display(m, p.value2)[0] : undefined }))
  const filled = fillDates(page.from, page.to, pts)
  const st = page.stats
  const goalV = page.goal ? display(m, page.goal.target)[0] : undefined
  const kind = m.agg === 'sum' ? 'bar' : m.agg === 'last' ? 'area' : 'line'
  const chg = st.change_pct
  const good = chg === undefined ? '' : (chg > 0 ? (m.higher_is_better === false ? 'bad' : m.higher_is_better ? 'ok' : '') : chg < 0 ? (m.higher_is_better === false ? 'ok' : m.higher_is_better ? 'bad' : '') : '')
  const removeSample = async (s: Sample) => { try { await api.del(`${V1}/samples/${s.id}`); setDel(null); load() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <PageHeader title={<span className="row" style={{ gap: 10 }}><span className="tile-ico lg" style={{ '--tile': m.color } as React.CSSProperties}><Icon name={m.icon} size={18} /></span>{m.name}</span>}
        sub={page.latest ? <>Latest {m.pair ? fmtPair(m, page.latest.value, page.latest.value2) : fmtV(page.latest.value)}{!sleep && !m.pair && !['count', '1–5', 'drinks'].includes(unitOf(m)) ? ' ' + unitOf(m) : ''} · {fmtDate(page.latest.date)}</> : 'No data yet'}
        actions={<>
          {m.manual && <button type="button" className="btn primary" onClick={() => openCreate('log', m.key)}><Icon name="plus" size={15} />Log</button>}
          {m.goal_dir || page.goal ? <button type="button" className="btn" onClick={() => setGoal(true)}><Icon name="target" size={15} />{page.goal ? 'Edit goal' : 'Set a goal'}</button> : null}
        </>} />
      <div className="scroll-x"><Segmented size="sm" value={range} onChange={v => setSp({ range: v })} options={RANGES.map(r => ({ value: r.id, label: r.label }))} /></div>

      <div className="stats-row">
        <Stat label={m.agg === 'sum' ? 'Daily average' : 'Average'} value={st.avg !== undefined ? fmtV(display(m, st.avg)[0]) : '—'} sub={chg !== undefined ? <span className={`chg ${good}`}>{chg > 0 ? '+' : ''}{chg}% vs previous {page.points.length ? RANGES.find(r => r.id === range)?.label.toLowerCase() : ''}</span> : undefined} />
        {m.agg === 'sum' && <Stat label="Total" value={st.sum !== undefined ? fmtV(display(m, st.sum)[0]) : '—'} />}
        <Stat label={m.higher_is_better === false ? 'Lowest' : 'Highest'} value={st.max !== undefined ? fmtV(display(m, m.higher_is_better === false ? st.min! : st.max)[0]) : '—'} />
        <Stat label={m.higher_is_better === false ? 'Highest' : 'Lowest'} value={st.min !== undefined ? fmtV(display(m, m.higher_is_better === false ? st.max! : st.min)[0]) : '—'} />
        {st.per_week !== undefined && <Stat label="Trend" value={`${st.per_week > 0 ? '+' : ''}${display(m, st.per_week)[0].toFixed(m.decimals)} ${unitOf(m)}/week`} />}
        {page.goal && <Stat label="Goal streak" value={`${page.streak} day${page.streak === 1 ? '' : 's'}`} sub={`best ${page.best_streak}`} />}
        <Stat label="Days with data" value={String(st.days)} />
      </div>

      <section className="card">
        <div className="card-body">
          {page.points.length === 0 ? <p className="muted">Nothing in this range.</p>
            : <LineChart points={filled} kind={kind} color={m.color} goal={goalV} goalLabel={page.goal ? `goal ${page.goal.direction === 'at_most' ? '≤' : '≥'} ${fmtV(goalV!)}` : undefined} format={v => fmtV(v)} zero={m.agg === 'sum'} avg={st.avg !== undefined && m.agg !== 'sum' ? display(m, st.avg)[0] : undefined} height={220} />}
        </div>
      </section>

      <div className="two-col">
        {page.weekday.some(w => w.n > 0) && <section className="card"><div className="card-head"><div className="grow"><h2>By weekday</h2><p className="muted small">Average per day of the week in this range</p></div></div><div className="card-body">
          <Bars items={page.weekday.map(w => ({ label: w.name, value: display(m, w.avg)[0], hint: `${w.n} days` }))} color={m.color} format={v => fmtV(v)} />
        </div></section>}
        {page.weekly.length > 1 && <section className="card"><div className="card-head"><div className="grow"><h2>Week by week</h2><p className="muted small">{m.agg === 'sum' ? 'Weekly totals' : 'Weekly averages'}</p></div></div><div className="card-body">
          <Bars items={page.weekly.slice(-12).map(w => ({ label: fmtDate(w.date, false), value: display(m, w.value)[0], hint: `${w.n} days` }))} color={m.color} format={v => fmtV(v)} />
        </div></section>}
      </div>

      {page.goal && page.goal_hits && <section className="card"><div className="card-head"><div className="grow"><h2>Goal days</h2><p className="muted small">{Object.keys(page.goal_hits).length} of {page.points.length} days {page.goal.direction === 'at_most' ? 'under' : 'at or above'} {fmtV(goalV!)}</p></div></div><div className="card-body">
        <div className="hit-grid">{fillDates(page.from, page.to, []).map(d => <span key={d.x} className={`hit ${page.goal_hits![d.x] ? 'yes' : pts.some(p => p.x === d.x) ? 'no' : ''}`} title={`${fmtDate(d.x)}${page.goal_hits![d.x] ? ' · met' : ''}`} />)}</div>
      </div></section>}

      <section className="card">
        <div className="card-head"><div className="grow"><h2>Recent entries</h2><p className="muted small">Every reading, newest first</p></div></div>
        <div className="card-body flush">
          {page.recent.length === 0 ? <p className="muted pad">None.</p> : <table className="table compact">
            <thead><tr><th>When</th><th>Value</th><th>Source</th><th>Note</th><th /></tr></thead>
            <tbody>{page.recent.map(s => <tr key={s.id}>
              <td>{fmtDateTime(s.start_at)}</td>
              <td className="tnum">{m.pair ? fmtPair(m, s.value, s.value2) : fmtV(display(m, s.value)[0])}{s.value_min !== undefined && s.value_max !== undefined ? <span className="muted small"> ({Math.round(s.value_min)}–{Math.round(s.value_max)})</span> : ''}</td>
              <td className="muted">{s.source}{s.device ? ` · ${s.device}` : ''}</td>
              <td className="muted">{s.note}</td>
              <td className="right">{(s.source === 'manual' || s.source === 'api' || s.source === 'csv') && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(s)}>Edit</button><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(s)}>Delete</button></span>}</td>
            </tr>)}</tbody>
          </table>}
        </div>
      </section>
      {goal && <GoalModal metric={m} goal={page.goal} onClose={() => setGoal(false)} onSaved={() => { setGoal(false); load() }} />}
      {page.goal && <p className="muted small">Remove the goal: <button type="button" className="link-btn" onClick={async () => { await api.del(`${V1}/goals/${m.key}`); load() }}>stop tracking this goal</button></p>}
      {edit && <LogModal sample={edit} metric={m.key} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); load() }} />}
      {del && <Confirm title="Delete this entry?" message={`${fmtDateTime(del.start_at)}: ${fmtV(display(m, del.value)[0])}`} confirmLabel="Delete" onCancel={() => setDel(null)} onConfirm={() => removeSample(del)} />}
      <p className="muted small"><button type="button" className="link-btn" onClick={() => navigate('/metrics')}>← All metrics</button></p>
    </div>
  )
}

function Stat({ label, value, sub }: { label: string; value: string; sub?: React.ReactNode }) {
  return <div className="stat"><span className="stat-l">{label}</span><span className="stat-v tnum">{value}</span>{sub && <span className="stat-s small">{sub}</span>}</div>
}

/** Every date in the range with the point or a gap (null). */
export function fillDates(from: string, to: string, pts: { x: string; y: number | null; y2?: number | null }[]) {
  const by = new Map(pts.map(p => [p.x, p]))
  const out: { x: string; y: number | null; y2?: number | null }[] = []
  const [y, m, d] = from.split('-').map(Number)
  const [y2, m2, d2] = to.split('-').map(Number)
  const end = Date.UTC(y2, m2 - 1, d2)
  for (let t = Date.UTC(y, m - 1, d), i = 0; t <= end && i < 800; t += 86400000, i++) {
    const dt = new Date(t).toISOString().slice(0, 10)
    out.push(by.get(dt) || { x: dt, y: null })
  }
  // too many points for a chart: fold to weekly averages beyond ~200 days
  if (out.length > 200) {
    const folded: typeof out = []
    for (let i = 0; i < out.length; i += 7) {
      const chunk = out.slice(i, i + 7).filter(p => p.y !== null)
      folded.push({ x: out[i].x, y: chunk.length ? chunk.reduce((a, p) => a + (p.y as number), 0) / chunk.length : null })
    }
    return folded
  }
  return out
}
