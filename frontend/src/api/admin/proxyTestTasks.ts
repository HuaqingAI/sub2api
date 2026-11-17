import { apiClient } from '../client'
import type { CreateProxyTestTaskRequest, ProxyTestTask } from '@/types'

const path = '/admin/proxy-test-tasks'
// A task may probe multiple proxies and wait for its execution lock.
const requestOptions = { timeout: 330_000 }

export const proxyTestTasksAPI = {
  async list(): Promise<ProxyTestTask[]> {
    const { data } = await apiClient.get<ProxyTestTask[]>(path)
    if (!Array.isArray(data)) throw new Error('Invalid proxy test task list response')
    return data
  },
  async create(input: CreateProxyTestTaskRequest): Promise<ProxyTestTask> {
    const { data } = await apiClient.post<ProxyTestTask>(path, input, requestOptions)
    return data
  },
  async update(id: number, input: Partial<CreateProxyTestTaskRequest>): Promise<ProxyTestTask> {
    const { data } = await apiClient.put<ProxyTestTask>(`${path}/${id}`, input, requestOptions)
    return data
  },
  async delete(id: number): Promise<void> {
    await apiClient.delete(`${path}/${id}`, requestOptions)
  },
  async run(id: number): Promise<ProxyTestTask> {
    const { data } = await apiClient.post<ProxyTestTask>(`${path}/${id}/run`, undefined, requestOptions)
    return data
  },
  async testEmail(id: number): Promise<void> {
    await apiClient.post(`${path}/${id}/test-email`, undefined, requestOptions)
  }
}

export default proxyTestTasksAPI
