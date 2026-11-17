import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import type { ProxyHealthSnapshot, ProxyNodeTestConfig } from '@/types'
import ProxyHealthCell from '../ProxyHealthCell.vue'

enableAutoUnmount(afterEach)
const checkedAt = '2026-11-17T10:00:00+08:00'
const now = Date.parse(checkedAt) + 60_000
const snapshot: ProxyHealthSnapshot & ProxyNodeTestConfig = {
  node_status: 'unhealthy', node_checked_at: checkedAt,
  business_status: 'healthy', business_checked_at: checkedAt,
  preferred_node: 'us2', is_backup: true, route_status: 'healthy', route_checked_at: checkedAt,
  mihomo_primary_node: 'us1', mihomo_group: 'us1-fallback', sampled_pod: 'pod-a'
}
function cell(kind: 'node' | 'business' | 'route', values = snapshot, currentTime = now) {
  return mount(ProxyHealthCell, {
    props: { proxy: values, kind, now: currentTime },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })] }
  })
}
describe('original node and fallback health display', () => {
  it('shows a failed original node together with a healthy backup entry', () => {
    const node = cell('node')
    expect(node.get('[data-health-kind="node"]').text()).toBe('故障')
    expect(node.get('[data-health-kind="node"]').classes()).toContain('badge-danger')
    expect(node.text()).toContain('pod-a')
    expect(cell('route').text()).toContain('us2（备用）')
    expect(cell('business').get('span.badge').text()).toBe('可用')
  })
  it('keeps a controller error separate from original-node and business checks', () => {
    const values = { ...snapshot, node_status: 'healthy' as const, route_status: 'unknown' as const, preferred_node: '', route_error: 'controller unavailable' }
    expect(cell('route', values).get('span.badge').text()).toBe('未知')
    expect(cell('node', values).get('span.badge').text()).toBe('正常')
    expect(cell('business', values).get('span.badge').text()).toBe('可用')
  })
  it('marks manual selection explicitly', () => {
    expect(cell('route', { ...snapshot, route_fixed: true }).text()).toContain('us2（手动指定）')
  })
  it.each([
    ['node', '故障'], ['business', '可用'], ['route', 'us2（备用）']
  ] as const)('keeps %s results fresh for one hour and expires them afterward', (kind, expected) => {
    const expiry = Date.parse(checkedAt) + 60 * 60_000
    expect(cell(kind, snapshot, expiry - 1).get('span.badge').text()).toBe(expected)
    expect(cell(kind, snapshot, expiry).get('span.badge').text()).toBe(expected)
    const stale = cell(kind, snapshot, expiry + 1)
    expect(stale.get('span.badge').text()).toBe('已过期')
    expect(stale.get('span.badge').classes()).not.toContain('badge-success')
    expect(stale.get('div').attributes('title')).toContain('检测结果已超过 1 小时')
  })
  it('does not infer business availability from an all-failed group preferred node', () => {
    const values = { ...snapshot, preferred_node: 'us1', is_backup: false, business_status: 'unhealthy' as const }
    expect(cell('route', values).get('span.badge').text()).toBe('us1')
    expect(cell('business', values).get('span.badge').text()).toBe('不可用')
  })
  it('can display a sampled task route without inventing a fallback group name', () => {
    expect(cell('route', { ...snapshot, mihomo_primary_node: '', mihomo_group: '' }).text()).toContain('us2（备用）')
  })
  it('keeps a configured task without a proxy record or sample as not yet tested', () => {
    expect(cell('route', { mihomo_primary_node: '自定义美国节点' }).get('span.badge').text()).toBe('未检测')
    expect(cell('route', { ...snapshot, route_status: 'not_applicable' }).get('span.badge').text()).toBe('不适用')
  })
  it('shows unknown when no probe ran and not applicable for an ordinary proxy route', () => {
    expect(cell('node', {}).get('span.badge').text()).toBe('未检测')
    expect(cell('node', { node_status: 'unknown', sample_id: 'invalid-config' }).get('span.badge').text()).toBe('未知')
    expect(cell('route', { route_status: 'not_applicable' }).get('span.badge').text()).toBe('不适用')
  })
})
