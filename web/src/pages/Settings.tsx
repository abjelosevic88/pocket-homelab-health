import { AskButton } from '../components/AskButton'
import React, { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, ConnMode, HabitRule, Import, Settings as SettingsT, Sources, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, ago, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, CopyField, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useDebounce, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'
import { AIStatusLine, RuleModal, useAIStatus } from '../components/forms'
import { useCatalog } from '../components/Layout'

type TabId = 'general' | 'sources' | 'notifications' | 'apps' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'settings', sub: 'Units, your body, the Today tiles, weather and the app title.' },
  { id: 'sources', label: 'Phone & imports', icon: 'smartphone', sub: 'Where the data comes from: your iPhone (the Pocket Homelab app or Health Auto Export), an Apple Health export, CSV, any app.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'Met goals, medication reminders, the weekly report and a nudge when the phone stops syncing.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Pocket Habits (rules that tick habits) and Pocket Movement (the route of a workout) — through Pocket Core or directly.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'Who writes the recap and answers questions: Pocket AI through Core, or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for the phone, Homepage widgets, Siri Shortcuts and scripts.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Download the database, export or import your data.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'general' && <General />}
            {tab === 'sources' && <SourcesTab />}
            {tab === 'notifications' && <Notifications />}
            {tab === 'apps' && <Integrations />}
            {tab === 'ai' && <AITab />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  const catalog = useCatalog()
  const { toast } = useToast()
  const [place, setPlace] = useState('')
  const dq = useDebounce(place, 300)
  const [places, setPlaces] = useState<{ label: string; lat: number; lon: number; name: string }[]>([])
  useEffect(() => { if (dq.length >= 2) api.get<typeof places>(`${V1}/weather/geocode?q=${encodeURIComponent(dq)}`).then(setPlaces).catch(() => {}); else setPlaces([]) }, [dq])
  if (!f) return <Loading />
  const imp = f.units === 'imperial'
  const refresh = async () => { try { const r = await api.post<{ days: number }>(`${V1}/weather/refresh`); toast(`${r.days} days fetched`) } catch (e) { toast((e as Error).message, 'err') } }
  const metricsAll = catalog?.metrics || []
  const toggleTile = (k: string) => setF({ ...f, home_metrics: f.home_metrics.includes(k) ? f.home_metrics.filter(x => x !== k) : [...f.home_metrics, k] })
  return (
    <div className="stack loose">
      <Section title="Units & you" sub="Yours alone — everyone on the box picks their own. Height feeds the BMI; birth date and sex are kept for the recap only.">
        <SettingRow title="Units"><Segmented value={f.units} onChange={v => setF({ ...f, units: v as 'metric' | 'imperial' })} options={[{ value: 'metric', label: 'Metric' }, { value: 'imperial', label: 'Imperial' }]} size="sm" /></SettingRow>
        <SettingRow title="Height" help={imp ? 'inches' : 'cm'}><input type="number" step="0.1" value={imp ? (f.height_cm ? +(f.height_cm / 2.54).toFixed(1) : '') : (f.height_cm || '')} onChange={e => setF({ ...f, height_cm: imp ? Number(e.target.value) * 2.54 : Number(e.target.value) })} className="set-time" /></SettingRow>
        <SettingRow title="Birth date"><input type="date" value={f.birth_date} onChange={e => setF({ ...f, birth_date: e.target.value })} /></SettingRow>
        <SettingRow title="Sex"><Select value={f.sex} onChange={v => setF({ ...f, sex: v })} options={[{ value: 'female', label: 'Female' }, { value: 'male', label: 'Male' }, { value: 'other', label: 'Other' }]} placeholder="Not set" clearLabel="Not set" /></SettingRow>
      </Section>
      <Section title="Today tiles" icon="grid" sub="Which numbers Today shows first, in this order. Tap to add or remove.">
        <div className="row wrap" style={{ gap: 6 }}>
          {f.home_metrics.map(k => { const m = metricsAll.find(x => x.key === k); return <button key={k} type="button" className="chip active" onClick={() => toggleTile(k)}>{m && <Icon name={m.icon} size={13} />}{m?.name || k}<Icon name="x" size={12} /></button> })}
        </div>
        <div className="row wrap mt" style={{ gap: 6 }}>
          {metricsAll.filter(m => !f.home_metrics.includes(m.key)).map(m => <button key={m.key} type="button" className="chip" onClick={() => toggleTile(m.key)}><Icon name={m.icon} size={13} />{m.name}</button>)}
        </div>
      </Section>
      <Section title="Weather" icon="sun" sub="Open-Meteo (free, no key) fetches the day's weather at your home location so Insights can split your days by rain and temperature." actions={<Switch checked={f.weather_enabled} onChange={v => setF({ ...f, weather_enabled: v })} label="Weather" />}>
        {f.weather_enabled && <>
          <SettingRow title="Home location" help={f.weather_place ? `${f.weather_place} (${f.weather_lat.toFixed(3)}, ${f.weather_lon.toFixed(3)})` : 'Search a town'} stack>
            <div style={{ position: 'relative' }}>
              <input value={place} onChange={e => setPlace(e.target.value)} placeholder="Sarajevo" />
              {places.length > 0 && <div className="sel-pop" style={{ position: 'absolute', top: 40, zIndex: 70 }}><div className="sel-list">{places.map(p => <div key={p.label} className="sel-opt" onMouseDown={e => { e.preventDefault(); setF({ ...f, weather_lat: p.lat, weather_lon: p.lon, weather_place: p.name }); setPlace(''); setPlaces([]) }}><span className="sel-lbl">{p.label}</span></div>)}</div></div>}
            </div>
          </SettingRow>
          <SettingRow title="Last fetched"><span className="row"><span className="muted small">{f.weather_last ? ago(f.weather_last) : 'never'}</span><button type="button" className="btn sm" onClick={refresh} disabled={!f.weather_lat}>Fetch now</button></span></SettingRow>
        </>}
      </Section>
      <Section title="Date & time">
        <SettingRow title="Date format"><Select value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
      </Section>
      <Section title="This box" sub={isAdmin ? 'Applies to everyone on this box.' : 'Set by an admin for everyone.'}>
        <SettingRow title="App title" help="Shown in the sidebar and the browser tab."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} disabled={!isAdmin} /></SettingRow>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function SourcesTab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<Sources>(`${V1}/sources`), [])
  const [token, setToken] = useState('')
  const [job, setJob] = useState<Import | null>(null)
  const [since, setSince] = useState('')
  const [csvMetric, setCsvMetric] = useState('weight')
  const fileRef = useRef<HTMLInputElement>(null)
  const catalog = useCatalog()
  const base = settings?.base_url || window.location.origin
  useEffect(() => {
    if (!job || job.status !== 'running') return
    const t = setInterval(() => api.get<Import>(`${V1}/imports/${job.id}`).then(j => { setJob(j); if (j.status !== 'running') { reload(); window.dispatchEvent(new CustomEvent('ph:changed')) } }).catch(() => {}), 2000)
    return () => clearInterval(t)
  }, [job, reload])
  const [appToken, setAppToken] = useState('')
  const mintToken = async () => { try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: 'Health Auto Export' }); setToken(r.token) } catch (e) { toast((e as Error).message, 'err') } }
  const mintAppToken = async () => { try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: 'Pocket Homelab' }); setAppToken(r.token) } catch (e) { toast((e as Error).message, 'err') } }
  const uploadApple = async (file: File) => {
    const fd = new FormData(); fd.append('file', file); if (since) fd.append('since', since)
    try {
      const res = await fetch(`${V1}/import/apple`, { method: 'POST', body: fd, credentials: 'same-origin' })
      const j = await res.json()
      if (!res.ok) throw new Error(j.error || res.statusText)
      setJob(j)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const uploadCSV = async (file: File) => {
    const fd = new FormData(); fd.append('file', file); fd.append('metric', csvMetric)
    try { const res = await fetch(`${V1}/import/csv`, { method: 'POST', body: fd, credentials: 'same-origin' }); const j = await res.json(); if (!res.ok) throw new Error(j.error || res.statusText); toast(`${j.samples} rows imported${j.skipped ? `, ${j.skipped} skipped` : ''}`); reload(); window.dispatchEvent(new CustomEvent('ph:changed')) } catch (e) { toast((e as Error).message, 'err') }
  }
  const [delSrc, setDelSrc] = useState('')
  const dropSource = async () => { try { const r = await api.del<{ deleted: number }>(`${V1}/sources/${delSrc}`); toast(`${r.deleted} samples removed`); setDelSrc(''); reload(); window.dispatchEvent(new CustomEvent('ph:changed')) } catch (e) { toast((e as Error).message, 'err') } }
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="iPhone: Pocket Homelab" icon="smartphone" sub="The Pocket Homelab app reads Apple Health on the phone and sends it here: steps, energy and distance per hour (Apple's own count, so phone and watch are not added twice), heart rate per hour with its range, every weight, blood pressure and other reading, workouts and nights with their stages. It sends what is new each time you open it, and in the background when Apple Health has something new.">
        <ol className="steps">
          <li>Paired with Pocket Core? Health is already there under <b>Settings → Services</b>; skip to step 3.</li>
          <li>Otherwise create a token: <button type="button" className="btn sm" onClick={mintAppToken}>Create token</button>{appToken && <div className="set-fresh mt"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — shown once.</b></div><pre className="mono">{appToken}</pre></div>} and in the app open <b>Settings → Services → Health</b>: address <CopyField value={base} /> and the token.</li>
          <li>Open the <b>Health</b> tab in the app, tap <b>Connect Apple Health</b>, allow what you want to share, choose how far back the first sync goes.</li>
        </ol>
      </Section>
      <Section title="iPhone: Health Auto Export" icon="smartphone" sub={<>The <a href="https://apps.apple.com/app/health-auto-export-json-csv/id1115567069" target="_blank" rel="noreferrer">Health Auto Export</a> app sends Apple Health to this server on a schedule. Every metric it offers lands here, aggregated as it sends them (an hour or a day per point), plus workouts and sleep stages.</>}>
        <ol className="steps">
          <li>Create a token for the phone: <button type="button" className="btn sm" onClick={mintToken}>Create token</button>{token && <div className="set-fresh mt"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — shown once.</b></div><pre className="mono">{token}</pre></div>}</li>
          <li>In the app: <b>Automations → + → REST API</b>. URL: <CopyField value={`${base}/api/v1/ingest/hae`} />
            Headers: <code>Authorization: Bearer &lt;token&gt;</code>. Format JSON, period “Since last sync”, aggregate <b>days</b> (or hours), select every metric and workouts, enable.</li>
          <li>Tap “Run” once to backfill (set the period to a year for the first run); then let it run in the background.</li>
        </ol>
        <p className="muted small">Any other app that can POST JSON works too, with the Pocket Health format at <code>POST /api/v1/ingest</code> — see <code>docs/api.md</code>.</p>
      </Section>
      <Section title="Apple Health export" icon="apple" sub="Health app → your picture → Export All Health Data → share the zip here. It can be several gigabytes and takes a few minutes; fine-grained records are folded into hourly points per device, one device per day counts for summed metrics.">
        <div className="row wrap">
          <label className="btn primary"><Icon name="upload" size={15} />Upload export.zip<input ref={fileRef} type="file" accept=".zip,.xml,application/zip,text/xml" hidden onChange={e => e.target.files?.[0] && uploadApple(e.target.files[0])} /></label>
          <span className="row small muted">only from <input type="date" value={since} onChange={e => setSince(e.target.value)} /></span>
        </div>
        {job && <div className={`callout ${job.status === 'error' ? 'danger' : job.status === 'done' ? 'ok' : 'info'} mt`}><Icon name={job.status === 'running' ? 'clock' : job.status === 'done' ? 'check-circle' : 'alert'} size={15} /><div>{job.status === 'running' ? `Importing… ${job.progress}` : job.status === 'done' ? `Done: ${job.samples} samples, ${job.workouts} workouts, ${job.sleep} nights${job.skipped ? `, ${job.skipped} skipped` : ''}. ${job.progress}` : `Failed: ${job.error}`}</div></div>}
        {(data?.imports.length || 0) > 0 && <table className="table compact mt"><thead><tr><th>When</th><th>File</th><th>Result</th></tr></thead><tbody>{data!.imports.slice(0, 5).map(i => <tr key={i.id}><td>{fmtDateTime(i.started_at)}</td><td className="muted">{i.file}</td><td>{i.status === 'done' ? `${i.samples} samples · ${i.workouts} workouts · ${i.sleep} nights` : i.status === 'error' ? <span className="bad">{i.error}</span> : i.progress}</td></tr>)}</tbody></table>}
      </Section>
      <Section title="CSV" icon="file" sub="A file with date and value columns (and optionally unit, note) — smart-scale exports, spreadsheets. Or a metric column per row.">
        <div className="row wrap">
          <Select value={csvMetric} onChange={setCsvMetric} options={(catalog?.metrics || []).map(m => ({ value: m.key, label: m.name, hint: m.unit }))} />
          <label className="btn"><Icon name="upload" size={15} />Import CSV…<input type="file" accept=".csv,text/csv" hidden onChange={e => e.target.files?.[0] && uploadCSV(e.target.files[0])} /></label>
        </div>
      </Section>
      <Section title="Sources seen" icon="database" sub="Where your data came from and when each last wrote. Removing a source deletes everything it sent (undo an import)." >
        {data && Object.keys(data.last_sync).length === 0 && <p className="muted small">Nothing yet.</p>}
        {data && Object.entries(data.last_sync).map(([src, at]) => <SettingRow key={src} title={src === 'hae' ? 'Health Auto Export' : src === 'apple' ? 'Apple Health export' : src === 'pocket-homelab' ? 'Pocket Homelab app' : src}><span className="row"><span className="muted small">last {ago(at)}</span><button type="button" className="btn ghost sm danger-text" onClick={() => setDelSrc(src)}>Remove</button></span></SettingRow>)}
        {data && data.devices.length > 1 && <SettingRow title="Preferred device" help="When two devices count the same day (watch and phone both count steps), this one wins; otherwise the larger total."><Select value={f.prefer_device} onChange={v => save({ prefer_device: v })} options={data.devices.map(d => ({ value: d, label: d }))} placeholder="Largest total" clearLabel="Largest total" /></SettingRow>}
        <div className="row mt"><button type="button" className="btn sm" onClick={async () => { await api.post(`${V1}/rollup`); toast('Days recomputed'); window.dispatchEvent(new CustomEvent('ph:changed')) }} disabled={busy}>Recompute every day</button></div>
      </Section>
      {delSrc && <Confirm title={`Remove everything from ${delSrc}?`} message="Samples, workouts and nights that source sent are deleted; the phone may send them again." confirmLabel="Remove" onCancel={() => setDelSrc('')} onConfirm={dropSource} />}
    </div>
  )
}

function Notifications() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const weekly = async () => { try { const r = await api.post<{ ok: boolean; sent?: boolean; error?: string; report?: string }>(`${V1}/notify/weekly`); if (r.ok) toast(r.sent ? 'Weekly report sent' : 'Report ready (no push URL): ' + (r.report || '').split('\n')[0]); else toast(r.error || 'failed', 'err') } catch (e) { toast((e as Error).message, 'err') } }
  const DAYS = [[1, 'Monday'], [2, 'Tuesday'], [3, 'Wednesday'], [4, 'Thursday'], [5, 'Friday'], [6, 'Saturday'], [7, 'Sunday']]
  return (
    <div className="stack loose">
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/health" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
        <SettingRow title="Goals" help="One push the moment a daily goal is reached."><Switch checked={f.notify_goals} onChange={v => setF({ ...f, notify_goals: v })} label="Goal reached" /></SettingRow>
        <SettingRow title="Medications" help="A reminder when a dose is due and not yet logged (once per dose)."><Switch checked={f.notify_meds} onChange={v => setF({ ...f, notify_meds: v })} label="Medication reminders" /></SettingRow>
        <SettingRow title="Weekly report" help="Averages vs the week before, workouts, goals."><Switch checked={f.notify_weekly} onChange={v => setF({ ...f, notify_weekly: v })} label="Weekly report" /></SettingRow>
        <SettingRow title="Phone stopped syncing" help="When nothing has arrived for 36 hours."><Switch checked={f.notify_inactive} onChange={v => setF({ ...f, notify_inactive: v })} label="Sync nudge" /></SettingRow>
        <SettingRow title="Daily pushes at" help="Hour of the day, server time."><input type="number" min={0} max={23} value={f.notify_hour} onChange={e => setF({ ...f, notify_hour: Number(e.target.value) })} className="set-time" /></SettingRow>
        <SettingRow title="Weekly report on"><Select value={String(f.weekly_day)} onChange={v => setF({ ...f, weekly_day: Number(v) })} options={DAYS.map(([d, l]) => ({ value: String(d), label: String(l) }))} /></SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        <button type="button" className="btn" onClick={weekly}><Icon name="chart" size={15} />Send the weekly report now</button>
        {status('notify')}
      </div>
    </div>
  )
}

function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { toast } = useToast()
  const { data: rules, reload } = useAsync(() => api.get<HabitRule[]>(`${V1}/rules`), [])
  const [edit, setEdit] = useState<HabitRule | null | 'new'>(null)
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const eff = (m: ConnMode): ConnMode => !coreOn && m === 'core' ? 'off' : m
  const toggleRule = async (r: HabitRule) => { try { await api.put(`${V1}/rules/${r.id}`, { ...r, enabled: !r.enabled }); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const delRule = async (r: HabitRule) => { try { await api.del(`${V1}/rules/${r.id}`); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const markNow = async () => { try { const r = await api.post<{ marked: number }>(`${V1}/habits/mark-now`); toast(`${r.marked} habit${r.marked === 1 ? '' : 's'} ticked`) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These connections are yours. Through <b>Pocket Core</b> Health acts as you in your own Pocket apps with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter.{!coreOn && <CoreHint />}</div></div>
      <Section title="Pocket Habits — tick habits for you" icon="habit" actions={connBadge(eff(f.habits_mode), f.habits_via)}
        sub="A recorded workout or a met daily goal ticks the habit you choose (“Gym”, “Move”), so the streak keeps itself.">
        <Connection app="Pocket Habits" mode={f.habits_mode} via={f.habits_via} detail={f.habits_detail} core={coreOn} hasToken={!!f.habits_token}
          onMode={m => save({ habits_mode: m })} onDisconnect={() => save({ habits_url: '', habits_token: '' })}
          test={<><button type="button" className="btn sm" onClick={() => run('habits', f.habits_url, f.habits_token)}>Test connection</button>{status('habits')}</>}
          direct={<div className="two-col tight">
            <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={f.habits_url} onChange={e => setF({ ...f, habits_url: e.target.value })} placeholder="http://host.docker.internal:8169" /></Field>
            <Field label="API token" help="Pocket Habits → Settings → API tokens"><input value={f.habits_token} onChange={e => setF({ ...f, habits_token: e.target.value })} autoComplete="off" /></Field>
          </div>} />
        {eff(f.habits_mode) !== 'off' && <div className="mt stack">
          <div className="row"><h4 className="grow">Rules</h4><button type="button" className="btn sm" onClick={() => setEdit('new')}><Icon name="plus" size={14} />Add a rule</button></div>
          {rules && rules.length === 0 && <p className="muted small">No rules yet: nothing is ticked automatically.</p>}
          {rules?.map(r => <div key={r.id} className={`rule ${r.enabled ? '' : 'off'}`}><Switch checked={r.enabled} onChange={() => toggleRule(r)} label="Enabled" size="sm" /><span className="grow">{r.trigger === 'workout' ? (r.match === 'any' ? 'Any workout' : `A ${r.match.replace('_', ' ')} workout`) : `${r.match.replace('_', ' ')} goal met`} → <b>{r.habit_name || `habit #${r.habit_id}`}</b></span><button type="button" className="btn ghost sm" onClick={() => setEdit(r)}>Edit</button><button type="button" className="btn ghost sm danger-text" onClick={() => delRule(r)}>Remove</button></div>)}
          {rules && rules.length > 0 && <div className="row"><button type="button" className="btn sm" onClick={markNow}>Tick pending workouts now</button><span className="muted small">Runs every 15 minutes on its own.</span></div>}
        </div>}
      </Section>
      <Section title="Pocket Movement — the route of a workout" icon="route" actions={connBadge(eff(f.movement_mode), f.movement_via)}
        sub="Every workout page shows the positions your phone recorded between its start and end as a route on a map, with the recorded distance (one tap copies it onto a workout the watch did not measure).">
        <Connection app="Pocket Movement" mode={f.movement_mode} via={f.movement_via} detail={f.movement_detail} core={coreOn} hasToken={!!f.movement_token}
          onMode={m => save({ movement_mode: m })} onDisconnect={() => save({ movement_url: '', movement_token: '' })}
          test={<><button type="button" className="btn sm" onClick={() => run('movement', f.movement_url, f.movement_token)}>Test connection</button>{status('movement')}</>}
          direct={<div className="two-col tight">
            <Field label="URL" help="Pocket Movement, or a Dawarich instance (same points API)."><input value={f.movement_url} onChange={e => setF({ ...f, movement_url: e.target.value })} placeholder="http://host.docker.internal:8197" /></Field>
            <Field label="API token" help="Pocket Movement → Settings → API tokens (a Dawarich API key works too)"><input value={f.movement_token} onChange={e => setF({ ...f, movement_token: e.target.value })} autoComplete="off" /></Field>
          </div>} />
      </Section>
      <Section title="Other Pocket apps" icon="layers" sub="Health offers what it knows to the rest of the box through Pocket Core: Pocket Day reads your day (steps, sleep, workouts, weight…), Pocket Calendar can show workouts and nights, Pocket Kitchen could log water and calories here. Medications and symptoms never leave this app.">
        <dl className="set-dl">
          <div><dt>health:read</dt><dd className="muted">Daily numbers, workouts, nights, goals.</dd></div>
          <div><dt>health:write</dt><dd className="muted">Record measurements and workouts; only what that app wrote itself can be changed by it.</dd></div>
        </dl>
      </Section>
      {busy && <Loading />}
      {edit && <RuleModal rule={edit === 'new' ? undefined : edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reload() }} />}
    </div>
  )
}

function AITab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const ai = useAIStatus()
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="Writes the recap on Insights and answers questions, from a compact summary of your own numbers (averages, workouts, sleep, patterns, goals, logged symptoms). It never diagnoses, and nothing is sent unless you press the button.">
        <AIStatusLine st={ai} />
      </Section>
      <Section title="Provider" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Who writes" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(saved === 'direct')
  const mode: ConnMode = !core && saved === 'core' ? 'off' : saved
  const ok = via !== '' && mode !== 'off'
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: Health does not talk to ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !core },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own data, goals, medications and connections. Admins manage accounts; they do not see anyone else’s health data.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with all their health data." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/dashboard</code>) or receive the phone's pushes. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Health Auto Export)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>POST /ingest/hae</code></dt><dd className="muted">A Health Auto Export body (metrics, workouts, sleep).</dd></div>
          <div><dt><code>POST /ingest</code></dt><dd className="muted"><code>{'{"samples":[{"metric":"weight","at":"…","value":80.4,"unit":"kg"}],"workouts":[…],"sleep":[…]}'}</code> — any app, Shortcuts, a scale script.</dd></div>
          <div><dt><code>POST /samples</code></dt><dd className="muted"><code>{'{"metric":"water","value":250,"unit":"mL"}'}</code> — one reading now (a Siri Shortcut “log a glass of water”).</dd></div>
          <div><dt><code>GET /dashboard</code></dt><dd className="muted">Steps, sleep, resting HR, weight, goals met — for Homepage.</dd></div>
          <div><dt><code>GET /day/YYYY-MM-DD</code></dt><dd className="muted">Everything about one day — what Pocket Day reads.</dd></div>
          <div><dt><code>GET /overlay?from=&to=</code></dt><dd className="muted">Workouts and nights as calendar items — for Pocket Calendar.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const importJSON = async (file: File) => {
    setBusy(true)
    try { const r = await api.post<Record<string, number>>(`${V1}/import`, JSON.parse(await file.text())); toast('Imported: ' + (Object.entries(r).map(([k, v]) => `${v} ${k}`).join(', ') || 'nothing new')); window.dispatchEvent(new CustomEvent('ph:changed')) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-health.db</code> in the data directory.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
      <Section title="Export & import" icon="archive" sub="Everything of yours as one JSON file (samples, workouts, nights, goals, medications with their log, symptoms, rules), or the daily numbers as a CSV. Importing merges; what is already there is updated, not doubled.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a><a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} />Daily CSV</a><label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Import JSON…'}<input type="file" accept="application/json,.json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label></div>
      </Section>
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const tiles: [string, number | undefined, string][] = [['Samples', counts.samples, 'activity'], ['Days', counts.daily, 'calendar'], ['Workouts', counts.workouts, 'run'], ['Nights', counts.sleep_sessions, 'moon']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Health</h3>
          <p className="muted small">Your health data at home: steps, workouts, sleep, heart, weight, vitals and nutrition from the phone, goals with streaks, medications with reminders, symptoms, insights with the weather, and a weekly recap. One binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-health" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Habits, Day, Calendar, Kitchen and more. Not medical advice.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
