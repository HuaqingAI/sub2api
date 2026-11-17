import { afterEach, describe, expect, it } from 'vitest'
import { defineComponent, ref } from 'vue'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import Select from '@/components/common/Select.vue'
import ProxyNodeTestConfig from '../ProxyNodeTestConfig.vue'
import { createProxyNodeTestForm } from '@/utils/proxyMonitoring'
import type { ProxyNodeTestConfig as NodeConfig } from '@/types'

enableAutoUnmount(afterEach)

function form(initial: NodeConfig = {}) {
  const harness = defineComponent({
    components: { ProxyNodeTestConfig },
    setup: () => ({ model: ref(createProxyNodeTestForm(initial)) }),
    template: '<ProxyNodeTestConfig v-model="model" />'
  })
  return mount(harness, {
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })],
      stubs: { Select: true }
    }
  })
}

describe('Mihomo node check configuration fields', () => {
  it('lets a new node use independent custom port and group values', async () => {
    const wrapper = form()
    expect(wrapper.findAll('input')).toHaveLength(0)
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'mihomo')
    await wrapper.vm.$nextTick()
    await wrapper.get('input[id$="-node"]').setValue('us6')
    await wrapper.get('input[id$="-port"]').setValue('22006')
    await wrapper.get('input[id$="-group"]').setValue('My US pool')
    await wrapper.get('input[id$="-node"]').setValue('us7')
    expect(wrapper.getComponent(ProxyNodeTestConfig).props('modelValue')).toEqual({
      enabled: true, node_test_port: 22006, mihomo_primary_node: 'us7', mihomo_group: 'My US pool'
    })
    expect(wrapper.get('input[id$="-port"]').element).toHaveProperty('value', '22006')
    expect(wrapper.get('input[id$="-group"]').element).toHaveProperty('value', 'My US pool')
  })

  it('loads stored values and clears the configuration when switched to a standard proxy', async () => {
    const wrapper = form({ node_test_port: 25000, mihomo_primary_node: 'original node', mihomo_group: 'Custom pool' })
    expect(wrapper.get('input[id$="-node"]').element).toHaveProperty('value', 'original node')
    expect(wrapper.get('input[id$="-port"]').element).toHaveProperty('value', '25000')
    expect(wrapper.get('input[id$="-group"]').element).toHaveProperty('value', 'Custom pool')
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'standard')
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('input')).toHaveLength(0)
    expect(wrapper.getComponent(ProxyNodeTestConfig).props('modelValue')).toEqual({
      enabled: false, node_test_port: null, mihomo_primary_node: '', mihomo_group: ''
    })
  })
})
