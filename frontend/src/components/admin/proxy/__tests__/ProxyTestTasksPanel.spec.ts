import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import type { ProxyTestTask } from '@/types'
import ProxyTestTasksPanel from '../ProxyTestTasksPanel.vue'

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
const api = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), update: vi.fn(), run: vi.fn(), delete: vi.fn(), testEmail: vi.fn(), proxies: vi.fn(), showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { proxyTestTasks: api, proxies: { list: api.proxies } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: api.showError, showSuccess: api.showSuccess }) }))
enableAutoUnmount(afterEach)
const task: ProxyTestTask = {
  id: 11, name: 'US check', proxy_ids: [2], cron_expression: '*/5 * * * *', timezone: 'Asia/Shanghai', enabled: true,
  recipients: ['admin@example.com'], failure_threshold: 2, timeout_seconds: 10, smtp_configured: true,
  targets: [{ proxy_id: 2, proxy_name: 'us1', consecutive_failures: 2, last_node_status: 'unhealthy', notified_recipients: ['admin@example.com'] }]
}
async function panel() {
  const wrapper = shallowMount(ProxyTestTasksPanel, {
    props: { show: false, selectedIds: [2], now: Date.now() },
    global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}
function button(wrapper: Awaited<ReturnType<typeof panel>>, key: string) {
  return wrapper.findAll('button').find(item => item.text() === key)!
}
describe('proxy scheduled task management', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.list.mockResolvedValue([])
    api.proxies.mockResolvedValue({ items: [{ id: 1, name: 'other', host: '127.0.0.1', port: 7905 }, { id: 2, name: 'us1', host: '127.0.0.1', port: 7903 }], pages: 1 })
  })
  it('creates a task for selected proxies with explicit schedule and recipients', async () => {
    api.create.mockResolvedValue(task)
    const wrapper = await panel()
    await button(wrapper, 'admin.proxyTestTasks.create').trigger('click')
    const inputs = wrapper.find('form').findAll('input')
    await inputs[0].setValue('US check')
    await inputs[3].setValue('admin@example.com, second@example.com')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'US check', proxy_ids: [2], cron_expression: '*/5 * * * *', timezone: 'Asia/Shanghai', recipients: ['admin@example.com', 'second@example.com'], failure_threshold: 1, timeout_seconds: 10 }))
  })
  it('runs diagnostics without editing task failure counters or sending an alert', async () => {
    api.list.mockResolvedValue([task])
    api.run.mockResolvedValue({ ...task, last_run_status: 'manual_success' })
    const wrapper = await panel()
    await button(wrapper, 'admin.proxyTestTasks.run').trigger('click')
    await flushPromises()
    expect(api.run).toHaveBeenCalledWith(task.id)
    expect(api.update).not.toHaveBeenCalled()
    expect(api.testEmail).not.toHaveBeenCalled()
    expect(wrapper.emitted('refreshed')).toHaveLength(1)
  })
  it('reports SMTP unavailable and requires a separate explicit test email action', async () => {
    api.list.mockResolvedValue([{ ...task, smtp_configured: false }])
    const wrapper = await panel()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.proxyTestTasks.smtpUnavailable')
    expect(button(wrapper, 'admin.proxyTestTasks.testEmail').attributes('disabled')).toBeDefined()
    expect(api.testEmail).not.toHaveBeenCalled()
  })
  it('does not accept a task without any selected proxy', async () => {
    const wrapper = await panel()
    await wrapper.setProps({ selectedIds: [] })
    await button(wrapper, 'admin.proxyTestTasks.create').trigger('click')
    const inputs = wrapper.find('form').findAll('input')
    await inputs[0].setValue('US check')
    await inputs[3].setValue('admin@example.com')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(api.create).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.proxyTestTasks.invalidForm')
  })
})
