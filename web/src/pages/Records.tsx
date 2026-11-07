// ECG recordings and hearing tests from Apple Health: the watch's ECG trace
// with Apple's reading of it, and each hearing test's thresholds per ear.
import { useEffect, useMemo, useState } from 'react'
import { api, V1 } from '../lib/api'
import type { Audiogram, ECG } from '../lib/types'
import { fmtDateTime } from '../lib/format'
import { Empty, Loading, PageHeader } from '../components/ui'
import { useCrumb } from '../components/Layout'

const CLASS: Record<string, string> = {
  sinus_rhythm: 'Sinus rhythm', atrial_fibrillation: 'Atrial fibrillation', inconclusive_low_hr: 'Inconclusive: heart rate under 50',
  inconclusive_high_hr: 'Inconclusive: heart rate over 150', inconclusive_poor_reading: 'Inconclusive: poor recording',
  inconclusive_other: 'Inconclusive', unrecognized: 'Not classified', not_set: 'Not classified',
}

/** The ECG trace as paper: 25 mm/s, 10 mm/mV, a grid every 0.2 s and 0.5 mV. */
function Trace({ ecg }: { ecg: ECG }) {
  const v = ecg.voltages || []
  const hz = ecg.sampling_hz || 1
  const secs = v.length / hz
  const W = Math.max(600, secs * 100), H = 160
  const path = useMemo(() => {
    if (!v.length) return ''
    const step = Math.max(1, Math.floor(v.length / 6000))
    let d = ''
    for (let i = 0; i < v.length; i += step) d += `${i === 0 ? 'M' : 'L'}${((i / hz) * 100).toFixed(1)},${(H / 2 - (v[i] / 1000) * 60).toFixed(1)} `
    return d
  }, [v, hz])
  if (!v.length) return <p className="muted small">The trace was not sent.</p>
  return <div className="scroll-x"><svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} className="ecg-trace" role="img" aria-label="ECG trace">
    {Array.from({ length: Math.ceil(secs * 5) + 1 }, (_, i) => <line key={`x${i}`} x1={i * 20} x2={i * 20} y1={0} y2={H} className={i % 5 ? 'grid' : 'grid major'} />)}
    {Array.from({ length: 9 }, (_, i) => <line key={`y${i}`} x1={0} x2={W} y1={i * 20} y2={i * 20} className={(i - 4) % 5 ? 'grid' : 'grid major'} />)}
    <path d={path} className="line" />
  </svg></div>
}

export default function Records() {
  useCrumb('ECG & hearing')
  const [ecgs, setEcgs] = useState<ECG[] | null>(null)
  const [tests, setTests] = useState<Audiogram[] | null>(null)
  const [open, setOpen] = useState<ECG | null>(null)
  useEffect(() => {
    api.get<ECG[]>(`${V1}/ecgs`).then(setEcgs).catch(() => setEcgs([]))
    api.get<Audiogram[]>(`${V1}/audiograms`).then(setTests).catch(() => setTests([]))
  }, [])
  const show = async (e: ECG) => { if (open?.id === e.id) { setOpen(null); return } try { setOpen(await api.get<ECG>(`${V1}/ecgs/${e.id}`)) } catch { setOpen(null) } }
  if (!ecgs || !tests) return <Loading />
  return (
    <div className="stack loose">
      <PageHeader title="ECG & hearing" sub="Recordings and tests from Apple Health. Only you see them; no linked app does." />
      <section className="card"><div className="card-head"><div className="grow"><h2>ECG recordings</h2><p className="muted small">{ecgs.length ? `${ecgs.length} from the watch` : 'None yet'}</p></div></div>
        {ecgs.length === 0 ? <div className="card-body"><Empty title="No ECG yet">Record one with the ECG app on Apple Watch; the iPhone sends it on its next sync.</Empty></div>
          : <ul className="list">{ecgs.map(e => <li key={e.id}>
            <button type="button" className="list-row" onClick={() => show(e)}><span className="grow"><b>{CLASS[e.classification] || e.classification || 'ECG'}</b>
              <span className="muted small block">{fmtDateTime(e.at)}{e.avg_hr ? ` · ${Math.round(e.avg_hr)} bpm` : ''}{e.symptoms_status === 'present' ? ' · with symptoms' : ''}{e.device ? ` · ${e.device}` : ''}</span></span></button>
            {open?.id === e.id && <div className="card-body"><Trace ecg={open} /></div>}
          </li>)}</ul>}
      </section>
      <section className="card"><div className="card-head"><div className="grow"><h2>Hearing tests</h2><p className="muted small">Threshold per frequency in dB HL; lower hears quieter sounds.</p></div></div>
        {tests.length === 0 ? <div className="card-body"><Empty title="No hearing test yet">Tests from a hearing app or AirPods Pro arrive with the next sync.</Empty></div>
          : tests.map(t => <div key={t.id} className="card-body"><p className="small"><b>{fmtDateTime(t.at)}</b>{t.device ? <span className="muted"> · {t.device}</span> : null}</p>
            <table className="table compact"><thead><tr><th>Frequency</th><th className="right">Left</th><th className="right">Right</th></tr></thead>
              <tbody>{t.points.map(([hz, l, r]) => <tr key={hz ?? 0}><td className="tnum">{hz && hz >= 1000 ? `${hz / 1000} kHz` : `${hz} Hz`}</td><td className="right tnum">{l ?? '—'}</td><td className="right tnum">{r ?? '—'}</td></tr>)}</tbody></table></div>)}
      </section>
    </div>
  )
}
