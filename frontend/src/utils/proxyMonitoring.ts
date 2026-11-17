import type { ProxyNodeTestConfig } from '@/types'

const rawPorts: Record<string, number> = { us1: 17903, us2: 17905, us3: 17906, us4: 17907, us5: 17908 }

export function mihomoNodeConfig(node: string): ProxyNodeTestConfig {
  const port = rawPorts[node]
  return port
    ? { node_test_port: port, mihomo_primary_node: node, mihomo_group: `${node}-fallback` }
    : { node_test_port: null, mihomo_primary_node: '', mihomo_group: '' }
}
