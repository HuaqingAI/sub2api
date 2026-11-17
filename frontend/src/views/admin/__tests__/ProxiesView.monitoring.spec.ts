import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import ProxiesView from '../ProxiesView.vue'
import ProxyHealthCell from '@/components/admin/proxy/ProxyHealthCell.vue'
import ProxyNodeTestConfig from '@/components/admin/proxy/ProxyNodeTestConfig.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn(), getAllWithCount: vi.fn(), testProxy: vi.fn(), checkProxyQuality: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
const base = { id: 9, name: 'us1', protocol: 'socks5', host: '127.0.0.1', port: 7903, username: '', status: 'active', mihomo_primary_node: 'us1', mihomo_group: 'us1-fallback', node_test_port: 17903, updated_at: '2026-11-17T10:00:00Z' }
function mountView() {
  return shallowMount(ProxiesView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="table" /></div>' },
    DataTable: { inheritAttrs: false, emits: ['sort'], props: ['data'], template: '<div v-for="row in data" :key="row.id"><slot name="cell-node_status" :row="row" /><slot name="cell-preferred_node" :row="row" /><slot name="cell-business_status" :row="row" /><slot name="cell-actions" :row="row" /></div>' },
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }
  } } })
}
function button(wrapper: ReturnType<typeof mountView>, key: string) { return wrapper.findAll('button').find(item => item.text() === key)! }
beforeEach(() => {
  vi.resetAllMocks()
  api.list.mockResolvedValue({ items: [{ ...base }], total: 1, pages: 1 })
  api.getAllWithCount.mockResolvedValue([])
  api.update.mockResolvedValue({})
})
describe('proxy monitoring integration', () => {
  it('keeps original failure and healthy fallback from one manual result together', async () => {
    const result = { success: false, message: 'us1 timed out', node_status: 'unhealthy', business_status: 'healthy', preferred_node: 'us2', is_backup: true, route_status: 'healthy', node_checked_at: new Date().toISOString(), business_checked_at: new Date().toISOString(), route_checked_at: new Date().toISOString(), sampled_pod: 'pod-a' }
    api.testProxy.mockResolvedValue(result)
    const wrapper = mountView(); await flushPromises()
    await button(wrapper, 'admin.proxies.testConnection').trigger('click'); await flushPromises()
    const cells = wrapper.findAllComponents(ProxyHealthCell)
    expect(cells).toHaveLength(3)
    for (const cell of cells) expect(cell.props('proxy')).toMatchObject({ node_status: 'unhealthy', business_status: 'healthy', preferred_node: 'us2', sampled_pod: 'pod-a' })
  })
  it('saves the matching raw port and group and clears all metadata together', async () => {
    const wrapper = mountView(); await flushPromises()
    await button(wrapper, 'common.edit').trigger('click')
    const selector = wrapper.findComponent(ProxyNodeTestConfig)
    selector.vm.$emit('update:modelValue', 'us3'); await flushPromises()
    await wrapper.get('#edit-proxy-form').trigger('submit'); await flushPromises()
    expect(api.update).toHaveBeenLastCalledWith(9, expect.objectContaining({ node_test_port: 17906, mihomo_primary_node: 'us3', mihomo_group: 'us3-fallback' }))
    await button(wrapper, 'common.edit').trigger('click')
    wrapper.findComponent(ProxyNodeTestConfig).vm.$emit('update:modelValue', ''); await flushPromises()
    await wrapper.get('#edit-proxy-form').trigger('submit'); await flushPromises()
    expect(api.update).toHaveBeenLastCalledWith(9, expect.objectContaining({ node_test_port: null, mihomo_primary_node: '', mihomo_group: '' }))
  })
  it('quality checks do not replace the three-column connectivity snapshot', async () => {
    const checkedAt = new Date().toISOString()
    api.list.mockResolvedValue({ items: [{ ...base, node_status: 'unhealthy', node_checked_at: checkedAt, business_status: 'healthy', preferred_node: 'us2', sampled_pod: 'pod-a' }], total: 1, pages: 1 })
    api.checkProxyQuality.mockResolvedValue({ summary: 'quality', score: 100, grade: 'A', checked_at: Date.now() / 1000, passed_count: 1, warn_count: 0, failed_count: 0, challenge_count: 0, items: [{ target: 'base_connectivity', status: 'pass' }] })
    const wrapper = mountView(); await flushPromises()
    const qualityButton = button(wrapper, 'admin.proxies.qualityCheck')
    await qualityButton.trigger('click'); await flushPromises()
    expect(wrapper.findComponent(ProxyHealthCell).props('proxy')).toMatchObject({ node_status: 'unhealthy', business_status: 'healthy', preferred_node: 'us2', sampled_pod: 'pod-a', node_checked_at: checkedAt })
  })
})
