export function NativeXkeenStatus({ facts, onOpenNodes }) {
  if (!facts) return null
  const installed = facts.installation === 'available'
  const attached = facts.panelIntegration === 'available'
  const title = !installed ? 'Install XKeen first' : !attached ? 'Connect the panel to XKeen' : facts.xrayRunning ? 'XKeen is running' : 'Add your VPN profiles'
  return <section className="panel" aria-label="Native XKeen">
    <div className="workspace-heading"><h2>{title}</h2><span>{installed ? `XKeen ${facts.version} · ${facts.channel}` : 'Installation not detected'}</span></div>
    <p>{!installed ? 'Use the official XKeen installer, then refresh this page.' : !attached ? 'The native installation is available. Complete the panel connection to manage VPN profiles.' : facts.xrayRunning ? 'XKeen manages the traffic service. The panel shows its status and manages your profiles.' : 'Import a subscription or add a VPN key to configure your connections.'}</p>
    {attached && onOpenNodes && <button type="button" onClick={onOpenNodes}>Manage VPN profiles</button>}
  </section>
}

export function NativeXkeenSection({ facts, onRefresh, onOpenSystem }) {
  return <div className="section-stack">
    <NativeXkeenStatus facts={facts} />
    <section className="panel"><div className="workspace-heading"><h2>XKeen and components</h2><button type="button" onClick={onRefresh}>Refresh status</button></div>
      <dl><dt>Core</dt><dd>{facts?.core || 'Not detected'}</dd><dt>Geodata files</dt><dd>{facts?.geodataFiles ?? 'Unknown'}</dd><dt>Native geodata schedule</dt><dd>{facts?.geodataCron === 'available' ? 'Configured in XKeen' : facts?.geodataCron === 'missing' ? 'Not configured' : 'Unknown'}</dd></dl>
      <p>XKeen owns updates for its script, the core and geodata.</p>
    </section>
    <section className="panel"><h2>Panel updates</h2><p>Update XKeen Control from the panel settings.</p><button type="button" onClick={onOpenSystem}>Open panel settings</button></section>
  </div>
}
