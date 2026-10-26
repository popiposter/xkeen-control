import test from 'node:test'
import assert from 'node:assert/strict'
import { catchAllRule, membershipKind, trafficInbounds } from '../src/routing-rules.js'

test('new rules cover every user inbound the draft names, never a system one', () => {
  const lan = ['redirect', 'tproxy']
  assert.deepEqual(trafficInbounds([{ inboundTag: ['api'] }, { inboundTag: lan }, { inboundTag: ['socks'] }]), ['redirect', 'tproxy', 'socks'])
  // A system tag in a mixed list is dropped alone; custom tags stay.
  assert.deepEqual(trafficInbounds([{ inboundTag: ['force-proxy-redirect', 'lan-custom'] }, { inboundTag: ['probe'] }]), ['lan-custom'])
  assert.deepEqual(trafficInbounds([{ inboundTag: ['api'] }, { domain: ['x'] }]), ['redirect', 'tproxy', 'socks'])
  assert.deepEqual(trafficInbounds([]), ['redirect', 'tproxy', 'socks'])
  // Like Xray: one string splits at commas, array elements stay verbatim,
  // nothing is trimmed.
  assert.deepEqual(trafficInbounds([{ inboundTag: 'custom-lan' }]), ['custom-lan'])
  assert.deepEqual(trafficInbounds([{ inboundTag: 'api,lan-a,lan-b' }]), ['lan-a', 'lan-b'])
  assert.deepEqual(trafficInbounds([{ inboundTag: ['lan,socks'] }]), ['lan,socks'])
  assert.deepEqual(trafficInbounds([{ inboundTag: 'lan-a, socks' }]), ['lan-a', ' socks'])
})

test('only a rule that admits every connection is a catch-all', () => {
  assert.equal(catchAllRule({ type: 'field', inboundTag: ['redirect'], network: 'tcp,udp', outboundTag: 'direct' }), true)
  assert.equal(catchAllRule({ type: 'field', network: 'udp, tcp', outboundTag: 'direct' }), true)
  assert.equal(catchAllRule({ type: 'field', outboundTag: 'direct' }), true)
  assert.equal(catchAllRule({ type: 'field', network: 'udp', outboundTag: 'block' }), false)
  assert.equal(catchAllRule({ type: 'field', network: 'tcp', outboundTag: 'direct' }), false)
  assert.equal(catchAllRule({ type: 'field', domain: ['x'], outboundTag: 'direct' }), false)
  assert.equal(catchAllRule({ type: 'field', future: 1, outboundTag: 'direct' }), false)
  assert.equal(catchAllRule({ type: 'field', domain: [], outboundTag: 'direct' }), true)
  // An empty network list adds no condition in Xray; arrays name networks too.
  assert.equal(catchAllRule({ type: 'field', inboundTag: ['redirect'], network: [], outboundTag: 'direct' }), true)
  assert.equal(catchAllRule({ type: 'field', network: ['tcp', 'udp'], outboundTag: 'direct' }), true)
  assert.equal(catchAllRule({ type: 'field', network: ['udp'], outboundTag: 'block' }), false)
  // An empty attributes map adds no condition; a non-empty one does.
  assert.equal(catchAllRule({ type: 'field', inboundTag: ['redirect'], attrs: {}, outboundTag: 'direct' }), true)
  assert.equal(catchAllRule({ type: 'field', attrs: { ':method': 'GET' }, outboundTag: 'direct' }), false)
  assert.equal(catchAllRule({ type: 'field', future: {}, outboundTag: 'direct' }), false)
})

test('membership lookups are offered for addresses and host names only', () => {
  assert.equal(membershipKind('192.0.2.5'), 'ip')
  assert.equal(membershipKind('2001:db8::5'), 'ip')
  assert.equal(membershipKind('::ffff:192.0.2.5'), 'ip')
  assert.equal(membershipKind('64:ff9b::192.0.2.5'), 'ip')
  assert.equal(membershipKind('::ffff:192.0.2.500'), '')
  assert.equal(membershipKind('login.live.com'), 'domain')
  assert.equal(membershipKind('login.live.com.'), 'domain')
  assert.equal(membershipKind('microsoft'), '')
  assert.equal(membershipKind('192.0.2.0/24'), '')
  assert.equal(membershipKind('a b.com'), '')
})
