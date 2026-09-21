import { client, type ApiEnvelope } from './client'
import type { FeedingRestriction, PageQuery, PageResult } from '@/types/models'
import type { RestrictionStatus } from '@/types/enums'

export const restrictionApi = {
  async list(params: PageQuery = {}) {
    const response = await client.get<ApiEnvelope<PageResult<FeedingRestriction>>>('/restrictions', { params })
    return response.data.data
  },
  async handle(id: number, note: string) {
    const response = await client.patch<ApiEnvelope<FeedingRestriction>>(`/restrictions/${id}/handle`, { note })
    return response.data.data
  },
  async release(id: number, reviewNote: string) {
    const response = await client.patch<ApiEnvelope<FeedingRestriction>>(`/restrictions/${id}/release`, { reviewNote })
    return response.data.data
  },
}

export type { RestrictionStatus }
