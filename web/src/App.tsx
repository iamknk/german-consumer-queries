import { useState } from 'react'

function App() {
  const [query, setQuery] = useState('Familienfreundliches Hotel mit Frühstück und WLAN unter 120€ in Berlin vom 12.–14.10.2025, 2 Erwachsene, 1 Kind.')
  const [result, setResult] = useState<any>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: any) => {
    e.preventDefault()
    setError(null); setLoading(true); setResult(null)
    try {
      const res = await fetch('http://localhost:8080/v1/parse', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query_de: query }),
      })
      const text = await res.text()
      if (!res.ok) throw new Error(text || res.statusText)
      setResult(JSON.parse(text))
    } catch (err:any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{maxWidth: 800, margin: '0 auto', padding: 24}}>
      <h1>Hotel Query Parser (GPT‑5)</h1>
      <form onSubmit={handleSubmit} style={{display:'flex', gap:8, marginBottom:16}}>
        <input
          style={{flex:1, padding:8, border:'1px solid #ccc'}}
          placeholder="Beschreibe, wonach du suchst…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <button style={{padding:'8px 16px'}}>{loading ? '...' : 'Parse'}</button>
      </form>
      {error && <div style={{color:'red', marginBottom:8}}>Error: {error}</div>}
      {result && (
        <pre style={{background:'#f7f7f7', padding:12, borderRadius:6, overflow:'auto'}}>{JSON.stringify(result, null, 2)}</pre>
      )}
    </div>
  )
}

export default App
