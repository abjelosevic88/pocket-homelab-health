// Display helpers: canonical values → the person's unit system.
import type { Metric } from './types'

let IMPERIAL = false
export function setImperial(v: boolean) { IMPERIAL = v }
export function imperial() { return IMPERIAL }

/** Convert a canonical value for display; returns [value, unit]. */
export function display(m: Metric, v: number): [number, string] {
  if (!IMPERIAL || !m.imperial_unit) return [v, m.unit]
  switch (m.unit) {
    case 'kg': return [v / 0.45359237, m.imperial_unit]
    case 'km': return [v / 1.609344, m.imperial_unit]
    case 'm': return [v / 0.9144, m.imperial_unit]
    case 'cm': return [v / 2.54, m.imperial_unit]
    case '°C': return [v * 9 / 5 + 32, m.imperial_unit]
    case 'mL': return [v / 29.5735, m.imperial_unit]
    case 'km/h': return [v / 1.609344, m.imperial_unit]
  }
  return [v, m.unit]
}

/** The unit label the person sees for a metric. */
export function unitOf(m: Metric): string { return IMPERIAL && m.imperial_unit ? m.imperial_unit : m.unit }

/** Convert a typed value in the display unit back to canonical. */
export function toCanonical(m: Metric, v: number): number {
  if (!IMPERIAL || !m.imperial_unit) return v
  switch (m.unit) {
    case 'kg': return v * 0.45359237
    case 'km': return v * 1.609344
    case 'm': return v * 0.9144
    case 'cm': return v * 2.54
    case '°C': return (v - 32) * 5 / 9
    case 'mL': return v * 29.5735
    case 'km/h': return v * 1.609344
  }
  return v
}

export function fmtNum(v: number | null | undefined, decimals = 0): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—'
  return v.toLocaleString(undefined, { minimumFractionDigits: 0, maximumFractionDigits: decimals })
}

/** "7h 12m" for minutes. */
export function fmtMinutes(min: number | null | undefined): string {
  if (min === null || min === undefined || Number.isNaN(min)) return '—'
  const m = Math.round(min)
  const h = Math.floor(m / 60)
  return h ? `${h}h ${String(m % 60).padStart(2, '0')}m` : `${m} min`
}

/** A metric value for display with its unit. */
export function fmtMetric(m: Metric, v: number | null | undefined, withUnit = true): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—'
  if (m.key.startsWith('sleep_') || (m.unit === 'min' && m.agg === 'sum' && v >= 90)) return fmtMinutes(v)
  const [d, u] = display(m, v)
  const dec = m.decimals ?? 0
  const s = d.toLocaleString(undefined, { minimumFractionDigits: 0, maximumFractionDigits: dec })
  if (!withUnit) return s
  if (u === 'count' || u === '1–5' || u === 'drinks') return s
  return `${s} ${u}`
}

export function fmtPair(m: Metric, v: number, v2?: number): string {
  if (m.pair && v2 !== undefined && v2 !== null) return `${Math.round(v)}/${Math.round(v2)} ${m.unit}`
  return fmtMetric(m, v)
}

export function fmtDistance(meters: number): string {
  if (!meters) return '—'
  if (IMPERIAL) return `${(meters / 1609.344).toFixed(meters < 16093 ? 2 : 1)} mi`
  return meters < 1000 ? `${Math.round(meters)} m` : `${(meters / 1000).toFixed(meters < 10000 ? 2 : 1)} km`
}

export function fmtPace(secPerKm: number): string {
  if (!secPerKm || !Number.isFinite(secPerKm)) return '—'
  let s = secPerKm, u = '/km'
  if (IMPERIAL) { s = secPerKm * 1.609344; u = '/mi' }
  const m = Math.floor(s / 60), r = Math.round(s % 60)
  return `${m}:${String(r).padStart(2, '0')}${u}`
}

export function fmtDurationS(s: number): string {
  if (!s) return '—'
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60)
  return h ? `${h}h ${String(m).padStart(2, '0')}m` : `${m} min`
}
