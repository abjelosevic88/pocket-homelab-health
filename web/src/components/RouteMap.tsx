import { useEffect, useRef } from 'react'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'

// slow → fast, the way the Fitness app colours a route
const PACE = ['#ef4444', '#f97316', '#f59e0b', '#eab308', '#a3e635', '#4ade80', '#22c55e']

function paceBuckets(speeds: number[]): number[] {
  const sorted = speeds.filter(v => v > 0).sort((a, b) => a - b)
  if (sorted.length < 2) return speeds.map(() => PACE.length - 1)
  const lo = sorted[Math.floor(sorted.length * 0.1)], hi = sorted[Math.floor(sorted.length * 0.9)]
  return speeds.map(v => hi > lo ? Math.max(0, Math.min(PACE.length - 1, Math.round(((v - lo) / (hi - lo)) * (PACE.length - 1)))) : PACE.length - 1)
}

/** A workout's recorded route on an OpenStreetMap layer with start and end dots; with `speeds`
 *  (one per point) the line is coloured by pace. */
export function RouteMap({ points, speeds, className = '' }: { points: [number, number][]; speeds?: number[]; className?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const map = useRef<L.Map | null>(null)
  const layer = useRef<L.LayerGroup | null>(null)
  useEffect(() => {
    if (!ref.current || map.current) return
    const m = L.map(ref.current, { zoomControl: true, attributionControl: true, scrollWheelZoom: false })
    m.setView([45, 15], 5)
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', { attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors', maxZoom: 19 }).addTo(m)
    layer.current = L.layerGroup().addTo(m)
    map.current = m
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(() => m.invalidateSize()) : null
    ro?.observe(ref.current)
    return () => { ro?.disconnect(); m.remove(); map.current = null }
  }, [])
  useEffect(() => {
    const m = map.current, g = layer.current
    if (!m || !g) return
    g.clearLayers()
    if (points.length < 2) return
    const line = L.polyline(points, { color: '#e11d48', weight: 4, opacity: speeds ? 0 : .9, lineJoin: 'round' }).addTo(g)
    if (speeds && speeds.length === points.length) {
      // runs of the same colour, each one line, joined at their ends
      const b = paceBuckets(speeds)
      let from = 0
      for (let i = 1; i <= points.length; i++) {
        if (i === points.length || b[i] !== b[from]) {
          L.polyline(points.slice(from, Math.min(i + 1, points.length)), { color: PACE[b[from]], weight: 5, opacity: .95, lineJoin: 'round', lineCap: 'round' }).addTo(g)
          from = i
        }
      }
    }
    L.circleMarker(points[0], { radius: 6, color: '#fff', weight: 2, fillColor: '#16a34a', fillOpacity: 1 }).addTo(g).bindTooltip('Start')
    L.circleMarker(points[points.length - 1], { radius: 6, color: '#fff', weight: 2, fillColor: '#111', fillOpacity: 1 }).addTo(g).bindTooltip('End')
    m.fitBounds(line.getBounds(), { padding: [24, 24] })
  }, [points, speeds])
  return <div ref={ref} className={`route-map ${className}`} />
}
