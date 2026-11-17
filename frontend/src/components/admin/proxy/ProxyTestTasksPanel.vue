<template>
  <BaseDialog :show="show" :title="t('admin.proxyTestTasks.title')" width="wide" @close="emit('close')">
    <div class="space-y-4">
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.proxyTestTasks.description') }}</p>
      <p v-if="tasks.some(task => task.smtp_configured === false)" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
        {{ t('admin.proxyTestTasks.smtpUnavailable') }}
      </p>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary" :disabled="loading || busy" @click="load()">{{ t('common.refresh') }}</button>
        <button type="button" class="btn btn-primary" :disabled="loading || busy" @click="startCreate">{{ t('admin.proxyTestTasks.create') }}</button>
      </div>

      <form v-if="showForm" class="space-y-3 rounded-xl border border-primary-200 p-4 dark:border-primary-800" @submit.prevent="save">
        <h3 class="font-medium">{{ editingId ? t('admin.proxyTestTasks.edit') : t('admin.proxyTestTasks.create') }}</h3>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="block"><span class="input-label">{{ t('admin.proxyTestTasks.name') }}</span><input v-model="form.name" required maxlength="100" class="input" /></label>
          <label class="block"><span class="input-label">{{ t('admin.proxyTestTasks.cron') }}</span><input v-model="form.cron_expression" required placeholder="*/5 * * * *" class="input font-mono" /></label>
          <label class="block"><span class="input-label">{{ t('admin.proxyTestTasks.timezone') }}</span><input v-model="form.timezone" required placeholder="Asia/Shanghai" class="input" /></label>
          <label class="block"><span class="input-label">{{ t('admin.proxyTestTasks.recipients') }}</span><input v-model="recipientText" required class="input" :placeholder="t('admin.proxyTestTasks.recipientsPlaceholder')" /></label>
          <label class="block"><span class="input-label">{{ t('admin.proxyTestTasks.threshold') }}</span><input v-model.number="form.failure_threshold" type="number" min="1" max="20" required class="input" /></label>
          <label class="block"><span class="input-label">{{ t('admin.proxyTestTasks.timeout') }}</span><input v-model.number="form.timeout_seconds" type="number" min="3" max="30" required class="input" /></label>
        </div>
        <p class="text-xs text-gray-500">{{ t('admin.proxyTestTasks.cronHelp') }}</p>
        <fieldset>
          <legend class="input-label">{{ t('admin.proxyTestTasks.proxies') }}</legend>
          <div class="grid max-h-44 gap-2 overflow-y-auto rounded-lg border border-gray-200 p-3 dark:border-dark-600 sm:grid-cols-2">
            <label v-for="proxy in availableProxies" :key="proxy.id" class="flex items-center gap-2 text-sm">
              <input v-model="form.proxy_ids" type="checkbox" :value="proxy.id" class="h-4 w-4 rounded" />
              <span class="truncate" :title="`${proxy.name} (${proxy.host}:${proxy.port})`">{{ proxy.name }} <span class="text-xs text-gray-500">{{ proxy.host }}:{{ proxy.port }}</span></span>
            </label>
            <p v-if="!availableProxies.length" class="text-sm text-gray-500">{{ t('admin.proxyTestTasks.noProxies') }}</p>
          </div>
        </fieldset>
        <label class="flex items-center gap-2 text-sm"><input v-model="form.enabled" type="checkbox" class="h-4 w-4 rounded" />{{ t('admin.proxyTestTasks.enabled') }}</label>
        <p v-if="formError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ formError }}</p>
        <div class="flex justify-end gap-2">
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="showForm = false">{{ t('common.cancel') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="busy">{{ t('common.save') }}</button>
        </div>
      </form>

      <p v-if="loading" class="py-6 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
      <p v-else-if="!tasks.length" class="rounded-lg border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-600">{{ t('admin.proxyTestTasks.empty') }}</p>
      <article v-for="task in tasks" :key="task.id" class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600" :data-task-id="task.id">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 class="font-medium text-gray-900 dark:text-white">{{ task.name }}</h3>
            <p class="mt-1 font-mono text-xs text-gray-500">{{ task.cron_expression }} · {{ task.timezone }}</p>
            <p class="mt-1 text-xs text-gray-500">{{ task.recipients.join(', ') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <label class="flex items-center gap-1 text-sm"><input type="checkbox" :checked="task.enabled" :disabled="busy" @change="toggle(task)" />{{ t('admin.proxyTestTasks.enabled') }}</label>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="startEdit(task)">{{ t('common.edit') }}</button>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="run(task)">{{ t('admin.proxyTestTasks.run') }}</button>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || task.smtp_configured === false" @click="sendTestEmail(task)">{{ t('admin.proxyTestTasks.testEmail') }}</button>
            <button type="button" class="btn btn-danger btn-sm" :disabled="busy" @click="deletingTask = task">{{ t('common.delete') }}</button>
          </div>
        </div>
        <div class="flex flex-wrap gap-x-5 gap-y-1 text-xs text-gray-500">
          <span>{{ t('admin.proxyTestTasks.lastRun') }}: {{ task.last_run_at ? formatDateTime(task.last_run_at) : '-' }}</span>
          <span>{{ t('admin.proxyTestTasks.nextRun') }}: {{ task.enabled && task.next_run_at ? formatDateTime(task.next_run_at) : '-' }}</span>
          <span v-if="task.last_run_status">{{ t('admin.proxyTestTasks.runStatus') }}: {{ runStatusLabel(task.last_run_status) }}</span>
        </div>
        <p v-if="task.last_error" class="break-words text-sm text-red-600 dark:text-red-400">{{ task.last_error }}</p>
        <details v-if="task.targets?.length" :open="expandedTaskId === task.id" @toggle="onExpand(task.id, $event)">
          <summary class="cursor-pointer text-sm text-primary-600 dark:text-primary-400">{{ t('admin.proxyTestTasks.latestResults') }}</summary>
          <div class="mt-3 overflow-x-auto">
            <table class="w-full text-left text-xs">
              <thead class="text-gray-500"><tr>
                <th class="p-2">{{ t('admin.proxyTestTasks.proxies') }}</th>
                <th class="p-2">{{ t('admin.proxyMonitoring.nodeStatus') }}</th>
                <th class="p-2">{{ t('admin.proxyMonitoring.currentExit') }}</th>
                <th class="p-2">{{ t('admin.proxyMonitoring.businessStatus') }}</th>
                <th class="p-2">{{ t('admin.proxyTestTasks.failures') }}</th>
              </tr></thead>
              <tbody><tr v-for="target in task.targets" :key="target.proxy_id" class="border-t border-gray-100 dark:border-dark-700">
                <td class="p-2">{{ target.proxy_name || target.proxy_id }}<p v-if="target.last_error" class="mt-1 max-w-64 break-words text-red-600">{{ target.last_error }}</p></td>
                <td class="p-2"><ProxyHealthCell :proxy="targetSnapshot(target)" kind="node" :now="now" /></td>
                <td class="p-2"><ProxyHealthCell :proxy="targetSnapshot(target)" kind="route" :now="now" /></td>
                <td class="p-2"><ProxyHealthCell :proxy="targetSnapshot(target)" kind="business" :now="now" /></td>
                <td class="p-2">{{ target.consecutive_failures }}</td>
              </tr></tbody>
            </table>
          </div>
        </details>
      </article>
      <p class="text-xs text-gray-500">{{ t('admin.proxyTestTasks.manualHelp') }}</p>
    </div>
    <template #footer><button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button></template>
  </BaseDialog>
  <ConfirmDialog :show="Boolean(deletingTask)" :title="t('admin.proxyTestTasks.delete')" :message="t('admin.proxyTestTasks.deleteConfirm', { name: deletingTask?.name || '' })" :danger="true" @confirm="remove" @cancel="deletingTask = null" />
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { CreateProxyTestTaskRequest, Proxy, ProxyTestTask, ProxyTestTaskTarget } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ProxyHealthCell from './ProxyHealthCell.vue'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ show: boolean; selectedIds: number[]; now: number }>()
const emit = defineEmits<{ close: []; refreshed: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const tasks = ref<ProxyTestTask[]>([])
const availableProxies = ref<Proxy[]>([])
const loading = ref(false)
const busy = ref(false)
const showForm = ref(false)
const editingId = ref<number | null>(null)
const expandedTaskId = ref<number | null>(null)
const deletingTask = ref<ProxyTestTask | null>(null)
const recipientText = ref('')
const formError = ref('')
const defaults = (): CreateProxyTestTaskRequest => ({ name: '', proxy_ids: [], cron_expression: '*/5 * * * *', timezone: 'Asia/Shanghai', enabled: true, recipients: [], failure_threshold: 1, timeout_seconds: 10 })
const form = reactive(defaults())
let loadVersion = 0

async function allProxies(): Promise<Proxy[]> {
  const result: Proxy[] = []
  for (let page = 1; ; page++) {
    const response = await adminAPI.proxies.list(page, 200)
    result.push(...response.items)
    if (page >= (response.pages || 1)) return result
  }
}
async function load() {
  const version = ++loadVersion
  loading.value = true
  try {
    const [newTasks, proxies] = await Promise.all([adminAPI.proxyTestTasks.list(), allProxies()])
    if (version !== loadVersion || !props.show) return
    tasks.value = newTasks
    availableProxies.value = proxies
  } catch (error) {
    if (version === loadVersion && props.show) showError(error)
  } finally {
    if (version === loadVersion) loading.value = false
  }
}
function startCreate() {
  editingId.value = null
  Object.assign(form, defaults(), { proxy_ids: [...props.selectedIds] })
  recipientText.value = ''
  formError.value = ''
  showForm.value = true
}
function startEdit(task: ProxyTestTask) {
  editingId.value = task.id
  Object.assign(form, { name: task.name, proxy_ids: [...task.proxy_ids], cron_expression: task.cron_expression, timezone: task.timezone, enabled: task.enabled, recipients: [...task.recipients], failure_threshold: task.failure_threshold, timeout_seconds: task.timeout_seconds })
  recipientText.value = task.recipients.join(', ')
  formError.value = ''
  showForm.value = true
}
function showError(error: unknown) {
  const apiError = error as { response?: { data?: { detail?: string } }; message?: string }
  appStore.showError(apiError.response?.data?.detail || apiError.message || t('admin.proxyTestTasks.failed'))
}
function replace(task: ProxyTestTask) {
  const index = tasks.value.findIndex(item => item.id === task.id)
  if (index < 0) tasks.value.unshift(task)
  else tasks.value[index] = task
}
async function save() {
  formError.value = ''
  const recipients = [...new Set(recipientText.value.split(/[\s,;]+/).filter(Boolean))]
  if (!form.name.trim() || !form.proxy_ids.length || !recipients.length || recipients.some(email => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) || form.cron_expression.trim().split(/\s+/).length !== 5 || !Number.isInteger(form.failure_threshold) || form.failure_threshold < 1 || form.failure_threshold > 20 || !Number.isInteger(form.timeout_seconds) || form.timeout_seconds < 3 || form.timeout_seconds > 30) {
    formError.value = t('admin.proxyTestTasks.invalidForm')
    return
  }
  const input = { ...form, name: form.name.trim(), cron_expression: form.cron_expression.trim(), timezone: form.timezone.trim(), proxy_ids: [...form.proxy_ids], recipients }
  busy.value = true
  try {
    replace(editingId.value ? await adminAPI.proxyTestTasks.update(editingId.value, input) : await adminAPI.proxyTestTasks.create(input))
    showForm.value = false
    appStore.showSuccess(t('admin.proxyTestTasks.saved'))
  } catch (error) { showError(error) } finally { busy.value = false }
}
async function toggle(task: ProxyTestTask) {
  busy.value = true
  try { replace(await adminAPI.proxyTestTasks.update(task.id, { enabled: !task.enabled })) }
  catch (error) { showError(error) } finally { busy.value = false }
}
async function run(task: ProxyTestTask) {
  busy.value = true
  try {
    replace(await adminAPI.proxyTestTasks.run(task.id))
    expandedTaskId.value = task.id
    emit('refreshed')
    appStore.showSuccess(t('admin.proxyTestTasks.ran'))
  } catch (error) { showError(error) } finally { busy.value = false }
}
async function sendTestEmail(task: ProxyTestTask) {
  busy.value = true
  try { await adminAPI.proxyTestTasks.testEmail(task.id); appStore.showSuccess(t('admin.proxyTestTasks.emailSent')) }
  catch (error) { showError(error) } finally { busy.value = false }
}
async function remove() {
  if (!deletingTask.value || busy.value) return
  busy.value = true
  try {
    await adminAPI.proxyTestTasks.delete(deletingTask.value.id)
    tasks.value = tasks.value.filter(task => task.id !== deletingTask.value!.id)
    deletingTask.value = null
  } catch (error) { showError(error) } finally { busy.value = false }
}
function targetSnapshot(target: ProxyTestTaskTarget) {
  const proxy = availableProxies.value.find(item => item.id === target.proxy_id)
  return {
    mihomo_primary_node: proxy?.mihomo_primary_node || target.mihomo_primary_node,
    mihomo_group: proxy?.mihomo_group || (target.mihomo_primary_node ? `${target.mihomo_primary_node}-fallback` : ''),
    node_status: target.last_node_status,
    node_checked_at: target.last_checked_at,
    latency_message: target.last_error,
    ...target.last_result
  }
}
function onExpand(id: number, event: Event) {
  if ((event.target as HTMLDetailsElement).open) expandedTaskId.value = id
  else if (expandedTaskId.value === id) expandedTaskId.value = null
}
function runStatusLabel(status: string) {
  if (status.startsWith('manual')) return t('admin.proxyTestTasks.manualResult')
  if (status === 'running') return t('admin.proxyTestTasks.running')
  if (status === 'success' || status === 'completed') return t('admin.proxyTestTasks.completed')
  if (status === 'failed' || status === 'error') return t('admin.proxyTestTasks.runFailed')
  return status
}
watch(() => props.show, show => {
  if (show) { showForm.value = false; deletingTask.value = null; void load() }
  else { loadVersion++; loading.value = false }
}, { immediate: true })
</script>
