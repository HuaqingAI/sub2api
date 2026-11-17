import type { ProxyNodeTestConfig } from '@/types'

export interface ProxyNodeTestForm {
  enabled: boolean
  node_test_port: number | null
  mihomo_primary_node: string
  mihomo_group: string
}

export function createProxyNodeTestForm(config: ProxyNodeTestConfig = {}): ProxyNodeTestForm {
  return {
    enabled: config.node_test_port != null || Boolean(config.mihomo_primary_node || config.mihomo_group),
    node_test_port: config.node_test_port ?? null,
    mihomo_primary_node: config.mihomo_primary_node || '',
    mihomo_group: config.mihomo_group || ''
  }
}

export function isValidProxyNodeTestForm(config: ProxyNodeTestForm, businessPort: number): boolean {
  if (!config.enabled) return true
  const node = config.mihomo_primary_node.trim()
  const group = config.mihomo_group.trim()
  const encoder = new TextEncoder()
  const port = config.node_test_port
  return node.length > 0 && encoder.encode(node).length <= 100 &&
    group.length > 0 && encoder.encode(group).length <= 100 &&
    port !== null && Number.isInteger(port) && port >= 1 && port <= 65535 && port !== businessPort
}

export function proxyNodeTestConfig(config: ProxyNodeTestForm): Required<ProxyNodeTestConfig> {
  return config.enabled
    ? {
      node_test_port: config.node_test_port,
      mihomo_primary_node: config.mihomo_primary_node.trim(),
      mihomo_group: config.mihomo_group.trim()
    }
    : { node_test_port: null, mihomo_primary_node: '', mihomo_group: '' }
}
