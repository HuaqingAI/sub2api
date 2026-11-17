<template>
  <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <label class="input-label">{{ t('admin.proxyMonitoring.configTitle') }}</label>
    <Select
      :model-value="modelValue.enabled ? 'mihomo' : 'standard'"
      :options="options"
      @update:model-value="setMode"
    />
    <div v-if="modelValue.enabled" class="mt-3 space-y-3">
      <div>
        <label :for="`${id}-node`" class="input-label">{{ t('admin.proxyMonitoring.primaryNode') }}</label>
        <input
          :id="`${id}-node`" :value="modelValue.mihomo_primary_node" class="input" type="text" required maxlength="100"
          :placeholder="t('admin.proxyMonitoring.primaryNodePlaceholder')"
          @input="update('mihomo_primary_node', ($event.target as HTMLInputElement).value)"
        />
      </div>
      <div>
        <label :for="`${id}-port`" class="input-label">{{ t('admin.proxyMonitoring.testPort') }}</label>
        <input
          :id="`${id}-port`" :value="modelValue.node_test_port" class="input" type="number" required min="1" max="65535" step="1"
          :placeholder="t('admin.proxyMonitoring.testPortPlaceholder')"
          @input="updatePort(($event.target as HTMLInputElement).value)"
        />
      </div>
      <div>
        <label :for="`${id}-group`" class="input-label">{{ t('admin.proxyMonitoring.fallbackGroup') }}</label>
        <input
          :id="`${id}-group`" :value="modelValue.mihomo_group" class="input" type="text" required maxlength="100"
          :placeholder="t('admin.proxyMonitoring.fallbackGroupPlaceholder')"
          @input="update('mihomo_group', ($event.target as HTMLInputElement).value)"
        />
      </div>
    </div>
    <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.proxyMonitoring.configHelp') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { createProxyNodeTestForm, type ProxyNodeTestForm } from '@/utils/proxyMonitoring'
const props = defineProps<{ modelValue: ProxyNodeTestForm }>()
const emit = defineEmits<{ 'update:modelValue': [value: ProxyNodeTestForm] }>()
const { t } = useI18n()
const id = useId()
const options = computed(() => [
  { value: 'standard', label: t('admin.proxyMonitoring.normalProxy') },
  { value: 'mihomo', label: t('admin.proxyMonitoring.mihomoProxy') }
])

function setMode(value: string | number | boolean | null) {
  emit('update:modelValue', value === 'mihomo'
    ? { ...props.modelValue, enabled: true }
    : createProxyNodeTestForm())
}

function update<K extends keyof ProxyNodeTestForm>(key: K, value: ProxyNodeTestForm[K]) {
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}

function updatePort(value: string) {
  update('node_test_port', value === '' ? null : Number(value))
}
</script>
