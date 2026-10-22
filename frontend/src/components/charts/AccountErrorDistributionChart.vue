<template>
  <div class="card p-4">
    <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">
      {{ t('usage.accountErrorDistribution') }}
    </h3>

    <div v-if="loading" class="flex h-72 items-center justify-center">
      <LoadingSpinner />
    </div>
    <div v-else-if="chartData" class="overflow-x-auto">
      <div class="h-72" :style="{ minWidth: `${chartMinWidth}px` }">
        <Bar :data="chartData" :options="chartOptions" />
      </div>
    </div>
    <div v-else class="flex h-72 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Bar } from 'vue-chartjs'
import { BarElement, CategoryScale, Chart as ChartJS, Legend, LinearScale, Tooltip } from 'chart.js'
import type { TooltipItem } from 'chart.js'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { AccountErrorDistributionItem } from '@/api/admin/ops'

ChartJS.register(BarElement, CategoryScale, LinearScale, Tooltip, Legend)

const props = withDefaults(defineProps<{
  items: AccountErrorDistributionItem[]
  loading?: boolean
}>(), {
  items: () => [],
  loading: false,
})

const { t } = useI18n()

const categoryOrder = [
  'upstream',
  'rate_limit',
  'internal',
  'auth',
  'quota',
  'invalid_request',
  'service_unavailable',
  'cyber',
  'other',
]

const categoryColors: Record<string, string> = {
  upstream: '#ef4444',
  rate_limit: '#f59e0b',
  internal: '#8b5cf6',
  auth: '#3b82f6',
  quota: '#ec4899',
  invalid_request: '#f97316',
  service_unavailable: '#06b6d4',
  cyber: '#14b8a6',
  other: '#9ca3af',
}

interface AccountSeries {
  id: number
  label: string
  counts: Record<string, number>
  total: number
}

const accounts = computed<AccountSeries[]>(() => {
  const byAccount = new Map<number, AccountSeries>()

  for (const item of props.items) {
    const id = Number(item.account_id)
    if (!Number.isFinite(id)) continue

    const account = byAccount.get(id) ?? {
      id,
      label: item.account_name || `#${id}`,
      counts: {},
      total: 0,
    }
    for (const categoryItem of item.categories || []) {
      const count = Number(categoryItem.count)
      if (!Number.isFinite(count) || count <= 0) continue

      const category = categoryOrder.includes(categoryItem.category) ? categoryItem.category : 'other'
      account.counts[category] = (account.counts[category] || 0) + count
      account.total += count
    }
    if (account.total > 0) byAccount.set(id, account)
  }

  return [...byAccount.values()].sort((a, b) => b.total - a.total || a.label.localeCompare(b.label))
})

const categories = computed(() =>
  categoryOrder.filter((category) => accounts.value.some((account) => (account.counts[category] || 0) > 0))
)

const chartMinWidth = computed(() => Math.max(640, accounts.value.length * 96))

const chartData = computed(() => {
  if (!accounts.value.length || !categories.value.length) return null

  return {
    labels: accounts.value.map((account) => account.label),
    datasets: categories.value.map((category) => ({
      label: t(`usage.errors.categories.${category}`),
      data: accounts.value.map((account) => account.counts[category] || 0),
      backgroundColor: categoryColors[category],
      borderWidth: 0,
      borderRadius: 2,
      borderSkipped: false,
      maxBarThickness: 56,
      stack: 'errors',
    })),
  }
})

const isDarkMode = computed(() => document.documentElement.classList.contains('dark'))

const chartColors = computed(() => ({
  text: isDarkMode.value ? '#e5e7eb' : '#4b5563',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb',
}))

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index' as const,
  },
  plugins: {
    legend: {
      position: 'top' as const,
      labels: {
        color: chartColors.value.text,
        usePointStyle: true,
        pointStyle: 'circle',
        padding: 15,
        font: { size: 11 },
      },
    },
    tooltip: {
      callbacks: {
        footer: (items: TooltipItem<'bar'>[]) =>
          `${t('common.total')}: ${items.reduce((total, item) => total + (item.parsed.y ?? 0), 0).toLocaleString()}`,
      },
    },
  },
  scales: {
    x: {
      stacked: true,
      grid: { display: false },
      ticks: {
        color: chartColors.value.text,
        font: { size: 10 },
        maxRotation: 45,
        minRotation: 0,
      },
    },
    y: {
      stacked: true,
      beginAtZero: true,
      grid: { color: chartColors.value.grid },
      ticks: {
        color: chartColors.value.text,
        font: { size: 10 },
        precision: 0,
      },
    },
  },
}))
</script>
