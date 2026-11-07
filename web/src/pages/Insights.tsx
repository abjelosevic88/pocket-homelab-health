import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { RANGES, type Insights as Data, type Metric, type Trend } from '../lib/types'
import { fmtDate } from '../lib/format'
import { display, fmtMetric, fmtMinutes } from '../lib/units'
import { Loading, PageHeader, Segmented, useToast } from '../components/ui'
import { Icon, KIND_ICON } from '../components/icons'
import { Ring, Sparkline } from '../components/Chart'
import { AIStatusLine, useAIStatus } from '../components/forms'
import { useCatalog, useOnChange } from '../components/Layout'

/** Whether a change is good news: up is good for steps, bad for resting heart rate, neither for some. */
function verdict(m: Metric, pct: number): '' | 'ok' | 'bad' {
  if (!pct || m.higher_is_better === null || m.higher_is_better === undefined) return ''
  return (pct > 0) === m.higher_is_better ? 'ok' : 'bad'
}

function ChangePill({ pct, tone }: { pct: number; tone: '' | 'ok' | 'bad' }) {
  if (!pct) return <span className="chg-pill">±0%</span>
  return <span className={`chg-pill ${tone}`}><Icon name={pct > 0 ? 'arrow-up' : 'arrow-down'} size={11} />{Math.abs(pct)}%</span>
}

/** Seven bars, Monday to Sunday, the strongest day in full colour. */
function WeekBars({ rows, color, format }: { rows: { name: string; avg: number; n: number }[]; color: string; format: (v: number) => string }) {
  const max = Math.max(...rows.map(r => r.avg), 0) || 1
  const top = rows.reduce((b, r) => (r.avg > b.avg ? r : b), rows[0])
  return <div className="week-bars">{rows.map(r => {
    const best = r === top
    return <div key={r.name} className={`week-col ${best ? 'best' : ''}`} title={`${r.name}: ${format(r.avg)} on average (${r.n} days)`}>
      <span className="week-val tnum">{format(r.avg)}</span>
      <span className="week-track"><i style={{ height: `${Math.max(4, (r.avg / max) * 100)}%`, background: color }} /></span>
      <span className="week-lbl">{r.name}</span>
    </div>
  })}</div>
}

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
  const short = (k: string, v: number) => { const m = metricOf(k); if (!m) return String(v); const d = display(m, v)[0]; return Math.abs(d) >= 10000 ? `${(d / 1000).toFixed(1)}k` : fmtMetric(m, v, false) }
  const doRecap = async () => { setBusy(true); try { const r = await api.post<{ ok: boolean; error?: string; text: string; from: string; to: string }>(`${V1}/ai/recap`, { range }); if (r.ok) setRecap(r); else toast(r.error || 'failed', 'err') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) } }
  const ask = async (e: React.FormEvent) => { e.preventDefault(); if (!q.trim()) return; setBusy(true); try { const r = await api.post<{ ok: boolean; error?: string; text: string }>(`${V1}/ai/ask`, { question: q, range }); setAnswer(r.ok ? r.text : r.error || 'failed') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) } }
  const w = data.workouts
  const sleep = data.sleep
  const compared = data.trends.filter(t => t.prev_avg)
  // the biggest moves first, good or bad, so the strip says what changed
  const highlights = [...compared].sort((a, b) => Math.abs(b.change_pct) - Math.abs(a.change_pct)).slice(0, 4)
  const pct = (a: number, b: number) => b ? Math.round(((a - b) / b) * 100) : 0
  const kindMax = Math.max(...w.by_kind.map(k => k.duration_s), 1)
  const spark = (t: Trend) => t.weekly.map(p => p.value)
  return (
    <div className="stack loose ins-page">
      <PageHeader title="Insights" sub={`${fmtDate(data.from)} – ${fmtDate(data.to)} · compared with the ${data.days} days before`}
        actions={<div className="scroll-x"><Segmented size="sm" value={range} onChange={v => setSp({ range: v })} options={RANGES.filter(r => r.id !== 'all').map(r => ({ value: r.id, label: r.label }))} /></div>} />

      {highlights.length > 0 && <div className="ins-hl">{highlights.map(t =>
        <Link key={t.metric.key} to={`/metrics/${t.metric.key}`} className="ins-hl-card" style={{ '--tile': t.metric.color } as React.CSSProperties}>
          <span className="ins-hl-top"><span className="tile-ico sm"><Icon name={t.metric.icon} size={13} /></span><span className="ins-hl-name">{t.metric.name}</span></span>
          <span className="ins-hl-v tnum">{fmtK(t.metric.key, t.avg)}</span>
          <span className="ins-hl-foot"><ChangePill pct={t.change_pct} tone={verdict(t.metric, t.change_pct)} /><span className="muted small">a day, was {fmtK(t.metric.key, t.prev_avg)}</span></span>
          {t.weekly.length > 1 && <span className="ins-hl-spark"><Sparkline values={spark(t)} color={t.metric.color} width={220} height={34} fluid /></span>}
        </Link>)}</div>}

      <section className="card ins-ai"><div className="ins-ai-head">
        <span className="ins-ai-ico"><Icon name="sparkles" size={16} /></span>
        <div className="grow" style={{ minWidth: 0 }}><h2>In words</h2><p className="muted small">A recap written from your numbers, and questions about them.</p></div>
        <button type="button" className="btn primary sm" onClick={doRecap} disabled={busy || !ai?.ready}><Icon name="sparkles" size={14} />{busy ? 'Thinking…' : 'Write a recap'}</button></div>
        <div className="ins-ai-body stack">
          <AIStatusLine st={ai} />
          {recap && <div className="recap"><p style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{recap.text}</p></div>}
          {ai?.ready && <form onSubmit={ask} className="row"><input value={q} onChange={e => setQ(e.target.value)} placeholder="Do I sleep better on days I run?" /><button className="btn" disabled={busy}>Ask</button></form>}
          {answer && <div className="recap"><p style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{answer}</p></div>}
        </div>
      </section>

      {data.trends.length > 0 && <section className="card"><div className="card-head"><div className="grow"><h2>Trends</h2><p className="muted small">Average per day, against the {data.days} days before</p></div></div>
        <div className="ins-trends">
          <div className="ins-trow ins-thead"><span>Metric</span><span className="ins-c-spark">Weekly</span><span className="right">Average</span><span className="right">Change</span><span className="right ins-c-before">Before</span><span className="ins-c-best">Best day</span></div>
          {data.trends.map(t => <Link key={t.metric.key} to={`/metrics/${t.metric.key}`} className="ins-trow">
            <span className="ins-c-name"><span className="tile-ico sm" style={{ '--tile': t.metric.color } as React.CSSProperties}><Icon name={t.metric.icon} size={13} /></span><span className="ins-ellipsis">{t.metric.name}</span></span>
            <span className="ins-c-spark">{t.weekly.length > 1 ? <Sparkline values={spark(t)} color={t.metric.color} width={110} height={26} /> : null}</span>
            <span className="right tnum ins-c-avg"><b>{fmtK(t.metric.key, t.avg)}</b>{t.per_week ? <span className="muted xsmall block">{t.per_week > 0 ? '+' : ''}{display(t.metric, t.per_week)[0].toFixed(t.metric.decimals)}/week</span> : null}</span>
            <span className="right">{t.prev_avg ? <ChangePill pct={t.change_pct} tone={verdict(t.metric, t.change_pct)} /> : <span className="muted">—</span>}</span>
            <span className="right tnum muted ins-c-before">{t.prev_avg ? fmtK(t.metric.key, t.prev_avg) : '—'}</span>
            <span className="muted small ins-c-best"><Icon name="trophy" size={12} /> {fmtK(t.metric.key, t.best.value)} · {fmtDate(t.best.date, false)}</span>
          </Link>)}
        </div>
      </section>}

      <div className="ins-grid">
        {Object.keys(data.patterns).length > 0 && <section className="card"><div className="card-head"><div className="grow"><h2>Weekday patterns</h2><p className="muted small">Your average on each day of the week</p></div></div>
          <div className="card-body stack">
            {Object.entries(data.patterns).map(([k, rows]) => {
              const m = metricOf(k)
              if (!m || !rows.length) return null
              const top = rows.reduce((b, r) => (r.avg > b.avg ? r : b), rows[0])
              const low = rows.reduce((b, r) => (r.avg < b.avg ? r : b), rows[0])
              return <div key={k} className="week-block">
                <div className="week-head"><span className="row" style={{ gap: 8 }}><span className="tile-ico sm" style={{ '--tile': m.color } as React.CSSProperties}><Icon name={m.icon} size={13} /></span><b>{m.name}</b></span>
                  <span className="muted small">most on {top.name} · least on {low.name}</span></div>
                <WeekBars rows={rows} color={m.color} format={v => short(k, v)} />
              </div>
            })}
          </div></section>}

        <div className="stack">
          <section className="card"><div className="card-head"><div className="grow"><h2>Workouts</h2><p className="muted small">Against the {data.days} days before</p></div><Link to="/workouts" className="btn ghost sm">All</Link></div>
            <div className="card-body stack">
              <div className="ins-duo">
                <div><span className="ins-duo-v tnum">{w.count}</span><span className="ins-duo-l">workouts {w.prev_count ? <ChangePill pct={pct(w.count, w.prev_count)} tone={w.count >= w.prev_count ? 'ok' : 'bad'} /> : <span className="muted">· none before</span>}</span></div>
                <div><span className="ins-duo-v tnum">{fmtMinutes(w.minutes)}</span><span className="ins-duo-l">moving {w.prev_minutes ? <ChangePill pct={pct(w.minutes, w.prev_minutes)} tone={w.minutes >= w.prev_minutes ? 'ok' : 'bad'} /> : <span className="muted">· none before</span>}</span></div>
              </div>
              {w.by_kind.length ? <div className="kind-bars">{w.by_kind.map(k => <Link key={k.kind} to={`/workouts?kind=${k.kind}`} className="kind-row">
                <span className="kind-ico"><Icon name={KIND_ICON[k.kind] || 'activity'} size={14} /></span>
                <span className="kind-main"><span className="kind-top"><b>{k.name}</b><span className="muted small tnum">{k.count} × · {fmtMinutes(Math.round(k.duration_s / 60))}</span></span>
                  <span className="kind-track"><i style={{ width: `${Math.max(3, (k.duration_s / kindMax) * 100)}%` }} /></span></span>
              </Link>)}</div> : <p className="muted small">No workouts in this range.</p>}
            </div>
          </section>

          {sleep.nights > 0 && <section className="card"><div className="card-head"><div className="grow"><h2>Sleep regularity</h2><p className="muted small">{sleep.nights} nights · the steadier the times, the higher the score</p></div></div>
            <div className="card-body ins-sleep">
              <Ring pct={sleep.score || 0} size={86} stroke={8} color="#6366f1"><span className="ins-sleep-score tnum">{sleep.score}</span></Ring>
              <div className="ins-sleep-stats">
                <div><span className="ins-sleep-l"><Icon name="bed" size={12} />Bedtime</span><b className="tnum">{sleep.bed_avg}</b><span className="muted xsmall">± {sleep.bed_sd_min} min</span></div>
                <div><span className="ins-sleep-l"><Icon name="sun" size={12} />Wake</span><b className="tnum">{sleep.wake_avg}</b><span className="muted xsmall">± {sleep.wake_sd_min} min</span></div>
                <div><span className="ins-sleep-l"><Icon name="moon" size={12} />Asleep</span><b className="tnum">{fmtMinutes(sleep.asleep_avg_min || 0)}</b><span className="muted xsmall">± {sleep.asleep_sd_min} min</span></div>
              </div>
            </div></section>}
        </div>
      </div>

      {data.correlations.length > 0 && <section className="card"><div className="card-head"><div className="grow"><h2>Links between your days</h2><p className="muted small">Things that tend to move together. Correlation, not cause.</p></div></div>
        <div className="card-body ins-corr">{data.correlations.map(c => {
          const strong = Math.abs(c.r) >= 0.4
          return <div key={c.a + c.b} className="ins-corr-row">
            <span className={`ins-corr-r ${c.r > 0 ? 'pos' : 'neg'} ${strong ? 'strong' : ''}`} title={`r = ${c.r} over ${c.n} days`}>{c.r > 0 ? '+' : ''}{c.r}</span>
            <span className="grow">{c.text}<span className="muted xsmall block">{strong ? 'Clear link' : 'Weak link'} · {c.n} days</span></span>
          </div>
        })}</div>
      </section>}

      {data.weather && <section className="card"><div className="card-head"><div className="grow"><h2>Weather</h2><p className="muted small">Your averages by the day's weather ({data.weather.days} days)</p></div></div><div className="card-body flush">
        <table className="table compact"><thead><tr><th>Days like</th><th className="right">Days</th>{data.weather.metrics.map(k => <th key={k} className="right">{metricOf(k)?.name || k}</th>)}</tr></thead>
          <tbody>{[...data.weather.by_rain, ...data.weather.by_temperature, ...data.weather.by_condition].map(r => <tr key={String(r.key)}><td>{String(r.label)}</td><td className="right muted">{String(r.days)}</td>{data.weather!.metrics.map(k => <td key={k} className="right tnum">{r[k] !== undefined ? fmtK(k, Number(r[k])) : <span className="muted">—</span>}</td>)}</tr>)}</tbody></table>
      </div></section>}

      {data.goals.length > 0 && <section className="card"><div className="card-head"><div className="grow"><h2>Goals</h2><p className="muted small">Days each goal was met in this range</p></div><Link to="/goals" className="btn ghost sm">Goals</Link></div>
        <div className="ins-goals">{data.goals.map(g => {
          const m = metricOf(g.metric)
          return <Link key={g.metric} to={`/metrics/${g.metric}`} className="ins-goal" style={{ '--tile': m?.color || 'var(--accent)' } as React.CSSProperties}>
            <span className="ins-goal-name"><span className="tile-ico sm"><Icon name={m?.icon || 'target'} size={13} /></span><span className="ins-ellipsis"><b>{g.name}</b> <span className="muted small">{g.direction === 'at_most' ? '≤' : '≥'} {fmtK(g.metric, g.target)}</span></span></span>
            <span className="ins-goal-bar"><span className="ins-goal-track"><i style={{ width: `${Math.max(g.pct ? 2 : 0, g.pct)}%` }} /></span><span className="tnum small"><b>{g.met}</b><span className="muted">/{g.days}</span></span></span>
            <span className="ins-goal-meta muted small tnum"><span title="Current streak"><Icon name="flame" size={12} />{g.streak}</span><span title="Best streak"><Icon name="trophy" size={12} />{g.best}</span></span>
          </Link>
        })}</div>
      </section>}

      {!data.weather && <p className="muted small ins-note"><Icon name="cloud" size={13} /> Switch weather on under <Link to="/settings/general">Settings → General</Link> to see how rain and temperature change your days.</p>}
    </div>
  )
}
