import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { RANGES, type Insights as Data } from '../lib/types'
import { fmtDate } from '../lib/format'
import { display, fmtMetric, fmtMinutes } from '../lib/units'
import { Loading, PageHeader, Segmented, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Bars } from '../components/Chart'
import { AIStatusLine, useAIStatus } from '../components/forms'
import { useCatalog, useOnChange } from '../components/Layout'

export default function Insights() {
  const [sp, setSp] = useSearchParams()
  const range = sp.get('range') || 'm'
  const catalog = useCatalog()
  const { toast } = useToast()
  const ai = useAIStatus()
  const [data, setData] = useState<Data | null>(null)
  const [recap, setRecap] = useState<{ text: string; from: string; to: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [q, setQ] = useState('')
  const [answer, setAnswer] = useState('')
  const load = () => api.get<Data>(`${V1}/insights?range=${range}`).then(setData).catch(() => {})
  useEffect(() => { load() }, [range]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(load)
  if (!data) return <Loading />
  const metricOf = (k: string) => catalog?.metrics.find(m => m.key === k)
  const fmtK = (k: string, v: number) => { const m = metricOf(k); return m ? (k.startsWith('sleep_') ? fmtMinutes(v) : fmtMetric(m, v)) : String(v) }
  const doRecap = async () => { setBusy(true); try { const r = await api.post<{ ok: boolean; error?: string; text: string; from: string; to: string }>(`${V1}/ai/recap`, { range }); if (r.ok) setRecap(r); else toast(r.error || 'failed', 'err') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) } }
  const ask = async (e: React.FormEvent) => { e.preventDefault(); if (!q.trim()) return; setBusy(true); try { const r = await api.post<{ ok: boolean; error?: string; text: string }>(`${V1}/ai/ask`, { question: q, range }); setAnswer(r.ok ? r.text : r.error || 'failed') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) } }
  const w = data.workouts
  const sleep = data.sleep
  return (
    <div className="stack loose">
      <PageHeader title="Insights" sub={`${fmtDate(data.from)} – ${fmtDate(data.to)} · ${data.days} days, compared with the ${data.days} before`} />
      <div className="scroll-x"><Segmented size="sm" value={range} onChange={v => setSp({ range: v })} options={RANGES.filter(r => r.id !== 'all').map(r => ({ value: r.id, label: r.label }))} /></div>

      <section className="card"><div className="card-head"><div className="grow"><h2>In words</h2><p className="muted small">A recap written from your numbers, and questions about them.</p></div>
        <button type="button" className="btn primary sm" onClick={doRecap} disabled={busy || !ai?.ready}><Icon name="sparkles" size={14} />{busy ? 'Thinking…' : 'Write a recap'}</button></div>
        <div className="card-body stack">
          <AIStatusLine st={ai} />
          {recap && <div className="recap"><p style={{ whiteSpace: 'pre-wrap' }}>{recap.text}</p></div>}
          {ai?.ready && <form onSubmit={ask} className="row"><input value={q} onChange={e => setQ(e.target.value)} placeholder="Do I sleep better on days I run?" /><button className="btn" disabled={busy}>Ask</button></form>}
          {answer && <div className="recap"><p style={{ whiteSpace: 'pre-wrap' }}>{answer}</p></div>}
        </div>
      </section>

      {data.trends.length > 0 && <section className="card"><div className="card-head"><div className="grow"><h2>Trends</h2><p className="muted small">Average per day against the period before</p></div></div><div className="card-body flush">
        <table className="table compact trends">
          <thead><tr><th>Metric</th><th className="right">Average</th><th className="right">Before</th><th className="right">Change</th><th>Best day</th></tr></thead>
          <tbody>{data.trends.map(t => {
            const good = t.change_pct > 0 ? (t.metric.higher_is_better === false ? 'bad' : t.metric.higher_is_better ? 'ok' : '') : t.change_pct < 0 ? (t.metric.higher_is_better === false ? 'ok' : t.metric.higher_is_better ? 'bad' : '') : ''
            return <tr key={t.metric.key}>
              <td><Link to={`/metrics/${t.metric.key}`} className="row" style={{ gap: 8 }}><span className="tile-ico sm" style={{ '--tile': t.metric.color } as React.CSSProperties}><Icon name={t.metric.icon} size={13} /></span>{t.metric.name}</Link></td>
              <td className="right tnum"><b>{fmtK(t.metric.key, t.avg)}</b>{t.per_week ? <span className="muted small block">{t.per_week > 0 ? '+' : ''}{display(t.metric, t.per_week)[0].toFixed(t.metric.decimals)}/week</span> : null}</td>
              <td className="right tnum muted">{t.prev_avg ? fmtK(t.metric.key, t.prev_avg) : '—'}</td>
              <td className={`right tnum chg ${good}`}>{t.prev_avg ? `${t.change_pct > 0 ? '+' : ''}${t.change_pct}%` : ''}</td>
              <td className="muted small">{fmtK(t.metric.key, t.best.value)} on {fmtDate(t.best.date, false)}</td>
            </tr>
          })}</tbody>
        </table>
      </div></section>}

      <div className="two-col">
        {Object.keys(data.patterns).length > 0 && <section className="card"><div className="card-head"><div className="grow"><h2>Weekday patterns</h2><p className="muted small">Which days you move and rest</p></div></div><div className="card-body stack">
          {Object.entries(data.patterns).map(([k, rows]) => { const m = metricOf(k); return m ? <div key={k}><div className="small muted mb">{m.name}</div><Bars items={rows.map(r => ({ label: r.name, value: display(m, r.avg)[0], hint: `${r.n} days` }))} color={m.color} format={v => fmtK(k, v)} height={90} /></div> : null })}
        </div></section>}
        <div className="stack">
          <section className="card"><div className="card-head"><div className="grow"><h2>Workouts</h2><p className="muted small">{w.count} ({w.minutes} min) · before: {w.prev_count} ({w.prev_minutes} min)</p></div></div><div className="card-body">
            {w.by_kind.length ? <Bars items={w.by_kind.map(k => ({ label: k.name, value: Math.round(k.duration_s / 60), hint: `${k.count} ×` }))} format={v => `${v} min`} height={100} /> : <p className="muted small">No workouts in this range.</p>}
          </div></section>
          {sleep.nights > 0 && <section className="card"><div className="card-head"><div className="grow"><h2>Sleep regularity</h2><p className="muted small">{sleep.nights} nights</p></div></div><div className="card-body">
            <div className="stats-row tight">
              <div className="stat"><span className="stat-l">Bedtime</span><span className="stat-v tnum">{sleep.bed_avg}</span><span className="stat-s small">± {sleep.bed_sd_min} min</span></div>
              <div className="stat"><span className="stat-l">Wake</span><span className="stat-v tnum">{sleep.wake_avg}</span><span className="stat-s small">± {sleep.wake_sd_min} min</span></div>
              <div className="stat"><span className="stat-l">Asleep</span><span className="stat-v tnum">{fmtMinutes(sleep.asleep_avg_min)}</span><span className="stat-s small">± {sleep.asleep_sd_min} min</span></div>
              <div className="stat"><span className="stat-l">Regularity</span><span className="stat-v tnum">{sleep.score}/100</span></div>
            </div>
          </div></section>}
        </div>
      </div>

      {data.correlations.length > 0 && <section className="card"><div className="card-head"><div className="grow"><h2>Patterns</h2><p className="muted small">Links between your days — correlation, not cause.</p></div></div><div className="card-body stack">
        {data.correlations.map(c => <div key={c.a + c.b} className="corr"><span className={`corr-r ${Math.abs(c.r) >= 0.4 ? 'strong' : ''}`}>{c.r > 0 ? '+' : ''}{c.r}</span><span>{c.text}</span></div>)}
      </div></section>}

      {data.weather && <section className="card"><div className="card-head"><div className="grow"><h2>Weather</h2><p className="muted small">Your averages by the day's weather ({data.weather.days} days)</p></div></div><div className="card-body flush">
        <table className="table compact"><thead><tr><th>Days like</th><th className="right">n</th>{data.weather.metrics.map(k => <th key={k} className="right">{metricOf(k)?.name || k}</th>)}</tr></thead>
          <tbody>{[...data.weather.by_rain, ...data.weather.by_temperature, ...data.weather.by_condition].map(r => <tr key={String(r.key)}><td>{String(r.label)}</td><td className="right muted">{String(r.days)}</td>{data.weather!.metrics.map(k => <td key={k} className="right tnum">{r[k] !== undefined ? fmtK(k, Number(r[k])) : <span className="muted">—</span>}</td>)}</tr>)}</tbody></table>
      </div></section>}
      {!data.weather && <p className="muted small">Switch weather on under <Link to="/settings/general">Settings → General</Link> to see how rain and temperature change your days.</p>}

      {data.goals.length > 0 && <section className="card"><div className="card-head"><div className="grow"><h2>Goals</h2><p className="muted small">Days met in this range</p></div></div><div className="card-body flush"><table className="table compact">
        <thead><tr><th>Goal</th><th className="right">Met</th><th className="right">Streak</th><th className="right">Best</th></tr></thead>
        <tbody>{data.goals.map(g => <tr key={g.metric}><td><Link to={`/metrics/${g.metric}`}>{g.name}</Link> <span className="muted small">{g.direction === 'at_most' ? '≤' : '≥'} {fmtK(g.metric, g.target)}</span></td><td className="right tnum">{g.met}/{g.days} <span className="muted small">({g.pct}%)</span></td><td className="right tnum">{g.streak}</td><td className="right tnum">{g.best}</td></tr>)}</tbody>
      </table></div></section>}
    </div>
  )
}
