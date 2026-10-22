import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import AccountErrorDistributionChart from '../AccountErrorDistributionChart.vue'

const messages: Record<string, string> = {
  'usage.accountErrorDistribution': 'Account Error Counts',
  'admin.dashboard.noDataAvailable': 'No data available',
  'usage.errors.categories.upstream': 'Upstream error',
  'usage.errors.categories.rate_limit': 'Rate limited',
  'usage.errors.categories.internal': 'Platform error',
  'usage.errors.categories.other': 'Other',
  'common.total': 'Total',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Bar: {
    name: 'Bar',
    props: ['data', 'options'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

describe('AccountErrorDistributionChart', () => {
  it('groups and stacks each error category by account', () => {
    const wrapper = mount(AccountErrorDistributionChart, {
      props: {
        items: [
          {
            account_id: 2,
            account_name: 'secondary',
            total: 2,
            categories: [{ category: 'upstream', count: 2 }],
          },
          {
            account_id: 1,
            account_name: 'primary',
            total: 9,
            categories: [
              { category: 'upstream', count: 3 },
              { category: 'rate_limit', count: 5 },
              { category: 'internal', count: 1 },
            ],
          },
        ],
      },
      global: {
        stubs: { LoadingSpinner: true },
      },
    })

    const chartData = JSON.parse(wrapper.get('.chart-data').text())
    expect(chartData.labels).toEqual(['primary', 'secondary'])
    expect(chartData.datasets).toEqual([
      expect.objectContaining({ label: 'Upstream error', data: [3, 2], stack: 'errors' }),
      expect.objectContaining({ label: 'Rate limited', data: [5, 0], stack: 'errors' }),
      expect.objectContaining({ label: 'Platform error', data: [1, 0], stack: 'errors' }),
    ])

    const bar = wrapper.findComponent({ name: 'Bar' })
    expect(bar.props('options').scales.x.stacked).toBe(true)
    expect(bar.props('options').scales.y.stacked).toBe(true)
  })

  it('shows the empty state when there are no account errors', () => {
    const wrapper = mount(AccountErrorDistributionChart, {
      props: { items: [] },
      global: {
        stubs: { LoadingSpinner: true },
      },
    })

    expect(wrapper.text()).toContain('No data available')
    expect(wrapper.find('.chart-data').exists()).toBe(false)
  })
})
