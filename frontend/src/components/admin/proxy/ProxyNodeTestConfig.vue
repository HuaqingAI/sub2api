<template>
  <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <label class="input-label">{{ t('admin.proxyMonitoring.configTitle') }}</label>
    <Select :model-value="modelValue" :options="options" @update:model-value="value => emit('update:modelValue', String(value ?? ''))" />
    <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.proxyMonitoring.configHelp') }}</p>
    <p v-if="modelValue" class="mt-1 font-mono text-xs text-gray-500">{{ configDescription }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { mihomoNodeConfig } from '@/utils/proxyMonitoring'
const props = defineProps<{ modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n()
const options = computed(() => [
  { value: '', label: t('admin.proxyMonitoring.normalProxy') },
  ...[1, 2, 3, 4, 5].map(n => ({ value: `us${n}`, label: `us${n}` }))
])
const configDescription = computed(() => {
  const config = mihomoNodeConfig(props.modelValue)
  return `${config.mihomo_group} · socks5h://127.0.0.1:${config.node_test_port}`
})
</script>
