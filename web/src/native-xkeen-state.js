// Pure projection of native XKeen status facts. Missing or unknown facts must
// never read as a usable installation.
export function nativeXkeenState(facts = {}) {
  const installed = facts?.installation === 'available'
  const attached = facts?.panelIntegration === 'available'
  const missing = facts?.installation === 'missing'
  const attachable = installed && facts?.panelIntegration === 'missing'
  const running = installed && facts?.xrayRunning === true
  const uncertain = !installed && !missing || installed && !running && !attached && !attachable
  const title = running ? 'XKeen is running' : uncertain ? 'Check XKeen status' : missing ? 'Install XKeen first' : attachable ? 'Connect the panel to XKeen' : 'Add your VPN profiles'
  const description = running || installed && attached ? 'XKeen manages the traffic service. The panel manages your profiles and configuration.' : uncertain ? installed ? 'XKeen is installed. Its configuration access needs inspection; refresh status.' : 'The native installation state is unavailable. Inspect XKeen and refresh status.' : !installed ? 'Use the official XKeen installer, then refresh this page.' : 'Connect the panel to the native configuration to manage profiles.'
  return { installed, attached, running, title, description, canManageProfiles: installed && attached }
}
