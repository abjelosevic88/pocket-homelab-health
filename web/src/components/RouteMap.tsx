import { useEffect, useRef } from 'react'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'

/** A workout's recorded route on an OpenStreetMap layer with start and end dots. */
export function RouteMap({ points, className = '' }: { points: [number, number][]; className?: string }) {
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
    const line = L.polyline(points, { color: '#e11d48', weight: 4, opacity: .9, lineJoin: 'round' }).addTo(g)
    L.circleMarker(points[0], { radius: 6, color: '#fff', weight: 2, fillColor: '#16a34a', fillOpacity: 1 }).addTo(g).bindTooltip('Start')
    L.circleMarker(points[points.length - 1], { radius: 6, color: '#fff', weight: 2, fillColor: '#111', fillOpacity: 1 }).addTo(g).bindTooltip('End')
    m.fitBounds(line.getBounds(), { padding: [24, 24] })
  }, [points])
  return <div ref={ref} className={`route-map ${className}`} />
}
