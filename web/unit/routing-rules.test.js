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
})

test('membership lookups are offered for addresses and host names only', () => {
  assert.equal(membershipKind('192.0.2.5'), 'ip')
  assert.equal(membershipKind('2001:db8::5'), 'ip')
  assert.equal(membershipKind('login.live.com'), 'domain')
  assert.equal(membershipKind('login.live.com.'), 'domain')
  assert.equal(membershipKind('microsoft'), '')
  assert.equal(membershipKind('192.0.2.0/24'), '')
  assert.equal(membershipKind('a b.com'), '')
})
