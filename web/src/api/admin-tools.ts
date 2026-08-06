import { apiGet, apiPost } from './client'
import type { User } from '@/identity'

export interface NotificationTestResult {
  event_id: string
  status: 'queued'
}

export function listNotificationTestRecipients(): Promise<User[]> {
  return apiGet<User[]>('/api/admin/tools/notifications/recipients')
}

export function requestNotificationTest(recipientID: string): Promise<NotificationTestResult> {
  return apiPost<NotificationTestResult>('/api/admin/tools/notifications/test', {
    recipient_id: recipientID,
  })
}
