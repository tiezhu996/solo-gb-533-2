import { inject, Injectable } from '@angular/core';
import { ApiClient } from './api-client';
import { SaveZoneRevisionDraft, ZoneRevision } from '../types/zone-revision';

@Injectable({ providedIn: 'root' })
export class ZoneRevisionApi {
  private readonly api = inject(ApiClient);
  list(zoneId: number) { return this.api.get<ZoneRevision[]>(`/zones/${zoneId}/revisions`); }
  openDraft(zoneId: number) { return this.api.get<ZoneRevision>(`/zones/${zoneId}/revisions/draft`); }
  get(zoneId: number, revisionId: number) { return this.api.get<ZoneRevision>(`/zones/${zoneId}/revisions/${revisionId}`); }
  saveDraft(zoneId: number, payload: SaveZoneRevisionDraft) { return this.api.put<ZoneRevision>(`/zones/${zoneId}/revisions/draft`, payload); }
  publish(zoneId: number, expectedVersion?: number) {
    return this.api.post<ZoneRevision>(`/zones/${zoneId}/revisions/publish`, { expected_version: expectedVersion ?? 0 });
  }
}
