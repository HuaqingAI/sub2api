<template>
  <div class="flex min-w-28 flex-col items-start gap-1" :title="details">
    <span class="badge whitespace-nowrap" :class="badgeClass" :data-health-kind="kind">
      {{ label }}
    </span>
    <span v-if="checkedAt" class="whitespace-nowrap text-xs text-gray-500 dark:text-gray-400">
      {{ formatDateTime(checkedAt) }}
    </span>
    <span v-if="kind === 'node' && proxy.sampled_pod" class="max-w-40 truncate text-xs text-gray-400">
      {{ proxy.sampled_pod }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProxyHealthSnapshot, ProxyNodeTestConfig } from '@/types'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  proxy: ProxyHealthSnapshot & ProxyNodeTestConfig & { latency_message?: string }
  kind: 'node' | 'business' | 'route'
  now: number
}>()
const { t } = useI18n()
const checkedAt = computed(() => props.kind === 'node' ? props.proxy.node_checked_at
  : props.kind === 'business' ? props.proxy.business_checked_at : props.proxy.route_checked_at)
const expired = computed(() => Boolean(checkedAt.value) &&
  (!Number.isFinite(Date.parse(checkedAt.value!)) || props.now - Date.parse(checkedAt.value!) > 15 * 60_000))
const notApplicable = computed(() => props.kind === 'route' &&
  (props.proxy.route_status === 'not_applicable' || !props.proxy.mihomo_group))
const status = computed(() => {
  if (expired.value || !checkedAt.value) return 'unknown'
  if (props.kind === 'node') return props.proxy.node_status || 'unknown'
  if (props.kind === 'business') return props.proxy.business_status || 'unknown'
  return props.proxy.route_status || 'unknown'
})
const label = computed(() => {
  if (notApplicable.value) return t('admin.proxyMonitoring.notApplicable')
  if (expired.value) return t('admin.proxyMonitoring.expired')
  if (!checkedAt.value) return props.proxy.sample_id
    ? t('admin.proxyMonitoring.unknown') : t('admin.proxyMonitoring.notTested')
  if (props.kind === 'route') {
    if (status.value === 'switching') return t('admin.proxyMonitoring.switching')
    if (status.value !== 'healthy' || !props.proxy.preferred_node) return t('admin.proxyMonitoring.unknown')
    const node = props.proxy.preferred_node
    if (props.proxy.route_fixed) return t('admin.proxyMonitoring.manualNode', { node })
    return props.proxy.is_backup ? t('admin.proxyMonitoring.backupNode', { node }) : node
  }
  if (status.value === 'healthy') return props.kind === 'node'
    ? t('admin.proxyMonitoring.healthy') : t('admin.proxyMonitoring.available')
  if (status.value === 'unhealthy') return props.kind === 'node'
    ? t('admin.proxyMonitoring.unhealthy') : t('admin.proxyMonitoring.unavailable')
  return t('admin.proxyMonitoring.unknown')
})
const badgeClass = computed(() => {
  if (notApplicable.value || status.value === 'unknown') return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-400'
  if (status.value === 'unhealthy') return 'badge-danger'
  if (status.value === 'switching' || (props.kind === 'route' && (props.proxy.is_backup || props.proxy.route_fixed))) return 'badge-warning'
  return 'badge-success'
})
const details = computed(() => [
  props.kind === 'route' ? t('admin.proxyMonitoring.routeHelp') : props.kind === 'node' ? t('admin.proxyMonitoring.nodeHelp') : t('admin.proxyMonitoring.businessHelp'),
  props.kind === 'node' ? props.proxy.latency_message : '',
  props.kind === 'route' ? props.proxy.route_error : '',
  props.proxy.sampled_pod ? t('admin.proxyMonitoring.sampledPod', { pod: props.proxy.sampled_pod }) : '',
  checkedAt.value ? formatDateTime(checkedAt.value) : '',
  expired.value ? t('admin.proxyMonitoring.expiredHelp') : ''
].filter(Boolean).join('\n'))
</script>
